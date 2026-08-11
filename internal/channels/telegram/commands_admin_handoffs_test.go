package telegram

import (
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
	if _, _, ok := parseAdminHandoffCommand("/handoff_done CMH-1234ABCD"); ok {
		t.Fatal("parseAdminHandoffCommand() accepted missing customer message")
	}
}

func TestHandoffReference(t *testing.T) {
	id := uuid.MustParse("12345678-abcd-4abc-8abc-123456789012")
	if got, want := handoffReference(id), "CMH-12345678"; got != want {
		t.Fatalf("handoffReference() = %q, want %q", got, want)
	}
}
