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

func TestCloudminiAdminHandoffOfferFallbackKeepsVerifiedFacts(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 94.103.56.231 connection proxy timeout"}, nil, "", nil)
	state.Cloudmini.RequestIPs = []string{"94.103.56.231"}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{
		IP: "94.103.56.231", Plan: "PrivateV4", PlanFamily: "private_v4", Status: "active", AccountEmailMatches: true,
	}}
	state.Cloudmini.LiveChecks = map[string]bool{"94.103.56.231": true}

	reply, ok := cloudminiAdminHandoffOfferResponse(state)
	if !ok {
		t.Fatal("LIVE connection case did not receive an Admin-review offer")
	}
	for _, required := range []string{"94.103.56.231", "còn hiệu lực", "LIVE", "có đồng ý để em chuyển"} {
		if !strings.Contains(strings.ToLower(reply), strings.ToLower(required)) {
			t.Fatalf("offer missing %q: %q", required, reply)
		}
	}
	if strings.Contains(strings.ToLower(reply), "đã chuyển") || strings.Contains(reply, "Ticket-") {
		t.Fatalf("offer falsely confirmed a handoff: %q", reply)
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
	if !strings.Contains(content, "hiện tại không còn gắn với dịch vụ nào") ||
		strings.Contains(content, "bị xóa") || strings.Contains(content, "bị xoá") {
		t.Fatalf("deleted service must use the neutral customer wording: %s", content)
	}
}

func TestCloudminiCustomerStatusWordingByServiceState(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "active", want: "có dịch vụ còn hiệu lực trên hệ thống"},
		{status: "expired", want: "đã hết hạn theo kết quả kiểm tra hiện tại"},
		{status: "deleted", want: "hiện tại không còn gắn với dịch vụ nào"},
		{status: "not_verified", want: "hiện chưa thể xác minh theo thông tin tài khoản"},
		{status: "unavailable", want: "hiện chưa thể xác minh do công cụ kiểm tra chưa trả dữ liệu"},
		{status: "unknown", want: "chưa thể xác định trạng thái dịch vụ"},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			if got := cloudminiFactStatusText(tc.status); got != tc.want {
				t.Fatalf("cloudminiFactStatusText(%q) = %q, want %q", tc.status, got, tc.want)
			}
			state := NewRunState(&RunInput{Message: "Nhờ kiểm tra IP 31.57.203.88"}, nil, "", nil)
			state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "31.57.203.88", Status: tc.status}}
			state.Tool.AdminHandoffTicket = "Ticket-000383"
			state.Tool.AdminHandoffCustomerReplyRequired = true
			reply := adminHandoffCustomerConfirmationWithFacts(state, state.Tool.AdminHandoffTicket)
			if !strings.Contains(reply, tc.want) {
				t.Fatalf("status %q reply = %q, want phrase %q", tc.status, reply, tc.want)
			}
			if adminHandoffResponseViolatesGuard(state, reply) {
				t.Fatalf("status %q canonical reply failed guard: %q", tc.status, reply)
			}
		})
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

func TestAdminHandoffConfirmationGroupsSameStatusIPsIntoOneClause(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Admin chuyển những proxy trên sang dải khác giúp em, email customer@example.com"}, nil, "", nil)
	ips := []string{"103.31.210.191", "103.31.210.14", "103.31.210.183", "103.31.210.19", "103.31.210.218"}
	for _, ip := range ips {
		state.Cloudmini.ServiceFacts = append(state.Cloudmini.ServiceFacts, CloudminiServiceFact{IP: ip, Status: "active", AccountEmailMatches: true})
	}
	state.Tool.AdminHandoffTicket = "Ticket-000342"

	reply := adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000342")
	if strings.Count(reply, "có dịch vụ còn hiệu lực") != 1 {
		t.Fatalf("same-status IPs must share one explanation clause: %q", reply)
	}
	for _, ip := range ips {
		if !strings.Contains(reply, "IP "+ip) {
			t.Fatalf("grouped reply missing %s: %q", ip, reply)
		}
	}
	if !strings.Contains(reply, "Ticket-000342") {
		t.Fatalf("grouped reply missing ticket: %q", reply)
	}
	if adminHandoffResponseViolatesGuard(state, reply) {
		t.Fatalf("grouped reply must still satisfy the per-IP guard: %q", reply)
	}
}

func TestCloudminiGroupingKeepsDifferentFactsSeparate(t *testing.T) {
	state := NewRunState(&RunInput{}, nil, "", nil)
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "192.0.2.1", Plan: "PrivateV4", Status: "active", AccountEmailMatches: true},
		{IP: "192.0.2.2", Plan: "PrivateV4", Status: "active", AccountEmailMatches: true},
		{IP: "192.0.2.3", Plan: "PrivateV4", Status: "active", AccountEmailMatches: true},
		{IP: "192.0.2.4", Plan: "BudgetV4", Status: "active", AccountEmailMatches: true},
		{IP: "192.0.2.5", Plan: "PrivateV4", Status: "active"},
	}
	state.Cloudmini.LiveChecks = map[string]bool{"192.0.2.1": true, "192.0.2.2": true, "192.0.2.3": false}
	clauses := cloudminiGroupedFactClauses(state)
	if len(clauses) != 4 {
		t.Fatalf("distinct outcomes merged: %#v", clauses)
	}
	if !strings.Contains(clauses[0], "192.0.2.1, IP 192.0.2.2") || !strings.Contains(clauses[0], "LIVE") {
		t.Fatalf("equal LIVE outcomes not grouped: %s", clauses[0])
	}
	if !strings.Contains(clauses[1], "DIE") || strings.Contains(clauses[1], "LIVE") {
		t.Fatalf("DIE inherited LIVE: %s", clauses[1])
	}
	if !strings.Contains(clauses[2], "BudgetV4") || strings.Contains(clauses[2], "kiểm tra kết nối") {
		t.Fatalf("different plan or unchecked connection merged: %s", clauses[2])
	}
	if strings.Contains(clauses[3], "đã xác minh") {
		t.Fatalf("unverified fact inherited verification: %s", clauses[3])
	}
}

func TestAdminHandoffConfirmationGroupsMixedStatusesSeparately(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục các IP này, email customer@example.com"}, nil, "", nil)
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "37.221.109.121", Status: "active", AccountEmailMatches: true},
		{IP: "37.221.109.122", Status: "active", AccountEmailMatches: true},
		{IP: "37.221.109.123", Status: "not_verified"},
	}
	state.Tool.AdminHandoffTicket = "Ticket-000343"

	reply := adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000343")
	if strings.Count(reply, "có dịch vụ còn hiệu lực") != 1 || strings.Count(reply, "chưa thể xác minh") != 1 {
		t.Fatalf("each distinct status must appear exactly once: %q", reply)
	}
	for _, ip := range []string{"37.221.109.121", "37.221.109.122", "37.221.109.123"} {
		if !strings.Contains(reply, "IP "+ip) {
			t.Fatalf("grouped reply missing %s: %q", ip, reply)
		}
	}
	if adminHandoffResponseViolatesGuard(state, reply) {
		t.Fatalf("grouped mixed-status reply must satisfy the guard: %q", reply)
	}
}
