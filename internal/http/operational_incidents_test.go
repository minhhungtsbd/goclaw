package http

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
