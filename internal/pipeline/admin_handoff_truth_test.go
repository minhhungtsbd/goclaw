package pipeline

import (
	"strings"
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
	if unsupportedAdminHandoffClaim("Đã chuyển Admin, mã Ticket-000243", state) {
		t.Fatal("real ticket was rejected")
	}
}

func TestUnsupportedAdminHandoffClaimRejectsLegacyCMHWithoutTool(t *testing.T) {
	if !unsupportedAdminHandoffClaim("Đã chuyển Kỹ thuật. Mã tiếp nhận CMH-EF915885", NewRunState(&RunInput{}, nil, "", nil)) {
		t.Fatal("invented legacy ticket claim was not rejected")
	}
}

func TestRecordAdminHandoffResultKeepsPendingReplyAcrossMergedRetry(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	call := providers.ToolCall{Name: "escalate_to_admin"}
	recordAdminHandoffResult(state, call, []providers.Message{{
		Role: "tool", Content: `{"status":"sent","ticket_id":"Ticket-000244"}`,
	}})
	recordAdminHandoffResult(state, call, []providers.Message{{
		Role: "tool", Content: `{"status":"merged","ticket_id":"Ticket-000244"}`,
	}})
	if !state.Tool.AdminHandoffCustomerReplyRequired {
		t.Fatal("merged retry cleared the pending customer confirmation")
	}
}

func TestRecordAdminHandoffResultMergedTicketDoesNotCreateSecondConfirmation(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	call := providers.ToolCall{Name: "escalate_to_admin"}
	recordAdminHandoffResult(state, call, []providers.Message{{
		Role: "tool", Content: `{"status":"merged","ticket_id":"Ticket-000244"}`,
	}})
	if state.Tool.AdminHandoffTicket != "Ticket-000244" {
		t.Fatalf("ticket = %q", state.Tool.AdminHandoffTicket)
	}
	if state.Tool.AdminHandoffCustomerReplyRequired {
		t.Fatal("merged ticket unexpectedly requested a second automatic customer confirmation")
	}
}

func TestAdminHandoffTruthInstructionUsesExistingTicket(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	state.Tool.AdminHandoffTicket = "Ticket-000245"
	instruction := adminHandoffTruthInstruction(state, "")
	if !strings.Contains(instruction, "Ticket-000245") || strings.Contains(instruction, "No Admin handoff ticket") {
		t.Fatalf("instruction = %q", instruction)
	}
}

func TestRecordAdminHandoffStatusResultAcceptsVerifiedHistoricalTicket(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	recordAdminHandoffStatusResult(state, providers.ToolCall{Name: "admin_handoff_status"}, []providers.Message{{
		Role: "tool", Content: `{"ticket_id":"Ticket-000282","status":"dismissed","service":"Proxy","related_ips":["103.239.67.131"]}`,
	}})
	fact, ok := adminHandoffVerifiedStatus(state, "Ticket-000282")
	if !ok || fact.Status != "dismissed" || len(fact.RelatedIPs) != 1 {
		t.Fatalf("fact = %#v, ok=%v", fact, ok)
	}
	if unsupportedAdminHandoffClaim("Mã Ticket-000282 đã đóng và không còn chờ xử lý.", state) {
		t.Fatal("verified dismissed status was rejected")
	}
	if !unsupportedAdminHandoffClaim("Mã Ticket-000282 vẫn đang chờ xử lý.", state) {
		t.Fatal("contradictory pending claim was accepted")
	}
}

func TestAdminHandoffStatusTruthBranches(t *testing.T) {
	tests := []struct {
		status  string
		valid   string
		invalid string
	}{
		{status: "pending", valid: "Ticket-000282 vẫn đang chờ xử lý.", invalid: "Ticket-000282 đã hoàn tất."},
		{status: "completed", valid: "Ticket-000282 đã hoàn tất.", invalid: "Ticket-000282 vẫn đang chờ xử lý."},
		{status: "dismissed", valid: "Ticket-000282 đã đóng và không còn chờ xử lý.", invalid: "Ticket-000282 vẫn đang chờ xử lý."},
		{status: "delivery_failed", valid: "Ticket-000282 trước đó không gửi được đến Admin.", invalid: "Ticket-000282 vẫn đang chờ xử lý."},
		{status: "unavailable", valid: "Chưa thể xác minh trạng thái Ticket-000282 trong cuộc trò chuyện này.", invalid: "Ticket-000282 vẫn đang chờ xử lý."},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			state := NewRunState(&RunInput{}, nil, "", nil)
			state.Tool.AdminHandoffStatuses = map[string]AdminHandoffStatusFact{
				"ticket-000282": {TicketID: "Ticket-000282", Status: tt.status},
			}
			if unsupportedAdminHandoffClaim(tt.valid, state) {
				t.Fatalf("valid response rejected: %q", tt.valid)
			}
			if !unsupportedAdminHandoffClaim(tt.invalid, state) {
				t.Fatalf("contradictory response accepted: %q", tt.invalid)
			}
			if got := adminHandoffStatusFallback(state, "Ticket-000282"); !strings.Contains(got, "Ticket-000282") {
				t.Fatalf("fallback omitted ticket: %q", got)
			}
		})
	}
}

func TestAdminHandoffTruthChecksEveryMentionedTicket(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	state.Tool.AdminHandoffTicket = "Ticket-000300"
	content := "Yêu cầu mới là Ticket-000300; ticket cũ Ticket-000282 vẫn đang chờ xử lý."
	if got := adminHandoffTicketNeedingCheck(state, content); got != "Ticket-000282" {
		t.Fatalf("ticket needing check = %q", got)
	}
	if !unsupportedAdminHandoffClaim(content, state) {
		t.Fatal("unchecked second ticket was accepted")
	}
}
