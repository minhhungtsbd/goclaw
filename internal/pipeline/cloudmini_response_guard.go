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
	return false
}

func adminHandoffResponseViolatesGuard(state *RunState, content string) bool {
	if state == nil || !state.Tool.AdminHandoffCustomerReplyRequired || strings.TrimSpace(content) == "" {
		return false
	}
	ticket := strings.TrimSpace(state.Tool.AdminHandoffTicket)
	return ticket != "" && !strings.Contains(strings.ToLower(content), strings.ToLower(ticket))
}

func cloudminiSafeGuardResponse(state *RunState) string {
	if state != nil && state.Tool.AdminHandoffCustomerReplyRequired && state.Tool.AdminHandoffTicket != "" {
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
		return "Chỉ gửi một response cuối cho khách, gộp xác nhận đã chuyển yêu cầu và mã Ticket " + state.Tool.AdminHandoffTicket + ". Không gửi thêm tin riêng và không gọi escalate_to_admin lần nữa."
	}
	if state != nil && state.Cloudmini.EmailRequired {
		return "Khách chưa có email tài khoản. Chỉ xin email; không nêu thông tin dịch vụ, plan, hạn, trạng thái, phí hoặc khả năng khôi phục/gia hạn."
	}
	if state != nil && state.Cloudmini.EmailMismatch {
		return "Không nêu tài khoản khác, chủ sở hữu, email không khớp, LIVE, chuyển nhượng hoặc mua IP mới; với yêu cầu khôi phục/gia hạn chỉ trả lời ngắn gọn là chưa thể hỗ trợ."
	}
	return "Không suy đoán dữ liệu dịch vụ hoặc quyền sở hữu."
}
