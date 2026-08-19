package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestParseAdminHandoffCommand(t *testing.T) {
	caseID, message, ok := parseAdminHandoffCommand("/handoff_done Ticket-123456 Da xu ly xong")
	if !ok {
		t.Fatal("parseAdminHandoffCommand() ok = false")
	}
	if caseID != "Ticket-123456" || message != "Da xu ly xong" {
		t.Fatalf("parseAdminHandoffCommand() = %q, %q", caseID, message)
	}
	caseID, message, ok = parseAdminHandoffCommand("/handoff_done Ticket-123456")
	if !ok || caseID != "Ticket-123456" || message != "" {
		t.Fatalf("parseAdminHandoffCommand() without custom message = %q, %q, %t", caseID, message, ok)
	}
}

func TestAdminHandoffReference(t *testing.T) {
	id := uuid.MustParse("12345678-abcd-4abc-8abc-123456789012")
	if got, want := (store.AdminHandoff{ID: id, TicketNumber: 123456}).Reference(), "Ticket-123456"; got != want {
		t.Fatalf("Reference() = %q, want %q", got, want)
	}
	if got, want := (store.AdminHandoff{ID: id}).Reference(), "CMH-12345678"; got != want {
		t.Fatalf("legacy Reference() = %q, want %q", got, want)
	}
}

func TestAdminHandoffUnauthorizedMessageIncludesSenderID(t *testing.T) {
	got := adminHandoffUnauthorizedMessage("987654321|username")
	if !strings.Contains(got, "987654321") {
		t.Fatalf("unauthorized message = %q, want sender ID", got)
	}
}

func TestAdminHandoffListPagesSplitsAndKeepsActions(t *testing.T) {
	handoffs := make([]store.AdminHandoff, adminHandoffsPerMessage+1)
	for i := range handoffs {
		handoffs[i] = store.AdminHandoff{
			ID:            uuid.New(),
			TicketNumber:  int64(i + 1),
			SourceChannel: "facebook",
			SourceChatID:  "customer",
			Priority:      "high",
			Service:       "Proxy PrivateV4",
			Identifiers:   []string{"191.101.251.120", "customer@example.com"},
			Summary:       strings.Repeat("nội dung xử lý ", 30),
		}
	}

	pages := adminHandoffListPages(handoffs)
	if len(pages) != 2 {
		t.Fatalf("page count = %d, want 2", len(pages))
	}
	if len(pages[0].rows) != adminHandoffsPerMessage || len(pages[1].rows) != 1 {
		t.Fatalf("button rows = %d, %d", len(pages[0].rows), len(pages[1].rows))
	}
	for _, page := range pages {
		if len([]rune(page.text)) >= telegramMaxMessageLen {
			t.Fatalf("page exceeds Telegram limit: %d", len([]rune(page.text)))
		}
	}
	if got := pages[0].rows[0][1].Text; got != "Manual" {
		t.Fatalf("manual action label = %q, want Manual", got)
	}
	if got := pages[0].rows[0][1].CallbackData; !strings.HasPrefix(got, "ah:manual:") {
		t.Fatalf("manual callback = %q", got)
	}
	if !strings.Contains(pages[0].text, "Ưu tiên: Cao") || !strings.Contains(pages[0].text, "Dịch vụ: Proxy PrivateV4") || !strings.Contains(pages[0].text, "customer@example.com") {
		t.Fatalf("list item missing persisted details: %s", pages[0].text)
	}
}

func TestAdminHandoffManualState(t *testing.T) {
	channel := &Channel{}
	handoffID := uuid.New()
	channel.beginAdminHandoffManual("-5570031702", "1602998514", handoffID)

	raw, ok := channel.adminHandoffManual.Load(adminHandoffManualKey("-5570031702", "1602998514"))
	if !ok {
		t.Fatal("manual state was not stored")
	}
	pending, ok := raw.(pendingAdminHandoffManual)
	if !ok || pending.HandoffID != handoffID || time.Until(pending.ExpiresAt) <= 0 {
		t.Fatalf("manual state = %#v", raw)
	}
}
