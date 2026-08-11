package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGAdminHandoffStore struct{ db *sql.DB }

func NewPGAdminHandoffStore(db *sql.DB) *PGAdminHandoffStore {
	return &PGAdminHandoffStore{db: db}
}

const adminHandoffColumns = `id, tenant_id, agent_id, admin_channel, admin_chat_id,
source_channel, source_chat_id, source_metadata, summary, status, created_at, completed_at, completion_message`

func (s *PGAdminHandoffStore) Create(ctx context.Context, h *store.AdminHandoff) error {
	metadata, err := json.Marshal(h.SourceMetadata)
	if err != nil {
		return fmt.Errorf("marshal source metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, summary, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', $11)`,
		h.ID, h.TenantID, h.AgentID, h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Summary, h.CreatedAt,
	)
	return err
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
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, source_metadata, dedupe_key, summary, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', $11)
		ON CONFLICT (tenant_id, dedupe_key) WHERE status = 'pending' AND dedupe_key <> ''
		DO UPDATE SET summary = admin_handoffs.summary || E'\n\n[Customer update]\n' || EXCLUDED.summary
		RETURNING `+adminHandoffColumns,
		h.ID, h.TenantID, h.AgentID, h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, metadata, h.DedupeKey, h.Summary, h.CreatedAt,
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
		WHERE id = $2 AND tenant_id = $3 AND status = 'pending'
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
		WHERE id = $1 AND tenant_id = $2 AND status = 'pending'`, id, store.TenantIDFromContext(ctx))
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
	handoff := &store.AdminHandoff{}
	var metadata json.RawMessage
	err := row.Scan(
		&handoff.ID, &handoff.TenantID, &handoff.AgentID, &handoff.AdminChannel, &handoff.AdminChatID,
		&handoff.SourceChannel, &handoff.SourceChatID, &metadata, &handoff.Summary, &handoff.Status,
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
	if handoff.SourceMetadata == nil {
		handoff.SourceMetadata = map[string]string{}
	}
	return handoff, nil
}
