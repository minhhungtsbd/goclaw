package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AdminHandoff records the exact customer route for an Admin-owned support case.
// Its ID is the public reference used by Telegram commands and callbacks.
type AdminHandoff struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	AgentID           uuid.UUID
	AdminChannel      string
	AdminChatID       string
	SourceChannel     string
	SourceChatID      string
	SourceMetadata    map[string]string
	Summary           string
	Status            string
	CreatedAt         time.Time
	CompletedAt       *time.Time
	CompletionMessage string
}

type AdminHandoffStore interface {
	Create(context.Context, *AdminHandoff) error
	Get(context.Context, uuid.UUID) (*AdminHandoff, error)
	ListPending(context.Context, uuid.UUID, string, string, int) ([]AdminHandoff, error)
	MarkCompleted(context.Context, uuid.UUID, string) (*AdminHandoff, error)
	MarkDeliveryFailed(context.Context, uuid.UUID) error
}
