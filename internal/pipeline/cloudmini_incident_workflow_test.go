package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func scheduledIncidentState() *RunState {
	s := defaultState()
	s.Input.Message = "bạn đổi ip mới giúp mình nhé 103.161.96.134 103.180.139.126"
	s.Cloudmini.RequestIPs = []string{"103.161.96.134", "103.180.139.126"}
	s.Cloudmini.IncidentsByIP = make(map[string]store.OperationalIncident)
	for _, ip := range s.Cloudmini.RequestIPs {
		s.Cloudmini.ServiceFacts = append(s.Cloudmini.ServiceFacts, CloudminiServiceFact{IP: ip, Plan: "PrivateV4", Status: "active", AccountEmailMatches: true})
		s.Cloudmini.IncidentsByIP[ip] = store.OperationalIncident{ID: "notice", Severity: "scheduled_outage", EventAt: "2099-11-30T00:00:00Z", AllowsAdminHandoff: true, ApprovedContent: "Dải sẽ ngưng ngày 30/11. Cloudmini hỗ trợ thay proxy miễn phí hoặc xem xét hoàn tiền."}
	}
	return s
}

func TestScheduledIncidentForcesHandoffBeforePaidChangeAdvice(t *testing.T) {
	state := scheduledIncidentState()
	calls := 0
	stage := NewThinkStage(&PipelineDeps{Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		BuildFilteredTools: func(*RunState) ([]providers.ToolDefinition, error) {
			return []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: "escalate_to_admin"}}, {Function: &providers.ToolFunctionSchema{Name: "read_file"}}}, nil
		},
		CallLLM: func(_ context.Context, _ *RunState, req providers.ChatRequest) (*providers.ChatResponse, error) {
			calls++
			if len(req.Tools) != 1 || req.Tools[0].Function.Name != "escalate_to_admin" || req.Options[providers.OptToolChoice] != "required" {
				t.Fatalf("handoff was not forced: %#v", req)
			}
			return &providers.ChatResponse{ToolCalls: []providers.ToolCall{{ID: "handoff", Name: "escalate_to_admin"}}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || stage.Result() != Continue {
		t.Fatalf("calls=%d result=%v", calls, stage.Result())
	}
}

func TestScheduledIncidentNaturalReplyAndLiveDoNotRemoveRemedies(t *testing.T) {
	state := scheduledIncidentState()
	state.Tool.AdminHandoffTicket = "Ticket-123456"
	state.Cloudmini.LiveChecks = map[string]bool{"103.161.96.134": true, "103.180.139.126": true}
	good := "Dạ IP 103.161.96.134 còn hiệu lực; IP 103.180.139.126 còn hiệu lực. Hai dải này dự kiến ngừng dịch vụ ngày 30/11. Bên em có phương án thay proxy không mất phí hoặc xem xét hoàn phần tiền, Admin sẽ kiểm tra theo Ticket-123456 ạ."
	if cloudminiResponseViolatesGuard(state, good) {
		t.Fatal("faithful paraphrase rejected")
	}
	for _, bad := range []string{
		"Hai IP đang LIVE, phí thay thế là 20.000đ/IP.",
		strings.Replace(good, "không mất phí", "20.000đ/IP", 1),
		strings.Replace(good, "dự kiến ngừng", "đã ngừng", 1),
	} {
		if !cloudminiResponseViolatesGuard(state, bad) {
			t.Fatalf("unsafe reply accepted: %s", bad)
		}
	}
	if len(cloudminiRequiredIncidentMessages(state)) != 1 {
		t.Fatal("LIVE suppressed advance notice")
	}
}

func TestIncidentHandoffRequiresVerifiedScopeAndRequest(t *testing.T) {
	state := scheduledIncidentState()
	if !cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("explicit verified request not routed")
	}
	state.Input.Message = "Đổi IP giúp mình được không?"
	if !cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("polite explicit request not routed")
	}
	state.Cloudmini.ServiceFacts[1].AccountEmailMatches = false
	if cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("unverified IP routed")
	}
	state.Cloudmini.ServiceFacts[1].AccountEmailMatches = true
	state.Input.Message = "Chính sách đổi IP này thế nào?"
	if cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("general policy question created ticket")
	}
	state.Input.Message = "đổi IP giúp mình"
	i := state.Cloudmini.IncidentsByIP[state.Cloudmini.RequestIPs[1]]
	i.AllowsAdminHandoff = false
	state.Cloudmini.IncidentsByIP[state.Cloudmini.RequestIPs[1]] = i
	if cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("handoff prohibition ignored")
	}
}

