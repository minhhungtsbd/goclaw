package store

import "testing"

func TestIncidentUnifiedGuidanceCompatibility(t *testing.T) {
	i := OperationalIncident{CustomerMessage: "Old notice", AllowedClaims: []string{"Approved remedy"}}
	if i.Guidance() != "Old notice\nApproved remedy" {
		t.Fatalf("legacy data lost: %q", i.Guidance())
	}
	i.ApprovedContent = "New authoritative guidance"
	if i.Guidance() != i.ApprovedContent {
		t.Fatal("legacy content overrode unified content")
	}
}

func TestIncidentExpandedSeverityValidation(t *testing.T) {
	for _, severity := range []string{"notice", "maintenance", "scheduled_outage", "resolved", "custom"} {
		i := validOperationalIncident("test")
		i.Severity = severity
		i.EventAt = "2026-11-30T00:00:00Z"
		i.SeverityLabel = "Nhà mạng đổi tuyến"
		if err := i.Validate(); err != nil {
			t.Fatalf("%s: %v", severity, err)
		}
	}
	i := validOperationalIncident("test")
	i.Severity = "custom"
	if i.Validate() == nil {
		t.Fatal("custom severity without label accepted")
	}
	i.Severity = "scheduled_outage"
	if i.Validate() == nil {
		t.Fatal("scheduled outage without event time accepted")
	}
	i.EventAt = "bad"
	if i.Validate() == nil {
		t.Fatal("malformed event time accepted")
	}
}
