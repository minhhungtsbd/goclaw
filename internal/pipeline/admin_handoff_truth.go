package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

var adminHandoffTicketPattern = regexp.MustCompile(`(?i)\bTicket-\d{6,}\b`)
var inventedLegacyHandoffPattern = regexp.MustCompile(`(?i)\bCMH-[A-Z0-9]+\b`)

func recordAdminHandoffResult(state *RunState, call providers.ToolCall, messages []providers.Message) {
	if state == nil || call.Name != "escalate_to_admin" {
		return
	}
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		var result struct {
			Status   string `json:"status"`
			TicketID string `json:"ticket_id"`
		}
		if json.Unmarshal([]byte(message.Content), &result) == nil &&
			(result.Status == "sent" || result.Status == "merged") && result.TicketID != "" {
			state.Tool.AdminHandoffTicket = result.TicketID
			return
		}
		if ticket := adminHandoffTicketPattern.FindString(message.Content); ticket != "" && !message.IsError {
			state.Tool.AdminHandoffTicket = ticket
			return
		}
	}
}

func unsupportedAdminHandoffClaim(content, actualTicket string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if inventedLegacyHandoffPattern.MatchString(content) {
		return true
	}
	if ticket := adminHandoffTicketPattern.FindString(content); ticket != "" {
		return actualTicket == "" || !strings.EqualFold(ticket, actualTicket)
	}
	if actualTicket != "" {
		return false
	}
	lower := strings.ToLower(content)
	mentionsDestination := strings.Contains(lower, "admin") || strings.Contains(lower, "kỹ thuật") || strings.Contains(lower, "ky thuat")
	if !mentionsDestination {
		return false
	}
	return strings.Contains(lower, "đã chuyển") || strings.Contains(lower, "da chuyen") ||
		strings.Contains(lower, "đã gửi yêu cầu") || strings.Contains(lower, "da gui yeu cau") ||
		strings.Contains(lower, "sẽ chuyển") || strings.Contains(lower, "se chuyen") ||
		strings.Contains(lower, "mã tiếp nhận") || strings.Contains(lower, "mã theo dõi")
}
