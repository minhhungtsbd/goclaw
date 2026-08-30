package pg

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

type PGAdminHandoffStore struct{ db *sql.DB }

func NewPGAdminHandoffStore(db *sql.DB) *PGAdminHandoffStore {
	return &PGAdminHandoffStore{db: db}
}

const adminHandoffColumns = `id, ticket_number, tenant_id, agent_id, admin_channel, admin_chat_id,
source_channel, source_chat_id, source_metadata, priority, service, identifiers, summary, status, created_at, completed_at, completion_message`

func (s *PGAdminHandoffStore) Create(ctx context.Context, h *store.AdminHandoff) error {
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
			source_channel, source_chat_id, source_metadata, dedupe_key, priority, service, identifiers, summary, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'pending', $14)
		RETURNING ticket_number`,
		h.ID, h.TenantID, h.AgentID, h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Priority, h.Service, identifiers, h.Summary, h.CreatedAt,
	)
	return row.Scan(&h.TicketNumber)
}

func (s *PGAdminHandoffStore) CreateOrMerge(ctx context.Context, h *store.AdminHandoff) (*store.AdminHandoff, error) {
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
			source_channel, source_chat_id, source_metadata, dedupe_key, priority, service, identifiers, summary, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'pending', $14)
		ON CONFLICT (tenant_id, dedupe_key) WHERE status = 'pending' AND dedupe_key <> ''
		DO UPDATE SET
			summary = admin_handoffs.summary || E'\n\n[Customer update]\n' || EXCLUDED.summary,
			priority = EXCLUDED.priority,
			service = CASE WHEN EXCLUDED.service <> '' THEN EXCLUDED.service ELSE admin_handoffs.service END,
			identifiers = CASE WHEN EXCLUDED.identifiers <> '[]'::jsonb THEN EXCLUDED.identifiers ELSE admin_handoffs.identifiers END
		RETURNING `+adminHandoffColumns,
		h.ID, h.TenantID, h.AgentID, h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Priority, h.Service, identifiers, h.Summary, h.CreatedAt,
	)
	return scanAdminHandoff(row)
}

func (s *PGAdminHandoffStore) Get(ctx context.Context, id uuid.UUID) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+adminHandoffColumns+` FROM admin_handoffs WHERE id = $1 AND tenant_id = $2`,
		id, store.TenantIDFromContext(ctx),
	)
	return scanAdminHandoff(row)
}

func (s *PGAdminHandoffStore) GetByTicketNumberForSource(ctx context.Context, ticketNumber int64, agentID uuid.UUID, channel, chatID string) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+adminHandoffColumns+`
		FROM admin_handoffs
		WHERE ticket_number = $1 AND tenant_id = $2 AND agent_id = $3
			AND source_channel = $4 AND source_chat_id = $5`,
		ticketNumber, store.TenantIDFromContext(ctx), agentID, channel, chatID,
	)
	return scanAdminHandoff(row)
}

func (s *PGAdminHandoffStore) ListPending(ctx context.Context, tenantID uuid.UUID, channel, chatID string, limit int) ([]store.AdminHandoff, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+adminHandoffColumns+`
		FROM admin_handoffs
		WHERE tenant_id = $1 AND admin_channel = $2 AND admin_chat_id = $3 AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT $4`, tenantID, channel, chatID, limit)
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

func (s *PGAdminHandoffStore) MarkCompleted(ctx context.Context, id uuid.UUID, message string) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'completed', completed_at = now(), completion_message = $1
		WHERE id = $2 AND tenant_id = $3 AND status IN ('pending', 'delivery_failed')
		RETURNING `+adminHandoffColumns, message, id, store.TenantIDFromContext(ctx))
	return scanAdminHandoff(row)
}

func (s *PGAdminHandoffStore) MarkDismissed(ctx context.Context, id uuid.UUID) (*store.AdminHandoff, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'dismissed', completed_at = now(), completion_message = '[dismissed by Admin]'
		WHERE id = $1 AND tenant_id = $2 AND status = 'pending'
		RETURNING `+adminHandoffColumns, id, store.TenantIDFromContext(ctx))
	return scanAdminHandoff(row)
}

func (s *PGAdminHandoffStore) MarkDeliveryFailed(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_handoffs
		SET status = 'delivery_failed'
		WHERE id = $1 AND tenant_id = $2 AND status IN ('pending', 'completed')`, id, store.TenantIDFromContext(ctx))
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

func (s *PGAdminHandoffStore) List(ctx context.Context, opts store.AdminHandoffListOptions) ([]store.AdminHandoff, int, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	conditions := []string{"tenant_id = $1"}
	args := []any{store.TenantIDFromContext(ctx)}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if value := strings.TrimSpace(opts.Status); value != "" {
		add("status = $%d", value)
	}
	if value := strings.TrimSpace(opts.Priority); value != "" {
		add("priority = $%d", value)
	}
	if value := strings.TrimSpace(opts.Service); value != "" {
		add("service ILIKE $%d", "%"+value+"%")
	}
	if value := strings.TrimSpace(opts.Search); value != "" {
		add("(summary ILIKE $%[1]d OR service ILIKE $%[1]d OR identifiers::text ILIKE $%[1]d OR ('Ticket-' || lpad(ticket_number::text, 6, '0')) ILIKE $%[1]d)", "%"+value+"%")
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admin_handoffs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, max(opts.Offset, 0))
	query := fmt.Sprintf("SELECT %s FROM admin_handoffs WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", adminHandoffColumns, where, len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
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

func (s *PGAdminHandoffStore) ListEvents(ctx context.Context, handoffID uuid.UUID) ([]store.AdminHandoffEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, handoff_id, action, actor_type, actor_id, content, metadata, created_at
		FROM admin_handoff_events WHERE tenant_id = $1 AND handoff_id = $2 ORDER BY created_at ASC`, store.TenantIDFromContext(ctx), handoffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.AdminHandoffEvent, 0)
	for rows.Next() {
		var event store.AdminHandoffEvent
		var metadata json.RawMessage
		if err := rows.Scan(&event.ID, &event.TenantID, &event.HandoffID, &event.Action, &event.ActorType, &event.ActorID, &event.Content, &metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *PGAdminHandoffStore) AppendEvent(ctx context.Context, event *store.AdminHandoffEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.Must(uuid.NewV7())
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TenantID = store.TenantIDFromContext(ctx)
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_handoff_events
		(id, tenant_id, handoff_id, action, actor_type, actor_id, content, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, event.ID, event.TenantID, event.HandoffID, event.Action, event.ActorType, event.ActorID, event.Content, metadata, event.CreatedAt)
	return err
}

type adminHandoffScanner interface {
	Scan(dest ...any) error
}

func scanAdminHandoff(row adminHandoffScanner) (*store.AdminHandoff, error) {
	handoff := &store.AdminHandoff{}
	var metadata json.RawMessage
	var identifiers json.RawMessage
	err := row.Scan(
		&handoff.ID, &handoff.TicketNumber, &handoff.TenantID, &handoff.AgentID, &handoff.AdminChannel, &handoff.AdminChatID,
		&handoff.SourceChannel, &handoff.SourceChatID, &metadata, &handoff.Priority, &handoff.Service, &identifiers, &handoff.Summary, &handoff.Status,
		&handoff.CreatedAt, &handoff.CompletedAt, &handoff.CompletionMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("admin handoff not found or is no longer pending")
	}
	if err != nil {
		return nil, err
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
