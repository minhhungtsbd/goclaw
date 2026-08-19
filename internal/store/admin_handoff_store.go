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
	ID                uuid.UUID
	TicketNumber      int64
	TenantID          uuid.UUID
	AgentID           uuid.UUID
	AdminChannel      string
	AdminChatID       string
	SourceChannel     string
	SourceChatID      string
	SourceMetadata    map[string]string
	DedupeKey         string
	Priority          string
	Service           string
	Identifiers       []string
	Summary           string
	Status            string
	CreatedAt         time.Time
	CompletedAt       *time.Time
	CompletionMessage string
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
	ListPending(context.Context, uuid.UUID, string, string, int) ([]AdminHandoff, error)
	MarkCompleted(context.Context, uuid.UUID, string) (*AdminHandoff, error)
	MarkDismissed(context.Context, uuid.UUID) (*AdminHandoff, error)
	MarkDeliveryFailed(context.Context, uuid.UUID) error
}
