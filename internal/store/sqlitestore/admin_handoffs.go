//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteAdminHandoffStore struct{ db *sql.DB }

func NewSQLiteAdminHandoffStore(db *sql.DB) *SQLiteAdminHandoffStore {
	return &SQLiteAdminHandoffStore{db: db}
}

const adminHandoffColumns = `id, ticket_number, tenant_id, agent_id, admin_channel, admin_chat_id,
source_channel, source_chat_id, source_metadata, priority, service, identifiers, summary, status, created_at, completed_at, completion_message`

func (s *SQLiteAdminHandoffStore) Create(ctx context.Context, h *store.AdminHandoff) error {
	metadata, err := json.Marshal(h.SourceMetadata)
	if err != nil {
		return fmt.Errorf("marshal source metadata: %w", err)
	}
	identifiers, err := json.Marshal(h.Identifiers)
	if err != nil {
		return fmt.Errorf("marshal handoff identifiers: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, priority, service, identifiers, summary, status, created_at, ticket_number
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, (SELECT COALESCE(MAX(ticket_number), 0) + 1 FROM admin_handoffs))
		RETURNING ticket_number`,
		h.ID.String(), h.TenantID.String(), h.AgentID.String(), h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Priority, h.Service, identifiers, h.Summary, h.CreatedAt,
	)
	return row.Scan(&h.TicketNumber)
}

func (s *SQLiteAdminHandoffStore) CreateOrMerge(ctx context.Context, h *store.AdminHandoff) (*store.AdminHandoff, error) {
	if h.DedupeKey == "" {
		if err := s.Create(ctx, h); err != nil {
			return nil, err
		}
		return h, nil
	}
	metadata, err := json.Marshal(h.SourceMetadata)
	if err != nil {
		return nil, fmt.Errorf("marshal source metadata: %w", err)
	}
	identifiers, err := json.Marshal(h.Identifiers)
	if err != nil {
		return nil, fmt.Errorf("marshal handoff identifiers: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, priority, service, identifiers, summary, status, created_at, ticket_number
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, (SELECT COALESCE(MAX(ticket_number), 0) + 1 FROM admin_handoffs))
		ON CONFLICT (tenant_id, dedupe_key) WHERE status = 'pending' AND dedupe_key <> ''
		DO UPDATE SET
			summary = admin_handoffs.summary || char(10) || char(10) || '[Customer update]' || char(10) || excluded.summary,
			priority = excluded.priority,
			service = CASE WHEN excluded.service <> '' THEN excluded.service ELSE admin_handoffs.service END,
			identifiers = CASE WHEN excluded.identifiers <> '[]' THEN excluded.identifiers ELSE admin_handoffs.identifiers END
		RETURNING `+adminHandoffColumns,
		h.ID.String(), h.TenantID.String(), h.AgentID.String(), h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Priority, h.Service, identifiers, h.Summary, h.CreatedAt,
	)
	return scanAdminHandoff(row)
}

func (s *SQLiteAdminHandoffStore) Get(ctx context.Context, id uuid.UUID) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+adminHandoffColumns+` FROM admin_handoffs WHERE id = ? AND tenant_id = ?`,
		id.String(), store.TenantIDFromContext(ctx).String(),
	)
	return scanAdminHandoff(row)
}

func (s *SQLiteAdminHandoffStore) GetByTicketNumberForSource(ctx context.Context, ticketNumber int64, agentID uuid.UUID, channel, chatID string) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+adminHandoffColumns+`
		FROM admin_handoffs
		WHERE ticket_number = ? AND tenant_id = ? AND agent_id = ?
			AND source_channel = ? AND source_chat_id = ?`,
		ticketNumber, store.TenantIDFromContext(ctx).String(), agentID.String(), channel, chatID,
	)
	return scanAdminHandoff(row)
}

func (s *SQLiteAdminHandoffStore) ListPending(ctx context.Context, tenantID uuid.UUID, channel, chatID string, limit int) ([]store.AdminHandoff, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+adminHandoffColumns+`
		FROM admin_handoffs
		WHERE tenant_id = ? AND admin_channel = ? AND admin_chat_id = ? AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT ?`, tenantID.String(), channel, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.AdminHandoff, 0)
	for rows.Next() {
		handoff, err := scanAdminHandoff(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *handoff)
	}
	return result, rows.Err()
}

