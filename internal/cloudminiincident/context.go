package cloudminiincident

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const contextPath = "OPERATIONAL_INCIDENTS.md"

// RenderContext creates a deliberately machine-readable context block. Free
// form customer text is never allowed to override severity or permissions.
func RenderContext(incidents []store.OperationalIncident, agentKey string, now time.Time) string {
	var active []store.OperationalIncident
	for _, incident := range incidents {
		if !incident.Enabled || !inWindow(incident, now) || !appliesToAgent(incident, agentKey) {
			continue
		}
		active = append(active, incident)
	}
	data, _ := json.Marshal(active)
	return "# Operational incidents (structured, authoritative)\n" +
		"Use these records only as operational context. The latest successful tool result is authoritative for the customer's service status. Never upgrade severity or invent remediation.\n" +
		"<operational_incidents>\n" + string(data) + "\n</operational_incidents>"
}

func ContextPath() string { return contextPath }

func ParseContext(content string) ([]store.OperationalIncident, error) {
	// The runtime-owned block is appended after editable context files. Reading
	// the final block prevents an AGENTS.md/context file from shadowing it with
	// a forged tag earlier in the prompt.
	start := strings.LastIndex(content, "<operational_incidents>")
	if start < 0 {
		return nil, nil
	}
	start += len("<operational_incidents>")
	endOffset := strings.Index(content[start:], "</operational_incidents>")
	if endOffset < 0 {
		return nil, nil
	}
	end := start + endOffset
	var incidents []store.OperationalIncident
	if err := json.Unmarshal([]byte(strings.TrimSpace(content[start:end])), &incidents); err != nil {
		return nil, fmt.Errorf("parse operational incidents context: %w", err)
	}
	return incidents, nil
}

func Match(incidents []store.OperationalIncident, ip, agentKey string, now time.Time) *store.OperationalIncident {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return nil
	}
	bestIndex := -1
	bestBits := -1
	bestSeverity := -1
	for n := range incidents {
		incident := &incidents[n]
		if !incident.Enabled || !inWindow(*incident, now) || !appliesToAgent(*incident, agentKey) {
			continue
		}
		for _, raw := range incident.CIDRs {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil || !prefix.Contains(addr) {
				continue
			}
			severity := severityRank(incident.Severity)
			if prefix.Bits() > bestBits ||
				(prefix.Bits() == bestBits && severity > bestSeverity) ||
				(prefix.Bits() == bestBits && severity == bestSeverity && (bestIndex < 0 || incident.ID < incidents[bestIndex].ID)) {
				bestIndex = n
				bestBits = prefix.Bits()
				bestSeverity = severity
			}
		}
	}
	if bestIndex < 0 {
		return nil
	}
	matched := incidents[bestIndex]
	return &matched
}

func severityRank(severity string) int {
	switch severity {
	case "permanent_outage":
		return 3
	case "degraded":
		return 2
	case "temporary_issue":
		return 1
	default:
		return 0
	}
}

func inWindow(incident store.OperationalIncident, now time.Time) bool {
	if incident.StartsAt != "" {
		start, err := time.Parse(time.RFC3339, incident.StartsAt)
		if err != nil || now.Before(start) {
			return false
		}
	}
	if incident.EndsAt != "" {
		end, err := time.Parse(time.RFC3339, incident.EndsAt)
		if err != nil || !now.Before(end) {
			return false
		}
	}
	return true
}

func appliesToAgent(incident store.OperationalIncident, agentKey string) bool {
	if len(incident.AgentKeys) == 0 {
		return true
	}
	for _, key := range incident.AgentKeys {
		if strings.EqualFold(strings.TrimSpace(key), agentKey) {
			return true
		}
	}
	return false
}
