package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AdminHandoff records the exact customer route for an Admin-owned support case.
// TicketNumber is the durable customer-facing reference; ID remains internal.
type AdminHandoff struct {
	ID                uuid.UUID         `json:"id"`
	TicketNumber      int64             `json:"ticket_number"`
	TenantID          uuid.UUID         `json:"tenant_id"`
	AgentID           uuid.UUID         `json:"agent_id"`
	AdminChannel      string            `json:"admin_channel"`
	AdminChatID       string            `json:"admin_chat_id"`
	SourceChannel     string            `json:"source_channel"`
	SourceChatID      string            `json:"source_chat_id"`
	SourceMetadata    map[string]string `json:"source_metadata"`
	DedupeKey         string            `json:"dedupe_key,omitempty"`
	Priority          string            `json:"priority"`
	Service           string            `json:"service"`
	Identifiers       []string          `json:"identifiers"`
	Summary           string            `json:"summary"`
	Status            string            `json:"status"`
	CreatedAt         time.Time         `json:"created_at"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty"`
	CompletionMessage string            `json:"completion_message,omitempty"`
}

type AdminHandoffListOptions struct {
	Search   string
	Status   string
	Priority string
	Service  string
	Limit    int
	Offset   int
}

type AdminHandoffEvent struct {
	ID        uuid.UUID      `json:"id"`
	TenantID  uuid.UUID      `json:"tenant_id"`
	HandoffID uuid.UUID      `json:"handoff_id"`
	Action    string         `json:"action"`
	ActorType string         `json:"actor_type"`
	ActorID   string         `json:"actor_id"`
	Content   string         `json:"content,omitempty"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// Reference returns the stable public ticket reference. The UUID form is kept
// only for rows created before ticket numbers were introduced.
func (h AdminHandoff) Reference() string {
	if h.TicketNumber > 0 {
		return fmt.Sprintf("Ticket-%06d", h.TicketNumber)
	}
	return "CMH-" + strings.ToUpper(h.ID.String()[:8])
}

type AdminHandoffStore interface {
	Create(context.Context, *AdminHandoff) error
	// CreateOrMerge reuses a pending handoff with the same dedupe key. It
	// preserves one customer case while appending later related requests.
	CreateOrMerge(context.Context, *AdminHandoff) (*AdminHandoff, error)
	Get(context.Context, uuid.UUID) (*AdminHandoff, error)
	// GetByTicketNumberForSource resolves a public ticket only within the
	// current tenant, agent, channel, and customer chat. This prevents an
	// agent from probing or disclosing another customer's handoff.
	GetByTicketNumberForSource(context.Context, int64, uuid.UUID, string, string) (*AdminHandoff, error)
	ListPending(context.Context, uuid.UUID, string, string, int) ([]AdminHandoff, error)
	MarkCompleted(context.Context, uuid.UUID, string) (*AdminHandoff, error)
	MarkDismissed(context.Context, uuid.UUID) (*AdminHandoff, error)
	MarkDeliveryFailed(context.Context, uuid.UUID) error
}

// AdminHandoffManagementStore extends the runtime store with tenant-scoped
// querying and audit history used by the Admin UI.
type AdminHandoffManagementStore interface {
	AdminHandoffStore
	List(context.Context, AdminHandoffListOptions) ([]AdminHandoff, int, error)
	ListEvents(context.Context, uuid.UUID) ([]AdminHandoffEvent, error)
	AppendEvent(context.Context, *AdminHandoffEvent) error
}
