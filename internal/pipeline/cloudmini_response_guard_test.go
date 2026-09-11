package pipeline

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestAdminHandoffReplyRequiresServiceExplanation(t *testing.T) {
	state := &RunState{}
	state.Tool.AdminHandoffCustomerReplyRequired = true
	state.Tool.AdminHandoffTicket = "Ticket-000294"
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "37.221.109.121", Status: "not_verified"}}
	if !adminHandoffResponseViolatesGuard(state, "Đã chuyển Admin, mã Ticket-000294") {
		t.Fatal("ticket-only reply should be rejected")
	}
	if adminHandoffResponseViolatesGuard(state, "Em chưa thể xác minh dịch vụ. Đã chuyển Admin, mã Ticket-000294") {
		t.Fatal("reply with status explanation should pass")
	}
}

func TestAdminHandoffReplyAcceptsActiveServiceWithDieConnection(t *testing.T) {
	const ip = "154.16.151.89"
	state := &RunState{}
	state.Tool.AdminHandoffCustomerReplyRequired = true
	state.Tool.AdminHandoffTicket = "Ticket-000307"
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: ip, Status: "active"}}
	state.Cloudmini.LiveAttempts = map[string]bool{ip: true}
	state.Cloudmini.LiveChecks = map[string]bool{ip: false}

	reply := strings.ToLower(adminHandoffCustomerConfirmationWithFacts(state, state.Tool.AdminHandoffTicket))
	if adminHandoffResponseViolatesGuard(state, reply) {
		t.Fatalf("active-service plus DIE explanation should pass: %s", reply)
	}
}

func TestDeletedServiceGuardRequiresNeutralDetachedWording(t *testing.T) {
	state := &RunState{}
	state.Tool.AdminHandoffCustomerReplyRequired = true
	state.Tool.AdminHandoffTicket = "Ticket-000383"
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "31.57.203.88", Status: "deleted"}}

	neutral := "IP 31.57.203.88 hiện tại không còn gắn với dịch vụ nào. Đã chuyển Admin, Ticket-000383."
	if adminHandoffResponseViolatesGuard(state, neutral) {
		t.Fatalf("neutral detached wording should pass: %s", neutral)
	}
	old := "IP 31.57.203.88 đã bị xoá theo kết quả kiểm tra hiện tại. Đã chuyển Admin, Ticket-000383."
	if !adminHandoffResponseViolatesGuard(state, old) {
		t.Fatalf("old deletion claim should be rejected: %s", old)
	}
}

func TestAdminHandoffReplyRequiresEveryIPStatus(t *testing.T) {
	state := &RunState{}
	state.Tool.AdminHandoffCustomerReplyRequired = true
	state.Tool.AdminHandoffTicket = "Ticket-000295"
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "37.221.109.121", Status: "active"},
		{IP: "37.221.109.122", Status: "not_verified"},
	}
	partial := "IP 37.221.109.121 đang hoạt động. Đã chuyển Admin, Ticket-000295."
	if !adminHandoffResponseViolatesGuard(state, partial) {
		t.Fatal("partial multi-IP explanation should be rejected")
	}
	complete := "IP 37.221.109.121 đang hoạt động; IP 37.221.109.122 chưa thể xác minh. Ticket-000295."
	if adminHandoffResponseViolatesGuard(state, complete) {
		t.Fatal("complete multi-IP explanation should pass")
	}
	misassigned := "IP 37.221.109.121 đang hoạt động và chưa thể xác minh; IP 37.221.109.122 đang hoạt động. Ticket-000295."
	if !adminHandoffResponseViolatesGuard(state, misassigned) {
		t.Fatal("status text assigned to the wrong IP should be rejected")
	}
}

func TestEmailMismatchAdminReplyExplainsEveryIPInMixedRequest(t *testing.T) {
	state := &RunState{Input: &RunInput{Message: "Khôi phục các IP này"}}
	state.Cloudmini.EmailMismatch = true
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "37.221.109.121", Status: "active", AccountEmailMatches: true},
		{IP: "37.221.109.122", Status: "not_verified", AccountEmailMatches: false},
	}
	state.Tool.AdminHandoffTicket = "Ticket-000404"
	state.Tool.AdminHandoffCustomerReplyRequired = true
	reply := cloudminiEmailMismatchReply(state, state.Tool.AdminHandoffTicket)
	if adminHandoffResponseViolatesGuard(state, reply) {
		t.Fatalf("canonical mixed-IP response must explain every service fact: %q", reply)
	}
}