func (s *SQLiteAdminHandoffStore) MarkCompleted(ctx context.Context, id uuid.UUID, message string) (*store.AdminHandoff, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'completed', completed_at = ?, completion_message = ?
		WHERE id = ? AND tenant_id = ? AND status IN ('pending', 'delivery_failed')`,
		time.Now().UTC(), message, id.String(), store.TenantIDFromContext(ctx).String())
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("admin handoff not found or is no longer pending")
	}
	return s.Get(ctx, id)
}

func (s *SQLiteAdminHandoffStore) List(ctx context.Context, opts store.AdminHandoffListOptions) ([]store.AdminHandoff, int, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	conditions := []string{"tenant_id = ?"}
	args := []any{store.TenantIDFromContext(ctx).String()}
	if value := strings.TrimSpace(opts.Status); value != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(opts.Priority); value != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(opts.Service); value != "" {
		conditions = append(conditions, "lower(service) LIKE lower(?)")
		args = append(args, "%"+value+"%")
	}
	if value := strings.TrimSpace(opts.Search); value != "" {
		conditions = append(conditions, "(lower(summary) LIKE lower(?) OR lower(service) LIKE lower(?) OR lower(identifiers) LIKE lower(?) OR lower('Ticket-' || printf('%06d', ticket_number)) LIKE lower(?))")
		search := "%" + value + "%"
		args = append(args, search, search, search, search)
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admin_handoffs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), limit, max(opts.Offset, 0))
	rows, err := s.db.QueryContext(ctx, "SELECT "+adminHandoffColumns+" FROM admin_handoffs WHERE "+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]store.AdminHandoff, 0)
	for rows.Next() {
		handoff, err := scanAdminHandoff(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *handoff)
	}
	return result, total, rows.Err()
}

func (s *SQLiteAdminHandoffStore) ListEvents(ctx context.Context, handoffID uuid.UUID) ([]store.AdminHandoffEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, handoff_id, action, actor_type, actor_id, content, metadata, created_at
		FROM admin_handoff_events WHERE tenant_id = ? AND handoff_id = ? ORDER BY created_at ASC`, store.TenantIDFromContext(ctx).String(), handoffID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.AdminHandoffEvent, 0)
	for rows.Next() {
		var event store.AdminHandoffEvent
		var id, tenantID, eventHandoffID string
		var metadata json.RawMessage
		if err := rows.Scan(&id, &tenantID, &eventHandoffID, &event.Action, &event.ActorType, &event.ActorID, &event.Content, &metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		var err error
		if event.ID, err = uuid.Parse(id); err != nil {
			return nil, err
		}
		if event.TenantID, err = uuid.Parse(tenantID); err != nil {
			return nil, err
		}
		if event.HandoffID, err = uuid.Parse(eventHandoffID); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *SQLiteAdminHandoffStore) AppendEvent(ctx context.Context, event *store.AdminHandoffEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.Must(uuid.NewV7())
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.TenantID = store.TenantIDFromContext(ctx)
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_handoff_events
		(id, tenant_id, handoff_id, action, actor_type, actor_id, content, metadata, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, event.ID.String(), event.TenantID.String(), event.HandoffID.String(), event.Action, event.ActorType, event.ActorID, event.Content, metadata, event.CreatedAt)
	return err
}

func (s *SQLiteAdminHandoffStore) MarkDismissed(ctx context.Context, id uuid.UUID) (*store.AdminHandoff, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'dismissed', completed_at = ?, completion_message = '[dismissed by Admin]'
		WHERE id = ? AND tenant_id = ? AND status = 'pending'`,
		time.Now().UTC(), id.String(), store.TenantIDFromContext(ctx).String())
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("admin handoff not found or is no longer pending")
	}
	return s.Get(ctx, id)
}

func (s *SQLiteAdminHandoffStore) MarkDeliveryFailed(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'delivery_failed'
		WHERE id = ? AND tenant_id = ? AND status IN ('pending', 'completed')`,
		id.String(), store.TenantIDFromContext(ctx).String())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("admin handoff is not pending or was not found")
	}
	return nil
}

type adminHandoffScanner interface {
	Scan(dest ...any) error
}

func scanAdminHandoff(row adminHandoffScanner) (*store.AdminHandoff, error) {
	var id, tenantID, agentID string
	var metadata json.RawMessage
	var identifiers json.RawMessage
	handoff := &store.AdminHandoff{}
	err := row.Scan(
		&id, &handoff.TicketNumber, &tenantID, &agentID, &handoff.AdminChannel, &handoff.AdminChatID,
		&handoff.SourceChannel, &handoff.SourceChatID, &metadata, &handoff.Priority, &handoff.Service, &identifiers, &handoff.Summary, &handoff.Status,
		&handoff.CreatedAt, &handoff.CompletedAt, &handoff.CompletionMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("admin handoff not found or is no longer pending")
	}
	if err != nil {
		return nil, err
	}
	var parseErr error
	if handoff.ID, parseErr = uuid.Parse(id); parseErr != nil {
		return nil, parseErr
	}
	if handoff.TenantID, parseErr = uuid.Parse(tenantID); parseErr != nil {
		return nil, parseErr
	}
	if handoff.AgentID, parseErr = uuid.Parse(agentID); parseErr != nil {
		return nil, parseErr
	}
	if err := json.Unmarshal(metadata, &handoff.SourceMetadata); err != nil {
		return nil, fmt.Errorf("decode source metadata: %w", err)
	}
	if err := json.Unmarshal(identifiers, &handoff.Identifiers); err != nil {
		return nil, fmt.Errorf("decode handoff identifiers: %w", err)
	}
	if handoff.SourceMetadata == nil {
		handoff.SourceMetadata = map[string]string{}
	}
	return handoff, nil
}
