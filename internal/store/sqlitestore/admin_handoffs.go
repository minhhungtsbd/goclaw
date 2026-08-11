//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
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

const adminHandoffColumns = `id, tenant_id, agent_id, admin_channel, admin_chat_id,
source_channel, source_chat_id, summary, status, created_at, completed_at, completion_message`

func (s *SQLiteAdminHandoffStore) Create(ctx context.Context, h *store.AdminHandoff) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_handoffs (
			id, tenant_id, agent_id, admin_channel, admin_chat_id,
			source_channel, source_chat_id, summary, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		h.ID.String(), h.TenantID.String(), h.AgentID.String(), h.AdminChannel, h.AdminChatID,
		h.SourceChannel, h.SourceChatID, h.Summary, h.CreatedAt,
	)
	return err
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
	handoff := &store.AdminHandoff{}
	err := row.Scan(
		&id, &tenantID, &agentID, &handoff.AdminChannel, &handoff.AdminChatID,
		&handoff.SourceChannel, &handoff.SourceChatID, &handoff.Summary, &handoff.Status,
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
	return handoff, nil
}
