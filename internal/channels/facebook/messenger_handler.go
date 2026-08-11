package facebook

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleMessagingEvent processes a Messenger inbox event.
func (ch *Channel) handleMessagingEvent(ctx context.Context, entry WebhookEntry, event MessagingEvent) {
	ctx = store.WithTenantID(ctx, ch.TenantID())
	// Feature gate.
	if !ch.config.Features.MessengerAutoReply {
		return
	}

	// Page routing guard (before dedup write).
	if entry.ID != ch.pageID {
		return
	}

	// Track admin (page) replies: when the page itself sends a message,
	// record the recipient's chat ID so the bot skips auto-reply for that
	// conversation during the cooldown window.
	if event.Sender.ID == ch.pageID {
		if event.Recipient.ID != "" {
			eventAt := messagingEventTime(event.Timestamp)
			if ch.isBotEcho(event.Recipient.ID, eventAt) {
				slog.Debug("facebook: bot echo ignored", "chat_id", event.Recipient.ID)
				return
			}
			ch.adminReplied.Store(event.Recipient.ID, eventAt)
			ch.persistAdminMessage(ctx, event.Recipient.ID, event.Message)
			slog.Debug("facebook: admin reply tracked", "chat_id", event.Recipient.ID)
		}
		return
	}

	// Skip delivery/read receipts and other non-content events.
	if event.Message == nil && event.Postback == nil {
		return
	}

	// Dedup by message MID or postback signature (include payload to reduce collision risk).
	var eventKey string
	switch {
	case event.Message != nil:
		eventKey = "msg:" + event.Message.MID
	case event.Postback != nil:
		eventKey = fmt.Sprintf("postback:%s:%d:%s", event.Sender.ID, event.Timestamp, event.Postback.Payload)
	}
	if ch.isDup(eventKey) {
		slog.Debug("facebook: duplicate messaging event skipped", "key", eventKey)
		return
	}

	// Check if admin already replied to this conversation recently.
	senderID := event.Sender.ID
	if ch.adminRepliedRecently(senderID, time.Now()) {
		slog.Info("facebook: skipping auto-reply (admin replied recently)", "chat_id", senderID)
		return
	}

	// Extract text content and media.
	var content string
	var media []string

	if event.Message != nil {
		content = event.Message.Text
		for _, att := range event.Message.Attachments {
			if att.Type == "image" && att.Payload.URL != "" {
				localPath, err := ch.downloadMedia(ctx, att.Payload.URL)
				if err != nil {
					slog.Warn("facebook: failed to download media", "url", att.Payload.URL, "error", err)
					continue
				}
				media = append(media, localPath)
			}
		}
	} else if event.Postback != nil {
		content = event.Postback.Title
	}

	// If no content and no media, skip.
	if content == "" && len(media) == 0 {
		return
	}

	// Messenger sessions are 1:1: chatID = senderID (channel name scopes the session).
	chatID := senderID

	metadata := map[string]string{
		"fb_mode":    "messenger",
		"message_id": eventKey,
		"page_id":    ch.pageID,
		"sender_id":  senderID,
	}
	if ch.config.MessengerOptions.SessionTimeout != "" {
		metadata["session_timeout"] = ch.config.MessengerOptions.SessionTimeout
	}

	ch.HandleMessage(senderID, chatID, content, media, metadata, "direct")
}

// persistAdminMessage records a human Page reply as assistant context only.
// It deliberately bypasses HandleMessage, so it cannot trigger an LLM turn or
// emit an outbound reply.
func (ch *Channel) persistAdminMessage(ctx context.Context, customerID string, message *IncomingMessage) {
	if ch.sessionMessages == nil || message == nil || strings.TrimSpace(message.Text) == "" || ch.AgentID() == "" {
		return
	}
	key := sessions.BuildSessionKey(ch.AgentID(), ch.Name(), sessions.PeerDirect, customerID)
	content := "[Tin nhắn do nhân viên Admin Cloudmini gửi cho khách]\n" + strings.TrimSpace(message.Text)
	ch.sessionMessages.AddMessage(ctx, key, providers.Message{Role: "assistant", Content: content})
}

func messagingEventTime(ts int64) time.Time {
	switch {
	case ts > 1_000_000_000_000:
		return time.UnixMilli(ts)
	case ts > 0:
		return time.Unix(ts, 0)
	default:
		return time.Now()
	}
}

func (ch *Channel) downloadMedia(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "facebook_media_*.jpg")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	const maxMediaBytes = 25 << 20
	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	if written > maxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file exceeds maximum size of 25MB")
	}

	return tmpFile.Name(), nil
}
