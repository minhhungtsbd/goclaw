package pipeline

import (
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
