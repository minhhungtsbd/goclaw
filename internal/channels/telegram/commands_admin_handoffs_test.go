package telegram

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestParseAdminHandoffCommand(t *testing.T) {
	caseID, message, ok := parseAdminHandoffCommand("/handoff_done CMH-1234ABCD Da xu ly xong")
	if !ok {
		t.Fatal("parseAdminHandoffCommand() ok = false")
	}
	if caseID != "CMH-1234ABCD" || message != "Da xu ly xong" {
		t.Fatalf("parseAdminHandoffCommand() = %q, %q", caseID, message)
	}
	caseID, message, ok = parseAdminHandoffCommand("/handoff_done CMH-1234ABCD")
	if !ok || caseID != "CMH-1234ABCD" || message != "" {
		t.Fatalf("parseAdminHandoffCommand() without custom message = %q, %q, %t", caseID, message, ok)
	}
}

func TestHandoffReference(t *testing.T) {
	id := uuid.MustParse("12345678-abcd-4abc-8abc-123456789012")
	if got, want := handoffReference(id), "CMH-12345678"; got != want {
		t.Fatalf("handoffReference() = %q, want %q", got, want)
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
			SourceChannel: "facebook",
			SourceChatID:  "customer",
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
}