func TestActiveServiceRejectsPermanentOutageClaim(t *testing.T) {
	state := &RunState{Cloudmini: CloudminiState{ServiceFacts: []CloudminiServiceFact{{Status: "active"}}}}
	if !cloudminiResponseViolatesGuard(state, "Proxy đang ngưng hoạt động hoàn toàn") {
		t.Fatal("active service must not be described as permanent outage")
	}
	state.Cloudmini.IncidentsByIP = map[string]store.OperationalIncident{"37.221.109.121": {Severity: "temporary_issue"}}
	if !cloudminiResponseViolatesGuard(state, "Proxy offline") {
		t.Fatal("temporary incident must not be promoted to offline")
	}
	incident := state.Cloudmini.IncidentsByIP["37.221.109.121"]
	incident.ForbiddenClaims = []string{"hoàn tiền ngay"}
	state.Cloudmini.IncidentsByIP["37.221.109.121"] = incident
	if !cloudminiResponseViolatesGuard(state, "Khách được hoàn tiền ngay") {
		t.Fatal("forbidden incident claim should be rejected")
	}
}

func TestMatchedTemporaryIncidentMustBeExplainedWhenLiveCheckFails(t *testing.T) {
	state := &RunState{Cloudmini: CloudminiState{
		ServiceFacts: []CloudminiServiceFact{{IP: "147.189.140.177", Status: "active"}},
		LiveAttempts: map[string]bool{"147.189.140.177": true},
		IncidentsByIP: map[string]store.OperationalIncident{
			"147.189.140.177": {
				Severity: "temporary_issue", CustomerMessage: "Các dải Proxy PrivateV4 Michigan này đang gặp lỗi tạm thời.",
			},
		},
	}}
	if !cloudminiResponseViolatesGuard(state, "IP đang hoạt động nhưng live_check chưa trả kết quả.") {
		t.Fatal("reply that omits the matched temporary incident should be rejected")
	}
	if cloudminiResponseViolatesGuard(state, "Các dải Proxy PrivateV4 Michigan này đang gặp lỗi tạm thời. IP vẫn active nhưng live_check chưa trả kết quả.") {
		t.Fatal("reply that explains the matched temporary incident should pass")
	}
	if cloudminiResponseViolatesGuard(state, "Dải Michigan đang gặp lỗi tạm thời; IP vẫn active nhưng live_check chưa trả kết quả.") {
		t.Fatal("a faithful natural paraphrase should be allowed")
	}
}

func TestOperationalIncidentFallbackGroupsTraceFactsOnce(t *testing.T) {
	allIPs := []string{
		"103.155.162.54", "103.155.163.69", "103.155.163.70",
		"103.162.22.132", "103.155.162.19", "103.155.162.187",
		"103.162.22.61", "103.155.163.8", "103.155.163.20",
	}
	affected := map[string]bool{
		"103.155.162.54": true, "103.155.163.69": true, "103.155.163.70": true,
		"103.155.162.19": true, "103.155.162.187": true,
		"103.155.163.8": true, "103.155.163.20": true,
	}
	state := &RunState{}
	state.Cloudmini.IncidentsByIP = make(map[string]store.OperationalIncident, len(affected))
	for _, ip := range allIPs {
		state.Cloudmini.ServiceFacts = append(state.Cloudmini.ServiceFacts, CloudminiServiceFact{
			IP: ip, Plan: "PrivateV4", Status: "active", AccountEmailMatches: true,
		})
		if affected[ip] {
			state.Cloudmini.IncidentsByIP[ip] = store.OperationalIncident{
				Severity: "scheduled_outage", EventAt: "2026-11-30T00:00:00+07:00",
				ApprovedContent: "Các dải dự kiến ngưng ngày 30/11; có thể hỗ trợ thay miễn phí hoặc xem xét hoàn tiền.",
			}
		}
	}

	reply, ok := cloudminiOperationalIncidentResponse(state)
	if !ok {
		t.Fatal("matched trace fixture did not produce incident fallback")
	}
	for _, shared := range []string{"thuộc gói PrivateV4", "có dịch vụ còn hiệu lực", "đã xác minh đúng tài khoản"} {
		if count := strings.Count(reply, shared); count != 1 {
			t.Fatalf("shared fact %q repeated %d times: %s", shared, count, reply)
		}
	}
	for _, ip := range allIPs {
		if !strings.Contains(reply, ip) {
			t.Fatalf("fallback omitted IP %s: %s", ip, reply)
		}
	}
	incidentClause := strings.SplitN(reply, "\n\n", 2)[1]
	for ip := range affected {
		if !strings.Contains(incidentClause, ip) {
			t.Fatalf("incident scope omitted affected IP %s: %s", ip, incidentClause)
		}
	}
	for _, unaffected := range []string{"103.162.22.132", "103.162.22.61"} {
		if strings.Contains(incidentClause, unaffected) {
			t.Fatalf("incident scope included unaffected IP %s: %s", unaffected, incidentClause)
		}
	}
}

