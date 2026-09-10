package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOperationalIncidentUnifiedFormContract(t *testing.T) {
	body := `{"name":"FPT","service":"PrivateV4","cidrs":["103.161.96.0/24"],"severity":"scheduled_outage","starts_at":"2026-09-08T00:00:00+07:00","event_at":"2026-11-30T00:00:00+07:00","approved_content":"Thay proxy miễn phí hoặc xem xét hoàn tiền.","customer_message":"old text","allowed_claims":["old claim"],"enabled":true,"requires_live_check":false,"allows_admin_handoff":true}`
	var incident store.OperationalIncident
	r := httptest.NewRequest("POST", "/v1/cloudmini/operational-incidents", strings.NewReader(body))
	w := httptest.NewRecorder()
	if err := decodeIncident(w, r, &incident); err != nil {
		t.Fatal(err)
	}
	if err := incident.Validate(); err != nil {
		t.Fatal(err)
	}
	if incident.CustomerMessage != "" || len(incident.AllowedClaims) != 0 {
		t.Fatal("two sources of truth persisted")
	}
	encoded, err := json.Marshal(incident)
	if err != nil {
		t.Fatal(err)
	}
	var got store.OperationalIncident
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.EventAt != "2026-11-30T00:00:00+07:00" || got.ApprovedContent != incident.ApprovedContent {
		t.Fatalf("contract lost event or guidance: %s", encoded)
	}
}

func TestDecodeOperationalIncidentRejectsUnknownAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"incident","unknown":true}`},
		{name: "trailing object", body: `{"name":"incident"} {"name":"second"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/v1/cloudmini/operational-incidents", strings.NewReader(tt.body))
			var incident store.OperationalIncident
			if err := decodeIncident(recorder, request, &incident); err == nil {
				t.Fatal("invalid request was accepted")
			}
			if recorder.Code != 400 {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestDecodeOperationalIncidentAcceptsOneObject(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/v1/cloudmini/operational-incidents", strings.NewReader(`{
		"name":"Michigan temporary issue",
		"service":"PrivateV4",
		"cidrs":["37.221.109.0/24"],
		"severity":"temporary_issue",
		"enabled":true,
		"requires_live_check":true,
		"allows_admin_handoff":false
	}`))
	var incident store.OperationalIncident
	if err := decodeIncident(recorder, request, &incident); err != nil {
		t.Fatalf("decodeIncident: %v, body = %s", err, recorder.Body.String())
	}
	if incident.Name != "Michigan temporary issue" || len(incident.CIDRs) != 1 {
		t.Fatalf("incident = %#v", incident)
	}
}
