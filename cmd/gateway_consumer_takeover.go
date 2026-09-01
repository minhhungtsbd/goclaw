package cmd

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// suppressInboundForAdminTakeover is the final pre-scheduler gate for Facebook
// Messenger. It closes the race where an Admin takes over after the webhook
// handler accepted a customer message but before debounce flushes it to the LLM.
// Store failures are fail-closed to avoid publishing a competing bot response.
func suppressInboundForAdminTakeover(ctx context.Context, msg bus.InboundMessage, deps *ConsumerDeps) bool {
	if deps == nil || deps.AdminTakeovers == nil || msg.Metadata["fb_mode"] != "messenger" {
		return false
	}
	_, err := deps.AdminTakeovers.GetActive(ctx, msg.Channel, msg.ChatID, time.Now())
	switch {
	case err == nil:
		slog.Info("inbound: skipping LLM schedule during Admin takeover",
			"channel", msg.Channel, "chat_id", msg.ChatID)
		return true
	case errors.Is(err, store.ErrChannelAdminTakeoverNotFound):
		return false
	default:
		slog.Warn("inbound: takeover lookup failed; suppressing LLM schedule",
			"channel", msg.Channel, "chat_id", msg.ChatID, "error", err)
		return true
	}
}
