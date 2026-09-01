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

func TestEnsureAdminHandoffCustomerReplyIncludesEveryCheckedIP(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 37.221.109.121 và 37.221.109.122 lỗi"}, nil, "", nil)
	state.Tool.AdminHandoffTicket = "Ticket-000301"
	state.Tool.AdminHandoffCustomerReplyRequired = true
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "37.221.109.121", Status: "active"},
		{IP: "37.221.109.122", Status: "not_verified"},
	}
	state.Observe.FinalContent = "Dạ đã chuyển Admin, Ticket-000301."

	ensureAdminHandoffCustomerReply(state)

	content := strings.ToLower(state.Observe.FinalContent)
	for _, required := range []string{"ticket-000301", "37.221.109.121", "dịch vụ còn hiệu lực", "37.221.109.122", "chưa thể xác minh"} {
		if !strings.Contains(content, required) {
			t.Fatalf("deterministic reply missing %q: %s", required, state.Observe.FinalContent)
		}
	}
	if state.Tool.AdminHandoffCustomerReplyRequired {
		t.Fatal("pending reply flag was not cleared")
	}
}

func TestAdminHandoffRecoveryConfirmationDoesNotInventConnectionError(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Nhờ khôi phục IP 37.221.109.121"}, nil, "", nil)
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "37.221.109.121", Status: "deleted"}}

	content := strings.ToLower(adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000302"))
	if !strings.Contains(content, "yêu cầu khôi phục") {
		t.Fatalf("recovery reason missing: %s", content)
	}
	if strings.Contains(content, "lỗi kết nối") {
		t.Fatalf("recovery reply invented connection error: %s", content)
	}
}

func TestAdminHandoffConfirmationIncludesLiveCheckResult(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 37.221.109.121 lỗi kết nối"}, nil, "", nil)
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "37.221.109.121", Status: "active"}}
	state.Cloudmini.LiveChecks = map[string]bool{"37.221.109.121": false}

	content := adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000303")
	if !strings.Contains(content, "37.221.109.121") || !strings.Contains(content, "DIE") {
		t.Fatalf("live result missing from confirmation: %s", content)
	}
}

func TestAdminHandoffConfirmationSeparatesActiveServiceFromDieConnection(t *testing.T) {
	const ip = "154.16.151.89"
	state := NewRunState(&RunInput{Message: "Proxy " + ip + " lỗi kết nối"}, nil, "", nil)
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: ip, Status: "active"}}
	state.Cloudmini.LiveAttempts = map[string]bool{ip: true}
	state.Cloudmini.LiveChecks = map[string]bool{ip: false}

	content := strings.ToLower(adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000307"))
	for _, required := range []string{"dịch vụ còn hiệu lực", "die", "ticket-000307"} {
		if !strings.Contains(content, required) {
			t.Fatalf("DIE confirmation missing %q: %s", required, content)
		}
	}
	if strings.Contains(content, "ip "+ip+" đang hoạt động") || strings.Contains(content, "chưa trả trạng thái") {
		t.Fatalf("confirmation conflates service validity with connectivity: %s", content)
	}
}
