package pipeline

import "strings"

func cloudminiResponseViolatesGuard(state *RunState, content string) bool {
	if state == nil || strings.TrimSpace(content) == "" {
		return false
	}
	lower := strings.ToLower(content)
	if state.Cloudmini.EmailRequired {
		if !containsAny(lower, "email", "e-mail", "mail") {
			return true
		}
		if containsAny(lower,
			"đang active", "dang active", "đang hoạt động", "dang hoat dong", "còn hạn", "con han",
			"residential", "privatev4", "budgetv4", "singapore", "không thể", "khong the",
			"khả dụng", "kha dung", "phí", "phi", "tài khoản khác", "tai khoan khac") {
			return true
		}
		intent := strings.ToLower(cloudminiSupportIntentText(state))
		return containsAny(intent, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han") &&
			containsAny(lower, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han")
	}
	if state.Cloudmini.EmailMismatch && containsAny(strings.ToLower(cloudminiSupportIntentText(state)),
		"khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han") {
		return containsAny(lower, "tài khoản khác", "tai khoan khac", "chủ sở hữu", "chu so huu",
			"không khớp", "khong khop", "sở hữu", "so huu", "live", "chuyển nhượng",
			"chuyen nhuong", "mua ip", "mua proxy", "email khác", "email khac")
	}
	// A structured incident is context only. It must never allow the model to
	// contradict a successful service_info result by claiming a permanent outage.
	if hasCloudminiUnsupportedOutageClaim(state, lower) {
		return true
	}
	for _, incident := range state.Cloudmini.IncidentsByIP {
		for _, claim := range incident.ForbiddenClaims {
			if claim = strings.TrimSpace(strings.ToLower(claim)); claim != "" && strings.Contains(lower, claim) {
				return true
			}
		}
	}
	return false
}

func hasCloudminiUnsupportedOutageClaim(state *RunState, lower string) bool {
	if state == nil {
		return false
	}
	permanentTerms := []string{
		"ngưng hoạt động hoàn toàn", "ngung hoat dong hoan toan",
		"ngưng hoạt động hạ tầng", "ngung hoat dong ha tang",
		"đã ngừng hoạt động", "da ngung hoat dong", "offline", "proxy die",
	}
	if !containsAny(lower, permanentTerms...) {
		return false
	}
	for _, fact := range state.Cloudmini.ServiceFacts {
		if fact.Status == "active" || fact.Status == "running" {
			return true
		}
	}
	for _, incident := range state.Cloudmini.IncidentsByIP {
		if incident.Severity != "permanent_outage" {
			return true
		}
	}
	return false
}

func adminHandoffResponseViolatesGuard(state *RunState, content string) bool {
	if state == nil || !state.Tool.AdminHandoffCustomerReplyRequired || strings.TrimSpace(content) == "" {
		return false
	}
	ticket := strings.TrimSpace(state.Tool.AdminHandoffTicket)
	if ticket == "" {
		return false
	}
	lower := strings.ToLower(content)
	if !strings.Contains(lower, strings.ToLower(ticket)) {
		return true
	}
	return cloudminiHandoffNeedsServiceExplanation(state) && !handoffReplyHasServiceExplanation(state, lower)
}

func cloudminiHandoffNeedsServiceExplanation(state *RunState) bool {
	return state != nil && len(state.Cloudmini.ServiceFacts) > 0
}

func handoffReplyHasServiceExplanation(state *RunState, lower string) bool {
	if state == nil {
		return false
	}
	// Require an observable status explanation for every checked IP, not merely
	// “đã chuyển Admin” or one status from a multi-IP request.
	for _, fact := range state.Cloudmini.ServiceFacts {
		scope := lower
		if len(state.Cloudmini.ServiceFacts) > 1 && fact.IP != "" {
			var ok bool
			scope, ok = cloudminiReplyClauseForIP(lower, strings.ToLower(fact.IP))
			if !ok {
				return false
			}
		}
		if !cloudminiFactStatusExplained(scope, fact.Status) {
			return false
		}
		if live, checked := state.Cloudmini.LiveChecks[fact.IP]; checked {
			if live && !containsAny(scope, "live", "kết nối bình thường", "ket noi binh thuong") {
				return false
			}
			if !live && !containsAny(scope, "die", "gián đoạn", "gian doan", "không kết nối", "khong ket noi") {
				return false
			}
		} else if state.Cloudmini.LiveAttempts[fact.IP] &&
			!containsAny(scope, "chưa trả", "chua tra", "không có kết quả", "khong co ket qua", "không thể kiểm tra", "khong the kiem tra", "tool lỗi", "tool loi") {
			return false
		}
	}
	return len(state.Cloudmini.ServiceFacts) > 0
}

func cloudminiReplyClauseForIP(content, ip string) (string, bool) {
	index := strings.Index(content, ip)
	if index < 0 {
		return "", false
	}
	start := index
	for start > 0 && !strings.ContainsRune(";.\n", rune(content[start-1])) {
		start--
	}
	end := index + len(ip)
	for end < len(content) && !strings.ContainsRune(";.\n", rune(content[end])) {
		end++
	}
	return content[start:end], true
}

func cloudminiFactStatusExplained(content, status string) bool {
	switch status {
	case "active", "running", "linked":
		return containsAny(content, "đang hoạt động", "dang hoat dong", "active", "đang chạy", "dang chay")
	case "not_verified", "unavailable":
		return containsAny(content, "chưa thể xác minh", "chua the xac minh", "chưa xác minh", "chua xac minh")
	case "email_required":
		return containsAny(content, "cần email", "xin email", "cho em email")
	case "expired":
		return containsAny(content, "hết hạn", "het han", "expired")
	case "deleted":
		return containsAny(content, "đã xoá", "đã xóa", "da xoa", "deleted")
	default:
		return containsAny(content, "chưa xác định", "chua xac dinh", "chưa thể xác định", "chua the xac dinh")
	}
}

func cloudminiSafeGuardResponse(state *RunState) string {
	if state != nil && state.Tool.AdminHandoffCustomerReplyRequired && state.Tool.AdminHandoffTicket != "" {
		if cloudminiHandoffNeedsServiceExplanation(state) {
			return adminHandoffCustomerConfirmationWithFacts(state, state.Tool.AdminHandoffTicket)
		}
		return adminHandoffCustomerConfirmation(state.Tool.AdminHandoffTicket)
	}
	if state != nil && state.Cloudmini.EmailRequired {
		return "Dạ anh cho em xin email đăng nhập Cloudmini để em kiểm tra và hỗ trợ chính xác ạ."
	}
	if state != nil && state.Cloudmini.EmailMismatch {
		return "Dạ, hiện tại em chưa thể hỗ trợ khôi phục hoặc gia hạn IP này ạ."
	}
	return "Dạ, em chưa thể xác minh thông tin IP này ngay lúc này ạ."
}

func cloudminiResponseGuardInstruction(state *RunState) string {
	if state != nil && state.Tool.AdminHandoffCustomerReplyRequired && state.Tool.AdminHandoffTicket != "" {
		instruction := "Chỉ gửi một response cuối cho khách, gộp xác nhận đã chuyển yêu cầu và mã Ticket " + state.Tool.AdminHandoffTicket + ". Không gửi thêm tin riêng và không gọi escalate_to_admin lần nữa."
		if cloudminiHandoffNeedsServiceExplanation(state) {
			instruction += " Bắt buộc giải thích trạng thái proxy theo kết quả service_info hiện tại (ví dụ đang hoạt động hoặc chưa thể xác minh); không được chỉ gửi mã ticket."
		}
		return instruction
	}
	if state != nil && state.Cloudmini.EmailRequired {
		return "Khách chưa có email tài khoản. Chỉ xin email; không nêu thông tin dịch vụ, plan, hạn, trạng thái, phí hoặc khả năng khôi phục/gia hạn."
	}
	if state != nil && state.Cloudmini.EmailMismatch {
		return "Không nêu tài khoản khác, chủ sở hữu, email không khớp, LIVE, chuyển nhượng hoặc mua IP mới; với yêu cầu khôi phục/gia hạn chỉ trả lời ngắn gọn là chưa thể hỗ trợ."
	}
	return "Không suy đoán dữ liệu dịch vụ hoặc quyền sở hữu."
}
