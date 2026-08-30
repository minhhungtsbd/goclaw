package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

var adminHandoffTicketPattern = regexp.MustCompile(`(?i)\bTicket-\d{6,}\b`)
var inventedLegacyHandoffPattern = regexp.MustCompile(`(?i)\bCMH-[A-Z0-9]+\b`)

func recordAdminHandoffStatusResult(state *RunState, call providers.ToolCall, messages []providers.Message) {
	if state == nil || call.Name != "admin_handoff_status" {
		return
	}
	for _, message := range messages {
		if message.Role != "tool" || message.IsError {
			continue
		}
		var result struct {
			TicketID   string   `json:"ticket_id"`
			Status     string   `json:"status"`
			Service    string   `json:"service"`
			RelatedIPs []string `json:"related_ips"`
		}
		if json.Unmarshal([]byte(message.Content), &result) != nil ||
			adminHandoffTicketPattern.FindString(result.TicketID) == "" || !knownAdminHandoffStatus(result.Status) {
			continue
		}
		if state.Tool.AdminHandoffStatuses == nil {
			state.Tool.AdminHandoffStatuses = make(map[string]AdminHandoffStatusFact)
		}
		result.TicketID = adminHandoffTicketPattern.FindString(result.TicketID)
		state.Tool.AdminHandoffStatuses[strings.ToLower(result.TicketID)] = AdminHandoffStatusFact{
			TicketID: result.TicketID, Status: result.Status, Service: result.Service,
			RelatedIPs: append([]string(nil), result.RelatedIPs...),
		}
		return
	}
}

func knownAdminHandoffStatus(status string) bool {
	switch status {
	case "pending", "completed", "dismissed", "delivery_failed", "unavailable":
		return true
	default:
		return false
	}
}

func recordAdminHandoffResult(state *RunState, call providers.ToolCall, messages []providers.Message) {
	if state == nil || call.Name != "escalate_to_admin" {
		return
	}
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		var result struct {
			Status           string `json:"status"`
			TicketID         string `json:"ticket_id"`
			CustomerNotified *bool  `json:"customer_notified"`
		}
		if json.Unmarshal([]byte(message.Content), &result) == nil &&
			(result.Status == "sent" || result.Status == "merged") && result.TicketID != "" {
			state.Tool.AdminHandoffTicket = result.TicketID
			// A duplicate/merged result must not clear a confirmation that is still
			// pending from a successful `sent` result earlier in this run.
			if result.Status == "sent" && (result.CustomerNotified == nil || !*result.CustomerNotified) {
				state.Tool.AdminHandoffCustomerReplyRequired = true
			}
			return
		}
		if ticket := adminHandoffTicketPattern.FindString(message.Content); ticket != "" && !message.IsError {
			state.Tool.AdminHandoffTicket = ticket
			state.Tool.AdminHandoffCustomerReplyRequired = true
			return
		}
	}
}

func adminHandoffCustomerReplyPending(state *RunState) bool {
	return state != nil && state.Tool.AdminHandoffCustomerReplyRequired &&
		strings.TrimSpace(state.Tool.AdminHandoffTicket) != ""
}

func adminHandoffCustomerConfirmation(ticket string) string {
	return "Dạ, em đã ghi nhận và chuyển yêu cầu đến bộ phận Admin/Kỹ thuật. Mã theo dõi của anh là " +
		strings.TrimSpace(ticket) + ". Bên em sẽ cập nhật lại anh khi có kết quả ạ."
}

// ensureAdminHandoffCustomerReply is the final delivery guard. It keeps a
// valid model-written confirmation, but replaces an empty/invalid response
// with a deterministic confirmation containing the ticket that was created.
func ensureAdminHandoffCustomerReply(state *RunState) {
	if !adminHandoffCustomerReplyPending(state) {
		return
	}
	ticket := strings.TrimSpace(state.Tool.AdminHandoffTicket)
	if !strings.Contains(strings.ToLower(state.Observe.FinalContent), strings.ToLower(ticket)) {
		state.Observe.FinalContent = adminHandoffCustomerConfirmation(ticket)
		state.Observe.FinalThinking = ""
	}
	state.Tool.AdminHandoffCustomerReplyRequired = false
}

func adminHandoffTruthInstruction(state *RunState, mentionedTicket string) string {
	if state != nil && strings.TrimSpace(state.Tool.AdminHandoffTicket) != "" {
		ticket := strings.TrimSpace(state.Tool.AdminHandoffTicket)
		return "[ADMIN HANDOFF TRUTH CHECK] An Admin handoff already succeeded in this run with ticket " + ticket +
			". Do not call `escalate_to_admin` again. Send one concise Vietnamese customer response containing exactly this ticket ID."
	}
	if fact, ok := adminHandoffVerifiedStatus(state, mentionedTicket); ok {
		switch fact.Status {
		case "pending":
			return "[ADMIN HANDOFF TRUTH CHECK] Ticket " + fact.TicketID + " was verified as pending. Tell the customer it is still waiting for Admin; do not create a duplicate or claim completion."
		case "completed":
			return "[ADMIN HANDOFF TRUTH CHECK] Ticket " + fact.TicketID + " was verified as completed. Tell the customer it is completed, using only confirmed conversation history for the outcome."
		case "dismissed":
			return "[ADMIN HANDOFF TRUTH CHECK] Ticket " + fact.TicketID + " was verified as dismissed/closed and is no longer pending. If the issue remains, re-check current Cloudmini service facts before deciding whether a new handoff is required."
		case "delivery_failed":
			return "[ADMIN HANDOFF TRUTH CHECK] Ticket " + fact.TicketID + " was verified as delivery_failed. Do not say it is pending or delivered. Re-check current Cloudmini service facts before deciding whether to create a new handoff."
		case "unavailable":
			return "[ADMIN HANDOFF TRUTH CHECK] Ticket " + fact.TicketID + " could not be verified for this customer conversation. Say only that its status cannot be verified; do not guess whether it exists elsewhere."
		}
	}
	if strings.TrimSpace(mentionedTicket) != "" {
		return "[ADMIN HANDOFF TRUTH CHECK] Historical ticket " + mentionedTicket + " has not been verified in this run. Call `admin_handoff_status` before describing its status or deciding whether to replace it."
	}
	return "[ADMIN HANDOFF TRUTH CHECK] No Admin handoff ticket has been created in this run. " +
		"Do not say that a request was sent/transferred and do not invent CMH/Ticket codes. " +
		"Review the current AGENTS.md operational notice and the Cloudmini tool result. " +
		"If Admin action is genuinely required, call `escalate_to_admin`; otherwise answer the customer directly without claiming a handoff."
}

