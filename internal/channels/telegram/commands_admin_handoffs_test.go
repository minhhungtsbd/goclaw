package telegram

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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