func TestIncidentRemedyDoesNotConfuseCompleteOutageWithRefund(t *testing.T) {
	i := store.OperationalIncident{ApprovedContent: "Proxy đã ngưng hoạt động hoàn toàn."}
	if cloudminiIncidentRemedyMissing(i, "Dải này đã ngừng hoạt động.") {
		t.Fatal("hoàn toàn mistaken for refund")
	}
	i.ApprovedContent = "Thay proxy miễn phí hoặc xem xét hoàn tiền."
	if cloudminiIncidentRemedyMissing(i, "IP 103.161.96.134 đang active, thay proxy miễn phí hoặc xem xét hoàn tiền.") {
		t.Fatal("IP followed by đang mistaken for money")
	}
	if !cloudminiIncidentRemedyMissing(i, "Thay proxy miễn phí, thu thêm 50.000đ.") {
		t.Fatal("invented price accepted")
	}
	if !cloudminiIncidentRemedyMissing(i, "Thay proxy miễn phí, dải ngưng hoạt động hoàn toàn.") {
		t.Fatal("refund omitted")
	}
}

func TestScheduledNoticeWordingBeforeAndAfterEvent(t *testing.T) {
	i := store.OperationalIncident{EventAt: "2026-11-30T00:00:00+07:00"}
	before := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	future := "Dải sẽ ngưng dịch vụ ngày 30/11."
	if cloudminiScheduledNoticeMissingAt(i, future, before) {
		t.Fatal("future notice rejected")
	}
	if !cloudminiScheduledNoticeMissingAt(i, future, after) {
		t.Fatal("past event described as future")
	}
	if cloudminiScheduledNoticeMissingAt(i, "Theo lịch dự kiến ngừng ngày 30 tháng 11, cần Admin xác nhận tình trạng thực tế.", after) {
		t.Fatal("unconfirmed past schedule rejected")
	}
	if !cloudminiScheduledNoticeMissingAt(i, "Dải sẽ ngưng ngày 29/11.", before) {
		t.Fatal("event date shifted to UTC yesterday")
	}
	if !cloudminiScheduledNoticeMissingAt(i, "Dải đã ngưng ngày 30/11.", after) {
		t.Fatal("schedule treated as proof of outage")
	}
}

func TestIncidentHandoffDoesNotDuplicateVerifiedPendingTicket(t *testing.T) {
	state := scheduledIncidentState()
	state.Tool.AdminHandoffStatuses = map[string]AdminHandoffStatusFact{"ticket-123456": {TicketID: "Ticket-123456", Status: "pending", RelatedIPs: append([]string(nil), state.Cloudmini.RequestIPs...)}}
	if cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("verified pending ticket ignored")
	}
}

func TestResolvedLiveConnectivityDoesNotForceIncidentHandoff(t *testing.T) {
	state := scheduledIncidentState()
	state.Input.Message = "Proxy đang lỗi kết nối"
	state.Cloudmini.LiveChecks = make(map[string]bool)
	for _, ip := range state.Cloudmini.RequestIPs {
		i := state.Cloudmini.IncidentsByIP[ip]
		i.Severity = "temporary_issue"
		state.Cloudmini.IncidentsByIP[ip] = i
		state.Cloudmini.LiveChecks[ip] = true
	}
	if cloudminiNeedsIncidentAdminReview(state) {
		t.Fatal("LIVE temporary issue bypassed troubleshooting")
	}
}