func TestSuccessfulLiveCheckSupersedesMatchedTemporaryIncident(t *testing.T) {
	state := &RunState{Cloudmini: CloudminiState{
		LiveChecks: map[string]bool{"147.189.140.177": true},
		IncidentsByIP: map[string]store.OperationalIncident{
			"147.189.140.177": {Severity: "temporary_issue", CustomerMessage: "Đang lỗi tạm thời."},
		},
	}}
	if cloudminiResponseViolatesGuard(state, "IP hiện đang LIVE và kết nối bình thường.") {
		t.Fatal("current successful LIVE result should supersede the incident message requirement")
	}
}

func TestPermanentIncidentAllowsOutageClaimForActiveSubscription(t *testing.T) {
	state := &RunState{Cloudmini: CloudminiState{
		ServiceFacts: []CloudminiServiceFact{{IP: "77.111.118.1", Status: "active"}},
		IncidentsByIP: map[string]store.OperationalIncident{
			"77.111.118.1": {Severity: "permanent_outage", CustomerMessage: "Dải này đã ngừng hoạt động hoàn toàn."},
		},
	}}
	if !cloudminiResponseViolatesGuard(state, "Dải này đã ngừng hoạt động hoàn toàn. Đây là thông báo vận hành hiện tại.") {
		t.Fatal("incident-only reply must not hide the verified active service status")
	}
	if cloudminiResponseViolatesGuard(state, "IP 77.111.118.1 có dịch vụ còn hiệu lực trên hệ thống. Dải này đã ngừng hoạt động hoàn toàn.") {
		t.Fatal("reply that separates active service from the matched permanent outage should pass")
	}
}

func TestPermanentIncidentForOneIPDoesNotAuthorizeOutageClaimForAnotherActiveIP(t *testing.T) {
	state := &RunState{}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "147.189.140.177", Status: "active"},
		{IP: "198.51.100.10", Status: "active"},
	}
	state.Cloudmini.IncidentsByIP = map[string]store.OperationalIncident{
		"147.189.140.177": {Severity: "permanent_outage", CustomerMessage: "Dải này đã ngừng hoạt động."},
	}

	if !cloudminiResponseViolatesGuard(state, "Dạ, hệ thống proxy đã ngừng hoạt động hoàn toàn.") {
		t.Fatal("one matched permanent incident authorized an outage claim covering another active IP")
	}
}

func TestResidentialVNResponseMustNotDemandNumericIP(t *testing.T) {
	state := &RunState{Cloudmini: CloudminiState{RequestHosts: []string{"ipv4-vt-04.resvn.net"}}}
	if !cloudminiResponseViolatesGuard(state, "Anh gửi em IP dạng số vì hostname chưa đủ để tra cứu nhé.") {
		t.Fatal("numeric IPv4 demand for Residential VN hostname was not rejected")
	}
	if cloudminiResponseViolatesGuard(state, "Residential VN dùng hostname ipv4-vt-04.resvn.net nên anh không cần IP dạng số ạ.") {
		t.Fatal("correct Residential VN hostname explanation was rejected")
	}
}
