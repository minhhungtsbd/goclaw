package pg

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

type PGChannelAdminTakeoverStore struct{ db *sql.DB }

func NewPGChannelAdminTakeoverStore(db *sql.DB) *PGChannelAdminTakeoverStore {
	return &PGChannelAdminTakeoverStore{db: db}
}

const channelAdminTakeoverColumns = `id, tenant_id, channel_name, chat_id, agent_key,
	admin_message_id, last_admin_message, taken_over_at, expires_at, released_at,
	released_by, release_reason, created_at, updated_at`

type takeoverScanner interface{ Scan(...any) error }

func scanChannelAdminTakeover(scanner takeoverScanner) (*store.ChannelAdminTakeover, error) {
	var item store.ChannelAdminTakeover
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.ChannelName, &item.ChatID, &item.AgentKey,
		&item.AdminMessageID, &item.LastAdminMessage, &item.TakenOverAt, &item.ExpiresAt,
		&item.ReleasedAt, &item.ReleasedBy, &item.ReleaseReason, &item.CreatedAt, &item.UpdatedAt,
	)
	return &item, err
}

func takeoverTenantID(ctx context.Context) (uuid.UUID, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("channel admin takeover: tenant_id required")
	}
	return tenantID, nil
}

func (s *PGChannelAdminTakeoverStore) Activate(ctx context.Context, item store.ChannelAdminTakeover) (*store.ChannelAdminTakeover, error) {
	tenantID, err := takeoverTenantID(ctx)
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
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
	ON CONFLICT (tenant_id, channel_name, chat_id) DO UPDATE SET
		agent_key = EXCLUDED.agent_key,
		admin_message_id = EXCLUDED.admin_message_id,
		last_admin_message = EXCLUDED.last_admin_message,
		taken_over_at = EXCLUDED.taken_over_at,
		expires_at = EXCLUDED.expires_at,
		released_at = NULL,
		released_by = '',
		release_reason = '',
		updated_at = EXCLUDED.updated_at
	RETURNING `+channelAdminTakeoverColumns,
		item.ID, tenantID, item.ChannelName, item.ChatID, item.AgentKey,
		item.AdminMessageID, item.LastAdminMessage, item.TakenOverAt, item.ExpiresAt, now)
	created, err := scanChannelAdminTakeover(row)
	if err != nil {
		return nil, fmt.Errorf("activate channel admin takeover: %w", err)
	}
	return created, nil
}

func (s *PGChannelAdminTakeoverStore) GetActive(ctx context.Context, channelName, chatID string, now time.Time) (*store.ChannelAdminTakeover, error) {
	tenantID, err := takeoverTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+channelAdminTakeoverColumns+`
		FROM channel_admin_takeovers
		WHERE tenant_id=$1 AND channel_name=$2 AND chat_id=$3
		  AND released_at IS NULL AND expires_at>$4`,
		tenantID, strings.TrimSpace(channelName), strings.TrimSpace(chatID), now)
	item, err := scanChannelAdminTakeover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrChannelAdminTakeoverNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active channel admin takeover: %w", err)
	}
	return item, nil
}

func (s *PGChannelAdminTakeoverStore) ListActive(ctx context.Context, opts store.ChannelAdminTakeoverListOptions) ([]store.ChannelAdminTakeover, int, error) {
	tenantID, err := takeoverTenantID(ctx)
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
	where := "tenant_id=$1 AND released_at IS NULL AND expires_at>$2"
	args := []any{tenantID, opts.Now}
	if strings.TrimSpace(opts.ChannelName) != "" {
		where += fmt.Sprintf(" AND channel_name=$%d", len(args)+1)
		args = append(args, strings.TrimSpace(opts.ChannelName))
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM channel_admin_takeovers WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count active channel admin takeovers: %w", err)
	}
	args = append(args, opts.Limit, opts.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelAdminTakeoverColumns+`
		FROM channel_admin_takeovers WHERE `+where+
		fmt.Sprintf(" ORDER BY taken_over_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list active channel admin takeovers: %w", err)
	}
	defer rows.Close()
	items := make([]store.ChannelAdminTakeover, 0)
	for rows.Next() {
		item, scanErr := scanChannelAdminTakeover(rows)
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

func (s *PGChannelAdminTakeoverStore) Release(ctx context.Context, id uuid.UUID, releasedBy, reason string, now time.Time) (*store.ChannelAdminTakeover, error) {
	tenantID, err := takeoverTenantID(ctx)
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
		SET released_at=$1, released_by=$2, release_reason=$3, updated_at=$1
		WHERE id=$4 AND tenant_id=$5 AND released_at IS NULL AND expires_at>$1
		RETURNING `+channelAdminTakeoverColumns,
		now, releasedBy, reason, id, tenantID)
	item, err := scanChannelAdminTakeover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrChannelAdminTakeoverNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("release channel admin takeover: %w", err)
	}
	return item, nil
}
