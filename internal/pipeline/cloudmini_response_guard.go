package pipeline

import (
	"sort"
	"strings"
)

func cloudminiResponseViolatesGuard(state *RunState, content string) bool {
	if state == nil || strings.TrimSpace(content) == "" {
		return false
	}
	lower := strings.ToLower(content)
	if len(state.Cloudmini.RequestHosts) > 0 && cloudminiResidentialVNAsksForNumericIP(lower) {
		return true
	}
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
	if cloudminiIncidentExplanationMissing(state, lower) {
		return true
	}
	return false
}

func cloudminiResidentialVNAsksForNumericIP(lower string) bool {
	if containsAny(lower, "không cần ip dạng số", "khong can ip dang so", "không yêu cầu ip dạng số", "khong yeu cau ip dang so") {
		return false
	}
	hasNumericIP := containsAny(lower, "ip dạng số", "ip dang so", "địa chỉ ip số", "dia chi ip so", "xxx.xxx")
	asksForIt := containsAny(lower, "gửi", "gui", "cung cấp", "cung cap", "bắt buộc", "bat buoc", "phải có", "phai co", "cần", "can ", "chưa đủ", "chua du")
	return hasNumericIP && asksForIt ||
		(strings.Contains(lower, "hostname") && containsAny(lower, "chưa đủ để", "chua du de"))
}

func cloudminiIncidentExplanationMissing(state *RunState, lower string) bool {
	if state == nil {
		return false
	}
	for ip, incident := range state.Cloudmini.IncidentsByIP {
		message := strings.TrimSpace(incident.CustomerMessage)
		if message == "" {
			continue
		}
		// A current successful LIVE result supersedes the incident notice for
		// this IP. A failed/unusable live attempt does not.
		if live, checked := state.Cloudmini.LiveChecks[ip]; checked && live {
			continue
		}
		// customer_message is operator-authored data. Requiring it verbatim
		// prevents the model from replacing a scoped notice with a vague or
		// stronger claim that happens to contain the same severity words.
		if !strings.Contains(lower, strings.ToLower(message)) {
			return true
		}
		switch incident.Severity {
		case "temporary_issue":
			if !containsAny(lower, "lỗi tạm thời", "loi tam thoi", "sự cố tạm thời", "su co tam thoi") {
				return true
			}
		case "degraded":
			if !containsAny(lower, "suy giảm", "suy giam", "không ổn định", "khong on dinh") {
				return true
			}
		case "permanent_outage":
			if !containsAny(lower, "ngưng hoạt động hoàn toàn", "ngung hoat dong hoan toan", "đã ngừng hoạt động", "da ngung hoat dong") {
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
	activeServiceSeen := false
	for _, fact := range state.Cloudmini.ServiceFacts {
		if fact.Status == "active" || fact.Status == "running" {
			activeServiceSeen = true
			incident, matched := state.Cloudmini.IncidentsByIP[fact.IP]
			if !matched || incident.Severity != "permanent_outage" {
				return true
			}
			if live, checked := state.Cloudmini.LiveChecks[fact.IP]; checked && live {
				return true
			}
		}
	}
	if activeServiceSeen {
		return false
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
	if state != nil && len(state.Cloudmini.RequestHosts) > 0 {
		return "Dạ, gói Residential VN này dùng hostname " + strings.Join(state.Cloudmini.RequestHosts, ", ") + " thay cho IP dạng số nên anh không cần tìm thêm IPv4. Anh dùng hostname ở trường Host/IP và port đúng trong cột Proxy Port; không gửi lại user/pass. Nếu kết nối vẫn chậm hoặc lỗi, bên em sẽ tiếp nhận xử lý theo hostname này ạ."
	}
	return "Dạ, em chưa thể xác minh thông tin IP này ngay lúc này ạ."
}

func cloudminiResponseGuardInstruction(state *RunState) string {
	if state != nil && state.Tool.AdminHandoffCustomerReplyRequired && state.Tool.AdminHandoffTicket != "" {
		instruction := "Chỉ gửi một response cuối cho khách, gộp xác nhận đã chuyển yêu cầu và mã Ticket " + state.Tool.AdminHandoffTicket + ". Không gửi thêm tin riêng và không gọi escalate_to_admin lần nữa."
		if cloudminiHandoffNeedsServiceExplanation(state) {
			instruction += " Bắt buộc giải thích trạng thái proxy theo kết quả service_info hiện tại (ví dụ đang hoạt động hoặc chưa thể xác minh); không được chỉ gửi mã ticket."
		}
		if messages := cloudminiRequiredIncidentMessages(state); len(messages) > 0 {
			instruction += " Bắt buộc truyền đạt đúng thông báo vận hành đã match: " + strings.Join(messages, " | ")
		}
		return instruction
	}
	if state != nil && state.Cloudmini.EmailRequired {
		return "Khách chưa có email tài khoản. Chỉ xin email; không nêu thông tin dịch vụ, plan, hạn, trạng thái, phí hoặc khả năng khôi phục/gia hạn."
	}
	if state != nil && state.Cloudmini.EmailMismatch {
		return "Không nêu tài khoản khác, chủ sở hữu, email không khớp, LIVE, chuyển nhượng hoặc mua IP mới; với yêu cầu khôi phục/gia hạn chỉ trả lời ngắn gọn là chưa thể hỗ trợ."
	}
	if state != nil && len(state.Cloudmini.RequestHosts) > 0 {
		return "Residential VN dùng hostname " + strings.Join(state.Cloudmini.RequestHosts, ", ") + "; không yêu cầu IP dạng số và không gọi cloudmini_proxy_check. Hỗ trợ cấu hình bằng hostname/Proxy Port. Nếu khách đang báo lỗi thực tế, chậm kéo dài hoặc yêu cầu thay proxy và email đã có, gọi escalate_to_admin ngay với đúng hostname và email, không kèm port:user:pass."
	}
	return "Không suy đoán dữ liệu dịch vụ hoặc quyền sở hữu."
}

func cloudminiRequiredIncidentMessages(state *RunState) []string {
	if state == nil {
		return nil
	}
	messages := make([]string, 0, len(state.Cloudmini.IncidentsByIP))
	seen := make(map[string]struct{})
	for ip, incident := range state.Cloudmini.IncidentsByIP {
		message := strings.TrimSpace(incident.CustomerMessage)
		if message == "" {
			continue
		}
		if live, checked := state.Cloudmini.LiveChecks[ip]; checked && live {
			continue
		}
		if _, exists := seen[message]; exists {
			continue
		}
		seen[message] = struct{}{}
		messages = append(messages, message)
	}
	sort.Strings(messages)
	return messages
}