func adminHandoffMentionedTicket(content string) string {
	return adminHandoffTicketPattern.FindString(content)
}

func adminHandoffTicketNeedingCheck(state *RunState, content string) string {
	for _, ticket := range adminHandoffTicketPattern.FindAllString(content, -1) {
		if state != nil && strings.EqualFold(ticket, state.Tool.AdminHandoffTicket) {
			continue
		}
		if _, ok := adminHandoffVerifiedStatus(state, ticket); !ok {
			return ticket
		}
	}
	return ""
}

func adminHandoffVerifiedStatus(state *RunState, ticket string) (AdminHandoffStatusFact, bool) {
	if state == nil || strings.TrimSpace(ticket) == "" || state.Tool.AdminHandoffStatuses == nil {
		return AdminHandoffStatusFact{}, false
	}
	fact, ok := state.Tool.AdminHandoffStatuses[strings.ToLower(strings.TrimSpace(ticket))]
	return fact, ok
}

func adminHandoffStatusNeedsCheck(state *RunState, content string) bool {
	return adminHandoffTicketNeedingCheck(state, content) != ""
}

func unsupportedAdminHandoffClaim(content string, state *RunState) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if inventedLegacyHandoffPattern.MatchString(content) {
		return true
	}
	for _, ticket := range adminHandoffTicketPattern.FindAllString(content, -1) {
		if state != nil && strings.EqualFold(ticket, state.Tool.AdminHandoffTicket) {
			continue
		}
		fact, ok := adminHandoffVerifiedStatus(state, ticket)
		if !ok || adminHandoffStatusResponseViolates(content, fact.Status) {
			return true
		}
	}
	if adminHandoffTicketPattern.MatchString(content) {
		return false
	}
	if state != nil && state.Tool.AdminHandoffTicket != "" {
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

func adminHandoffStatusResponseViolates(content, status string) bool {
	lower := strings.ToLower(content)
	containsAny := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(lower, value) {
				return true
			}
		}
		return false
	}
	switch status {
	case "pending":
		return !containsAny("đang chờ", "chờ xử lý", "pending")
	case "completed":
		return !containsAny("hoàn tất", "hoàn thành", "đã xử lý", "completed")
	case "dismissed":
		return !containsAny("đã đóng", "không còn chờ", "dismissed")
	case "delivery_failed":
		return !containsAny("không gửi được", "gửi thất bại", "delivery_failed", "delivery failed")
	case "unavailable":
		return !containsAny("chưa thể xác minh", "không thể xác minh", "không khả dụng", "unavailable")
	default:
		return true
	}
}

func adminHandoffStatusFallback(state *RunState, ticket string) string {
	fact, ok := adminHandoffVerifiedStatus(state, ticket)
	if !ok {
		if ticket != "" {
			return "Em chưa thể xác minh trạng thái mã " + ticket + " trong cuộc trò chuyện này, nên em chưa thể xác nhận ticket còn chờ hay đã đóng ạ."
		}
		return "Em chưa tạo yêu cầu Admin mới trong lượt này, nên em chưa thể xác nhận yêu cầu đã được chuyển xử lý ạ."
	}
	switch fact.Status {
	case "pending":
		return "Mã " + fact.TicketID + " hiện vẫn đang chờ Admin xử lý. Em chưa tạo thêm ticket trùng ạ."
	case "completed":
		return "Mã " + fact.TicketID + " đã hoàn tất. Em sẽ chỉ đối chiếu kết quả đã được xác nhận trong lịch sử trao đổi ạ."
	case "dismissed":
		return "Mã " + fact.TicketID + " đã đóng và không còn chờ xử lý. Nếu vấn đề vẫn còn, em cần kiểm tra lại trạng thái dịch vụ trước khi tạo yêu cầu mới ạ."
	case "delivery_failed":
		return "Mã " + fact.TicketID + " trước đó không gửi được đến Admin. Em cần kiểm tra lại trạng thái dịch vụ trước khi tạo yêu cầu mới ạ."
	case "unavailable":
		return "Em chưa thể xác minh trạng thái mã " + fact.TicketID + " trong cuộc trò chuyện này, nên em chưa thể xác nhận ticket còn chờ hay đã đóng ạ."
	default:
		return "Em chưa thể xác minh trạng thái ticket lúc này ạ."
	}
}
