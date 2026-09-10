package cloudminiincident

import (
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"testing"
	"time"
)

func TestAdvanceNoticeVisibleBeforeEventButNotBeforePublication(t *testing.T) {
	now := time.Date(2026, 9, 10, 1, 32, 0, 0, time.UTC)
	item := store.OperationalIncident{ID: "fpt", Enabled: true, Severity: "scheduled_outage", StartsAt: "2026-09-08T00:00:00Z", EventAt: "2026-11-30T00:00:00Z", CIDRs: []string{"103.161.96.0/24", "103.180.139.0/24"}, AgentKeys: []string{"linh-nhi-support-lead"}, ApprovedContent: "Thay proxy miễn phí hoặc xem xét hoàn tiền."}
	parsed, err := ParseContext(RenderContext([]store.OperationalIncident{item}, "linh-nhi-support-lead", now))
	if err != nil || len(parsed) != 1 {
		t.Fatalf("advance notice missing: %#v %v", parsed, err)
	}
	for _, ip := range []string{"103.161.96.134", "103.180.139.126"} {
		if Match(parsed, ip, "linh-nhi-support-lead", now) == nil {
			t.Fatalf("IP not matched: %s", ip)
		}
	}
	if Match(parsed, "103.161.97.134", "linh-nhi-support-lead", now) != nil {
		t.Fatal("unrelated subnet matched")
	}
	if Match(parsed, "103.161.96.134", "other-agent", now) != nil {
		t.Fatal("agent isolation lost")
	}
	item.StartsAt = "2026-09-11T00:00:00Z"
	parsed, err = ParseContext(RenderContext([]store.OperationalIncident{item}, "linh-nhi-support-lead", now))
	if err != nil || len(parsed) != 0 {
		t.Fatal("unpublished notice visible")
	}
}
