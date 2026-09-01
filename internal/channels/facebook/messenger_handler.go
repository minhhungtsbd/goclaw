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

	// Page routing guard (before dedup write).
	if entry.ID != ch.pageID {
		return
	}

	// Track admin (page) replies: when the page itself sends a message,
	// record the recipient's chat ID so the bot skips auto-reply for that
	// conversation during the cooldown window.
	if event.Sender.ID == ch.pageID {
		if event.Recipient.ID != "" && event.Message != nil {
			eventAt := messagingEventTime(event.Timestamp)
			messageID := strings.TrimSpace(event.Message.MID)
			dedupKey := ""
			if messageID != "" {
				dedupKey = "page:" + messageID
			}
			if dedupKey != "" && ch.isDup(dedupKey) {
				slog.Debug("facebook: duplicate Page message skipped", "message_id", messageID)
				return
			}
			if ch.isBotEcho(event.Recipient.ID, messageID, eventAt) {
				slog.Debug("facebook: bot echo ignored", "chat_id", event.Recipient.ID)
				return
			}
			takeoverAt := time.Now()
			ch.adminReplied.Store(event.Recipient.ID, takeoverAt)
			if err := ch.persistAdminTakeover(ctx, event.Recipient.ID, takeoverAt, event.Message); err != nil {
				slog.Warn("facebook: admin takeover persistence failed",
					"chat_id", event.Recipient.ID, "error", err)
				// A failed takeover write must remain retryable. If the durable lease
				// exists and only session persistence failed, retain the dedup claim
				// to avoid appending the same Admin reply twice.
				active, lookupErr := ch.adminTakeoverActive(ctx, event.Recipient.ID, time.Now())
				if dedupKey != "" && (lookupErr != nil || !active) {
					ch.dedup.Delete(dedupKey)
				}
			} else {
				slog.Info("facebook: admin takeover activated", "chat_id", event.Recipient.ID)
			}
		}
		return
	}

	// Feature gate. Page-admin events above are persisted even while automatic
	// replies are disabled, so re-enabling the channel cannot steal a live chat.
	if !ch.config.Features.MessengerAutoReply {
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
	active, err := ch.adminTakeoverActive(ctx, senderID, time.Now())
	if err != nil {
		slog.Warn("facebook: takeover lookup failed; suppressing inbound auto-reply",
			"chat_id", senderID, "error", err)
		return
	}
	if active {
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

// persistAdminTakeover records durable human control and stores the Page reply
// as assistant context. It bypasses HandleMessage, so it cannot trigger an LLM
// turn or emit an outbound reply.
func (ch *Channel) persistAdminTakeover(ctx context.Context, customerID string, eventAt time.Time, message *IncomingMessage) error {
	if ch.AgentID() == "" {
		return fmt.Errorf("agent id is required")
	}
	messageID, messageText := "", ""
	if message != nil {
		messageID = strings.TrimSpace(message.MID)
		messageText = adminMessageHistoryText(message)
	}
	if ch.takeovers != nil {
		_, err := ch.takeovers.Activate(ctx, store.ChannelAdminTakeover{
			ChannelName: ch.Name(), ChatID: customerID, AgentKey: ch.AgentID(),
			AdminMessageID: messageID, LastAdminMessage: messageText,
			TakenOverAt: eventAt, ExpiresAt: eventAt.Add(ch.adminReplyCooldown()),
		})
		if err != nil {
			return err
		}
	}
	if ch.sessionMessages == nil || messageText == "" {
		return nil
	}
	key := sessions.BuildSessionKey(ch.AgentID(), ch.Name(), sessions.PeerDirect, customerID)
	content := "[Tin nhắn do nhân viên Admin Cloudmini gửi cho khách]\n" + messageText
	ch.sessionMessages.AddMessage(ctx, key, providers.Message{Role: "assistant", Content: content})
	if err := ch.sessionMessages.Save(ctx, key); err != nil {
		return fmt.Errorf("save admin message: %w", err)
	}
	return nil
}

func adminMessageHistoryText(message *IncomingMessage) string {
	if message == nil {
		return ""
	}
	parts := make([]string, 0, len(message.Attachments)+1)
	if text := strings.TrimSpace(message.Text); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range message.Attachments {
		kind := strings.TrimSpace(attachment.Type)
		if kind == "" {
			kind = "file"
		}
		parts = append(parts, "[Admin gửi đính kèm: "+kind+"]")
	}
	return strings.Join(parts, "\n")
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
