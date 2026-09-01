//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteChannelAdminTakeoverStore struct{ db *sql.DB }

func NewSQLiteChannelAdminTakeoverStore(db *sql.DB) *SQLiteChannelAdminTakeoverStore {
	return &SQLiteChannelAdminTakeoverStore{db: db}
}

const sqliteChannelAdminTakeoverColumns = `id, tenant_id, channel_name, chat_id, agent_key,
	admin_message_id, last_admin_message, taken_over_at, expires_at, released_at,
	released_by, release_reason, created_at, updated_at`

type sqliteTakeoverScanner interface{ Scan(...any) error }

func scanSQLiteChannelAdminTakeover(scanner sqliteTakeoverScanner) (*store.ChannelAdminTakeover, error) {
	var item store.ChannelAdminTakeover
	var id, tenantID string
	takenOverAt, expiresAt := &sqliteTime{}, &sqliteTime{}
	releasedAt := &nullSqliteTime{}
	createdAt, updatedAt := scanTimePair()
	err := scanner.Scan(
		&id, &tenantID, &item.ChannelName, &item.ChatID, &item.AgentKey,
		&item.AdminMessageID, &item.LastAdminMessage, takenOverAt, expiresAt,
		releasedAt, &item.ReleasedBy, &item.ReleaseReason, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	item.TenantID, err = uuid.Parse(tenantID)
	item.TakenOverAt = takenOverAt.Time
	item.ExpiresAt = expiresAt.Time
	item.ReleasedAt = sqliteTimePtr(releasedAt)
	item.CreatedAt = createdAt.Time
	item.UpdatedAt = updatedAt.Time
	return &item, err
}

func sqliteTakeoverTenantID(ctx context.Context) (uuid.UUID, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("channel admin takeover: tenant_id required")
	}
	return tenantID, nil
}

func (s *SQLiteChannelAdminTakeoverStore) Activate(ctx context.Context, item store.ChannelAdminTakeover) (*store.ChannelAdminTakeover, error) {
	tenantID, err := sqliteTakeoverTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if err := item.ValidateActivation(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item.ID = uuid.Must(uuid.NewV7())
	row := s.db.QueryRowContext(ctx, `INSERT INTO channel_admin_takeovers (
		id, tenant_id, channel_name, chat_id, agent_key, admin_message_id,
		last_admin_message, taken_over_at, expires_at, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT (tenant_id, channel_name, chat_id) DO UPDATE SET
		agent_key=excluded.agent_key,
		admin_message_id=excluded.admin_message_id,
		last_admin_message=excluded.last_admin_message,
		taken_over_at=excluded.taken_over_at,
		expires_at=excluded.expires_at,
		released_at=NULL,
		released_by='',
		release_reason='',
		updated_at=excluded.updated_at
	RETURNING `+sqliteChannelAdminTakeoverColumns,
		item.ID.String(), tenantID.String(), item.ChannelName, item.ChatID, item.AgentKey,
		item.AdminMessageID, item.LastAdminMessage, item.TakenOverAt, item.ExpiresAt, now, now)
	created, err := scanSQLiteChannelAdminTakeover(row)
	if err != nil {
		return nil, fmt.Errorf("activate channel admin takeover: %w", err)
	}
	return created, nil
}

func (s *SQLiteChannelAdminTakeoverStore) GetActive(ctx context.Context, channelName, chatID string, now time.Time) (*store.ChannelAdminTakeover, error) {
	tenantID, err := sqliteTakeoverTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+sqliteChannelAdminTakeoverColumns+`
		FROM channel_admin_takeovers
		WHERE tenant_id=? AND channel_name=? AND chat_id=?
		  AND released_at IS NULL AND expires_at>?`,
		tenantID.String(), strings.TrimSpace(channelName), strings.TrimSpace(chatID), now)
	item, err := scanSQLiteChannelAdminTakeover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrChannelAdminTakeoverNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active channel admin takeover: %w", err)
	}
	return item, nil
}

func (s *SQLiteChannelAdminTakeoverStore) ListActive(ctx context.Context, opts store.ChannelAdminTakeoverListOptions) ([]store.ChannelAdminTakeover, int, error) {
	tenantID, err := sqliteTakeoverTenantID(ctx)
	if err != nil {
		return nil, 0, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	where := "tenant_id=? AND released_at IS NULL AND expires_at>?"
	args := []any{tenantID.String(), opts.Now}
	if strings.TrimSpace(opts.ChannelName) != "" {
		where += " AND channel_name=?"
		args = append(args, strings.TrimSpace(opts.ChannelName))
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM channel_admin_takeovers WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count active channel admin takeovers: %w", err)
	}
	queryArgs := append(append([]any{}, args...), opts.Limit, opts.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+sqliteChannelAdminTakeoverColumns+`
		FROM channel_admin_takeovers WHERE `+where+` ORDER BY taken_over_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list active channel admin takeovers: %w", err)
	}
	defer rows.Close()
	items := make([]store.ChannelAdminTakeover, 0)
	for rows.Next() {
		item, scanErr := scanSQLiteChannelAdminTakeover(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("scan active channel admin takeover: %w", scanErr)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list active channel admin takeovers: %w", err)
	}
	return items, total, nil
}

func (s *SQLiteChannelAdminTakeoverStore) Release(ctx context.Context, id uuid.UUID, releasedBy, reason string, now time.Time) (*store.ChannelAdminTakeover, error) {
	tenantID, err := sqliteTakeoverTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	releasedBy = strings.TrimSpace(releasedBy)
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(releasedBy) > 255 || utf8.RuneCountInString(reason) > 1000 {
		return nil, fmt.Errorf("release metadata is too long")
	}
	row := s.db.QueryRowContext(ctx, `UPDATE channel_admin_takeovers
		SET released_at=?, released_by=?, release_reason=?, updated_at=?
		WHERE id=? AND tenant_id=? AND released_at IS NULL AND expires_at>?
		RETURNING `+sqliteChannelAdminTakeoverColumns,
		now, releasedBy, reason, now, id.String(), tenantID.String(), now)
	item, err := scanSQLiteChannelAdminTakeover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrChannelAdminTakeoverNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("release channel admin takeover: %w", err)
	}
	return item, nil
}
