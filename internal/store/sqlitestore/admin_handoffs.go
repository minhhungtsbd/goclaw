//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteAdminHandoffStore struct{ db *sql.DB }

func NewSQLiteAdminHandoffStore(db *sql.DB) *SQLiteAdminHandoffStore {
	return &SQLiteAdminHandoffStore{db: db}
}

const adminHandoffColumns = `id, ticket_number, tenant_id, agent_id, admin_channel, admin_chat_id,
source_channel, source_chat_id, source_metadata, summary, status, created_at, completed_at, completion_message`

func (s *SQLiteAdminHandoffStore) Create(ctx context.Context, h *store.AdminHandoff) error {
	metadata, err := json.Marshal(h.SourceMetadata)
	if err != nil {
		return fmt.Errorf("marshal source metadata: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, summary, status, created_at, ticket_number
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, (SELECT COALESCE(MAX(ticket_number), 0) + 1 FROM admin_handoffs))
		RETURNING ticket_number`,
		h.ID.String(), h.TenantID.String(), h.AgentID.String(), h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Summary, h.CreatedAt,
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
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, summary, status, created_at, ticket_number
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, (SELECT COALESCE(MAX(ticket_number), 0) + 1 FROM admin_handoffs))
		ON CONFLICT (tenant_id, dedupe_key) WHERE status = 'pending' AND dedupe_key <> ''
		DO UPDATE SET summary = admin_handoffs.summary || char(10) || char(10) || '[Customer update]' || char(10) || excluded.summary
		RETURNING `+adminHandoffColumns,
		h.ID.String(), h.TenantID.String(), h.AgentID.String(), h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Summary, h.CreatedAt,
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
		WHERE id = ? AND tenant_id = ? AND status = 'pending'`,
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
		WHERE id = ? AND tenant_id = ? AND status = 'pending'`,
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
	handoff := &store.AdminHandoff{}
	err := row.Scan(
		&id, &handoff.TicketNumber, &tenantID, &agentID, &handoff.AdminChannel, &handoff.AdminChatID,
		&handoff.SourceChannel, &handoff.SourceChatID, &metadata, &handoff.Summary, &handoff.Status,
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
	if handoff.SourceMetadata == nil {
		handoff.SourceMetadata = map[string]string{}
	}
	return handoff, nil
}
