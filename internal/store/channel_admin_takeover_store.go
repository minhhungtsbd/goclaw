package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrChannelAdminTakeoverNotFound = errors.New("channel admin takeover not found")

// ChannelAdminTakeover is a durable human-takeover lease for one direct chat.
// TenantID is populated by stores and is never accepted from HTTP callers.
type ChannelAdminTakeover struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	TenantID         uuid.UUID  `json:"-" db:"tenant_id"`
	ChannelName      string     `json:"channel_name" db:"channel_name"`
	ChatID           string     `json:"chat_id" db:"chat_id"`
	AgentKey         string     `json:"agent_key,omitempty" db:"agent_key"`
	AdminMessageID   string     `json:"admin_message_id,omitempty" db:"admin_message_id"`
	LastAdminMessage string     `json:"last_admin_message,omitempty" db:"last_admin_message"`
	TakenOverAt      time.Time  `json:"taken_over_at" db:"taken_over_at"`
	ExpiresAt        time.Time  `json:"expires_at" db:"expires_at"`
	ReleasedAt       *time.Time `json:"released_at,omitempty" db:"released_at"`
	ReleasedBy       string     `json:"released_by,omitempty" db:"released_by"`
	ReleaseReason    string     `json:"release_reason,omitempty" db:"release_reason"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

func (t *ChannelAdminTakeover) Normalize() {
	if t == nil {
		return
	}
	t.ChannelName = strings.TrimSpace(t.ChannelName)
	t.ChatID = strings.TrimSpace(t.ChatID)
	t.AgentKey = strings.TrimSpace(t.AgentKey)
	t.AdminMessageID = strings.TrimSpace(t.AdminMessageID)
	t.LastAdminMessage = strings.TrimSpace(t.LastAdminMessage)
	if utf8.RuneCountInString(t.LastAdminMessage) > 4000 {
		t.LastAdminMessage = string([]rune(t.LastAdminMessage)[:4000])
	}
}

func (t *ChannelAdminTakeover) ValidateActivation() error {
	if t == nil {
		return fmt.Errorf("takeover is required")
	}
	t.Normalize()
	if t.ChannelName == "" || utf8.RuneCountInString(t.ChannelName) > 100 {
		return fmt.Errorf("channel_name is required and must be at most 100 characters")
	}
	if t.ChatID == "" || utf8.RuneCountInString(t.ChatID) > 255 {
		return fmt.Errorf("chat_id is required and must be at most 255 characters")
	}
	if t.AgentKey == "" || utf8.RuneCountInString(t.AgentKey) > 100 {
		return fmt.Errorf("agent_key is required and must be at most 100 characters")
	}
	if utf8.RuneCountInString(t.AdminMessageID) > 255 {
		return fmt.Errorf("admin_message_id must be at most 255 characters")
	}
	if t.TakenOverAt.IsZero() || !t.ExpiresAt.After(t.TakenOverAt) {
		return fmt.Errorf("expires_at must be after taken_over_at")
	}
	return nil
}

type ChannelAdminTakeoverListOptions struct {
	ChannelName string
	Limit       int
	Offset      int
	Now         time.Time
}

// ChannelAdminTakeoverStore persists human takeover leases. All methods are
// tenant-scoped through context and must fail when tenant_id is absent.
type ChannelAdminTakeoverStore interface {
	Activate(ctx context.Context, takeover ChannelAdminTakeover) (*ChannelAdminTakeover, error)
	GetActive(ctx context.Context, channelName, chatID string, now time.Time) (*ChannelAdminTakeover, error)
	ListActive(ctx context.Context, opts ChannelAdminTakeoverListOptions) ([]ChannelAdminTakeover, int, error)
	Release(ctx context.Context, id uuid.UUID, releasedBy, reason string, now time.Time) (*ChannelAdminTakeover, error)
}
