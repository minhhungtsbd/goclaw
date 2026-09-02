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
	if !cloudminiResponseViolatesGuard(state, "Dải Michigan đang gặp lỗi tạm thời; IP vẫn active nhưng live_check chưa trả kết quả.") {
		t.Fatal("a generic severity phrase must not replace the operator-authored customer_message")
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
