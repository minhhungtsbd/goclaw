package pipeline

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestRecordAdminHandoffResultRequiresSuccessfulTicketPayload(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	call := providers.ToolCall{Name: "escalate_to_admin"}
	recordAdminHandoffResult(state, call, []providers.Message{{
		Role: "tool", Content: `{"status":"sent","ticket_id":"Ticket-000243"}`,
	}})
	if state.Tool.AdminHandoffTicket != "Ticket-000243" {
		t.Fatalf("ticket = %q", state.Tool.AdminHandoffTicket)
	}
	if unsupportedAdminHandoffClaim("Đã chuyển Admin, mã Ticket-000243", state.Tool.AdminHandoffTicket) {
		t.Fatal("real ticket was rejected")
	}
}

func TestUnsupportedAdminHandoffClaimRejectsLegacyCMHWithoutTool(t *testing.T) {
	if !unsupportedAdminHandoffClaim("Đã chuyển Kỹ thuật. Mã tiếp nhận CMH-EF915885", "") {
		t.Fatal("invented legacy ticket claim was not rejected")
	}
}
