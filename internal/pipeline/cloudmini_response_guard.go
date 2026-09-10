package pipeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	if cloudminiNeedsConfiguredEmailAdminReview(state) && strings.TrimSpace(state.Tool.AdminHandoffTicket) == "" {
		// A response cannot replace the mandatory tool call. The customer may only
		// be told that the request was transferred after a real ticket exists.
		return true
	}
	if cloudminiNeedsIncidentAdminReview(state) && strings.TrimSpace(state.Tool.AdminHandoffTicket) == "" {
		return true
	}
	if cloudminiNeedsEmailMismatchAdminReview(state) {
		// A text-only answer is never sufficient here. The customer may only be
		// told about an Admin review after escalate_to_admin produced a real ticket.
		if strings.TrimSpace(state.Tool.AdminHandoffTicket) == "" {
			return true
		}
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
		message := incident.Guidance()
		if message == "" {
			continue
		}
		// A current successful LIVE result supersedes the incident notice for
		// this IP. A failed/unusable live attempt does not.
		if live, checked := state.Cloudmini.LiveChecks[ip]; checked && live && cloudminiLiveSupersedesIncident(incident) {
			continue
		}
		// Allow natural wording, while retaining conservative checks for known
		// remedy concepts. This is not a general semantic equivalence checker.
		if cloudminiIncidentRemedyMissing(incident, lower) {
			return true
		}
		// A service can remain active in billing/account data while its network
		// range is affected by an incident. Require both facts so the incident
		// notice cannot silently replace a successful service_info result.
		for _, fact := range state.Cloudmini.ServiceFacts {
			if fact.IP != ip {
				continue
			}
			scope := lower
			if len(state.Cloudmini.ServiceFacts) > 1 {
				var ok bool
				scope, ok = cloudminiReplyClauseForIP(lower, strings.ToLower(ip))
				if !ok {
					return true
				}
			}
			if !cloudminiFactStatusExplained(scope, fact.Status) {
				return true
			}
		}
		switch incident.Severity {
		case "scheduled_outage":
			if cloudminiScheduledNoticeMissingAt(incident, lower, time.Now()) {
				return true
			}
		case "maintenance":
			if !strings.Contains(lower, "bảo trì") {
				return true
			}
		case "resolved":
			if !containsAny(lower, "khôi phục", "khắc phục", "ổn định trở lại", "hoạt động trở lại") {
				return true
			}
		case "notice", "custom":
			if !containsAny(lower, "thông báo", "theo chính sách", "theo cập nhật") {
				return true
			}
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
			if !matched || (incident.Severity != "permanent_outage" && incident.Severity != "scheduled_outage") {
				return true
			}
			if live, checked := state.Cloudmini.LiveChecks[fact.IP]; checked && live && cloudminiLiveSupersedesIncident(incident) {
				return true
			}
		}
	}
	if activeServiceSeen {
		return false
	}
	for _, incident := range state.Cloudmini.IncidentsByIP {
		if incident.Severity != "permanent_outage" && incident.Severity != "scheduled_outage" {
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
			!containsAny(scope, "die", "gián đoạn", "gian doan", "không kết nối", "khong ket noi") {
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
		return containsAny(content, "còn hiệu lực", "con hieu luc", "đang hoạt động", "dang hoat dong", "active", "đang chạy", "dang chay")
	case "not_verified", "unavailable":
		return containsAny(content, "chưa thể xác minh", "chua the xac minh", "chưa xác minh", "chua xac minh")
	case "email_required":
		return containsAny(content, "cần email", "xin email", "cho em email")
	case "expired":
		return containsAny(content, "hết hạn", "het han", "expired")
	case "deleted":
		return containsAny(content, "không còn gắn với dịch vụ", "khong con gan voi dich vu") &&
			!containsAny(content, "đã xoá", "đã xóa", "bị xoá", "bị xóa", "da xoa", "deleted")
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
	if cloudminiNeedsEmailMismatchAdminReview(state) {
		return cloudminiEmailMismatchReply(state, "")
	}
	if cloudminiNeedsConfiguredEmailAdminReview(state) {
		return "Dạ, yêu cầu này cần được Admin kiểm tra trực tiếp nhưng hiện em chưa tạo được mã Ticket nên chưa thể xác nhận đã chuyển ạ."
	}
	if cloudminiNeedsIncidentAdminReview(state) {
		return "Dạ, " + strings.Join(cloudminiGroupedFactClauses(state), "; ") + ". Yêu cầu theo thông báo vận hành cần Admin kiểm tra, nhưng hiện em chưa tạo được Ticket nên chưa thể xác nhận đã chuyển ạ."
	}
	if state != nil && state.Cloudmini.EmailMismatch {
		return "Dạ em chưa thể xác minh thông tin IP này theo dữ liệu hiện tại ạ."
	}
	if response, ok := cloudminiOperationalIncidentResponse(state); ok {
		return response
	}
	if state != nil && len(state.Cloudmini.RequestHosts) > 0 {
		return "Dạ, gói Residential VN này dùng hostname " + strings.Join(state.Cloudmini.RequestHosts, ", ") + " thay cho IP dạng số nên anh không cần tìm thêm IPv4. Anh dùng hostname ở trường Host/IP và port đúng trong cột Proxy Port; không gửi lại user/pass. Nếu kết nối vẫn chậm hoặc lỗi, bên em sẽ tiếp nhận xử lý theo hostname này ạ."
	}
	return "Dạ, em chưa thể xác minh thông tin IP này ngay lúc này ạ."
}

// cloudminiOperationalIncidentResponse preserves verified service facts if the
// model cannot phrase the notice safely. Raw operator notes are never delivered.
func cloudminiOperationalIncidentResponse(state *RunState) (string, bool) {
	if state == nil || state.Cloudmini.EmailRequired || state.Cloudmini.EmailMismatch ||
		len(state.Cloudmini.ServiceFacts) == 0 {
		return "", false
	}
	messages := cloudminiRequiredIncidentMessages(state)
	if len(messages) == 0 {
		return "", false
	}

	facts := make([]string, 0, len(state.Cloudmini.ServiceFacts))
	for _, fact := range state.Cloudmini.ServiceFacts {
		label := "Dịch vụ"
		if ip := strings.TrimSpace(fact.IP); ip != "" {
			label = "IP " + ip
		}
		if plan := strings.TrimSpace(fact.Plan); plan != "" {
			label += " thuộc gói " + plan
		}

		status := "hiện chưa thể xác định trạng thái dịch vụ"
		switch fact.Status {
		case "active", "running", "linked":
			status = "có dịch vụ còn hiệu lực trên hệ thống"
			if fact.AccountEmailMatches {
				status += " và đã xác minh đúng tài khoản"
			}
		case "not_verified":
			status = "hiện chưa thể xác minh theo thông tin tài khoản"
		case "unavailable":
			status = "hiện chưa thể xác minh do công cụ kiểm tra chưa trả dữ liệu"
		case "expired":
			status = "đã hết hạn theo kết quả kiểm tra hiện tại"
		case "deleted":
			status = cloudminiFactStatusText(fact.Status)
		}
		if live, checked := state.Cloudmini.LiveChecks[fact.IP]; checked {
			if live {
				status += ", kiểm tra kết nối hiện là LIVE"
			} else {
				status += ", kiểm tra kết nối hiện là DIE"
			}
		} else if state.Cloudmini.LiveAttempts[fact.IP] {
			status += ", kiểm tra kết nối hiện là DIE"
		}
		facts = append(facts, label+" "+status)
	}
	if len(facts) == 0 {
		return "", false
	}

	return "Dạ em đã kiểm tra: " + strings.Join(facts, "; ") + " ạ.\n\n" +
		"IP thuộc phạm vi một thông báo vận hành. Em chưa thể xác nhận đầy đủ phương án hỗ trợ lúc này, nên cần Admin kiểm tra thêm trước khi hướng dẫn anh/chị thực hiện ạ.", true
}

func cloudminiResponseGuardInstruction(state *RunState) string {
	if cloudminiNeedsIncidentAdminReview(state) && state.Tool.AdminHandoffTicket == "" {
		return "Khách yêu cầu xử lý IP đã khớp thông báo vận hành và đã xác minh email. Bắt buộc gọi escalate_to_admin bằng đúng IP và email trước khi xác nhận chuyển. Không áp phí đổi/hủy thông thường thay cho phương án đã duyệt."
	}
	if state != nil && state.Tool.AdminHandoffCustomerReplyRequired && state.Tool.AdminHandoffTicket != "" {
		instruction := "Chỉ gửi một response cuối cho khách, gộp xác nhận đã chuyển yêu cầu và mã Ticket " + state.Tool.AdminHandoffTicket + ". Không gửi thêm tin riêng và không gọi escalate_to_admin lần nữa."
		if cloudminiHandoffNeedsServiceExplanation(state) {
			instruction += " Bắt buộc tách rõ trạng thái dịch vụ và kết quả kết nối: service_info active chỉ có nghĩa dịch vụ còn hiệu lực; live_check chỉ có LIVE hoặc DIE. Không được chỉ gửi mã ticket."
		}
		if messages := cloudminiRequiredIncidentMessages(state); len(messages) > 0 {
			instruction += " Bắt buộc truyền đạt đúng thông báo vận hành đã match: " + strings.Join(messages, " | ")
		}
		return instruction
	}
	if state != nil && state.Cloudmini.EmailRequired {
		return "Khách chưa có email tài khoản. Chỉ xin email; không nêu thông tin dịch vụ, plan, hạn, trạng thái, phí hoặc khả năng khôi phục/gia hạn."
	}
	if cloudminiNeedsEmailMismatchAdminReview(state) {
		return "Không nêu tài khoản khác, chủ sở hữu, email không khớp, LIVE, chuyển nhượng hoặc mua IP mới. Bắt buộc gọi escalate_to_admin với đúng IP và email Cloudmini khách đã cung cấp; không được nói đã/sẽ chuyển nếu tool chưa trả ticket."
	}
	if cloudminiNeedsConfiguredEmailAdminReview(state) {
		return "Email khách đã cung cấp thuộc danh sách chuyển Admin trực tiếp. Bắt buộc gọi escalate_to_admin với đúng IP/hostname và email Cloudmini khách đã cung cấp; không gọi live_check, không tự xử lý và không nói đã chuyển nếu chưa có Ticket thật."
	}
	if state != nil && len(state.Cloudmini.RequestHosts) > 0 {
		return "Residential VN dùng hostname " + strings.Join(state.Cloudmini.RequestHosts, ", ") + "; không yêu cầu IP dạng số và không gọi cloudmini_proxy_check. Hỗ trợ cấu hình bằng hostname/Proxy Port. Nếu khách đang báo lỗi thực tế, chậm kéo dài hoặc yêu cầu thay proxy và email đã có, gọi escalate_to_admin ngay với đúng hostname và email, không kèm port:user:pass."
	}
	if messages := cloudminiRequiredIncidentMessages(state); len(messages) > 0 {
		return "Diễn đạt tự nhiên, không sao chép nguyên văn. Truyền đạt đầy đủ nội dung đã duyệt sau, giữ đúng thời điểm, điều kiện, phương án hỗ trợ và trạng thái từng IP. Không thêm phí hoặc cam kết hoàn tất. Nội dung: " + strings.Join(messages, " | ")
	}
	return "Không suy đoán dữ liệu dịch vụ hoặc quyền sở hữu."
}

func cloudminiNeedsEmailMismatchAdminReview(state *RunState) bool {
	if state == nil || !state.Cloudmini.EmailMismatch {
		return false
	}
	intent := strings.ToLower(cloudminiSupportIntentText(state))
	return containsAny(intent, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han")
}

func cloudminiNeedsConfiguredEmailAdminReview(state *RunState) bool {
	return state != nil && state.Cloudmini.AdminHandoffRequired
}

func cloudminiEmailMismatchReply(state *RunState, ticket string) string {
	ips := cloudminiEmailMismatchIPs(state)
	if state != nil && len(state.Cloudmini.ServiceFacts) > 1 {
		// Group facts by their mismatch-aware status so a many-IP request does
		// not repeat the same explanation once per IP. Each IP remains inside
		// its own clause so the per-IP guards can still attribute statuses.
		type mismatchGroup struct {
			statusText string
			ips        []string
			hasUnknown bool
		}
		order := make([]string, 0, len(state.Cloudmini.ServiceFacts))
		groups := make(map[string]*mismatchGroup, len(state.Cloudmini.ServiceFacts))
		for _, fact := range state.Cloudmini.ServiceFacts {
			ip := strings.TrimSpace(fact.IP)
			status := "chưa thể xác định trạng thái dịch vụ"
			switch fact.Status {
			case "active", "running", "linked":
				if fact.AccountEmailMatches {
					status = "có dịch vụ còn hiệu lực trên hệ thống"
				} else {
					status = "hiện tại chưa thể xác minh được thông tin"
				}
			case "not_verified":
				status = "hiện tại chưa thể xác minh được thông tin"
			case "unavailable":
				status = "hiện chưa thể xác minh do công cụ kiểm tra chưa trả dữ liệu"
			case "email_required":
				status = "cần email tài khoản Cloudmini để xác minh"
			case "expired":
				status = "đã hết hạn theo kết quả kiểm tra hiện tại"
			case "deleted":
				status = cloudminiFactStatusText(fact.Status)
			}
			group, exists := groups[status]
			if !exists {
				group = &mismatchGroup{statusText: status}
				groups[status] = group
				order = append(order, status)
			}
			if ip == "" {
				group.hasUnknown = true
				continue
			}
			group.ips = append(group.ips, ip)
		}
		facts := make([]string, 0, len(order))
		for _, status := range order {
			group := groups[status]
			labels := make([]string, 0, len(group.ips)+1)
			for _, ip := range group.ips {
				labels = append(labels, "IP "+ip)
			}
			if group.hasUnknown {
				labels = append(labels, "Dịch vụ")
			}
			if len(labels) == 0 {
				continue
			}
			facts = append(facts, strings.Join(labels, ", ")+" "+group.statusText)
		}
		reply := "Dạ em kiểm tra: " + strings.Join(facts, "; ") + ". Vì vậy em chưa thể hỗ trợ khôi phục hoặc gia hạn các IP chưa xác minh ạ."
		if strings.TrimSpace(ticket) != "" {
			reply += " Em đã chuyển case này cho Admin kiểm tra trực tiếp. Mã theo dõi của anh/chị là " + strings.TrimSpace(ticket) + " ạ."
		}
		return reply
	}
	target := "IP này"
	object := "IP này"
	if len(ips) == 1 {
		target = "IP " + ips[0]
	} else if len(ips) > 1 {
		target = "các IP " + strings.Join(ips, ", ")
		object = "các IP này"
	}
	reply := "Dạ em kiểm tra " + target + " hiện tại chưa thể xác minh được thông tin, nên em chưa thể hỗ trợ khôi phục hoặc gia hạn " + object + " ạ."
	if strings.TrimSpace(ticket) != "" {
		reply += " Em đã chuyển case này cho Admin kiểm tra trực tiếp. Mã theo dõi của anh/chị là " + strings.TrimSpace(ticket) + " ạ."
	}
	return reply
}

func cloudminiEmailMismatchIPs(state *RunState) []string {
	if state == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(state.Cloudmini.ServiceFacts))
	for _, fact := range state.Cloudmini.ServiceFacts {
		ip := strings.TrimSpace(fact.IP)
		if ip == "" || fact.Status == "deleted" ||
			(fact.Status != "not_verified" && fact.AccountEmailMatches) {
			continue
		}
		if _, exists := seen[ip]; exists {
			continue
		}
		seen[ip] = struct{}{}
		result = append(result, ip)
	}
	if len(result) == 0 {
		return append([]string(nil), state.Cloudmini.RequestIPs...)
	}
	return result
}

func cloudminiRequiredIncidentMessages(state *RunState) []string {
	if state == nil {
		return nil
	}
	messages := make([]string, 0, len(state.Cloudmini.IncidentsByIP))
	seen := make(map[string]struct{})
	for ip, incident := range state.Cloudmini.IncidentsByIP {
		message := incident.Guidance()
		if message == "" {
			continue
		}
		if live, checked := state.Cloudmini.LiveChecks[ip]; checked && live && cloudminiLiveSupersedesIncident(incident) {
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

func cloudminiLiveSupersedesIncident(incident store.OperationalIncident) bool {
	return incident.Severity == "temporary_issue" || incident.Severity == "degraded" || incident.Severity == "permanent_outage"
}

func cloudminiIncidentRemedyMissing(incident store.OperationalIncident, reply string) bool {
	guidance := strings.ToLower(incident.Guidance())
	if containsAny(guidance, "miễn phí", "không tính phí") &&
		(!containsAny(reply, "miễn phí", "không tính phí", "không mất phí") || cloudminiIncidentPrice.MatchString(reply)) {
		return true
	}
	refund := containsAny(guidance, "hoàn tiền", "hoàn phần", "hoàn lại", "hoàn số", "hoàn phí")
	if refund && !containsAny(reply, "hoàn tiền", "hoàn phần", "hoàn lại", "hoàn số", "hoàn phí") {
		return true
	}
	if strings.Contains(guidance, "xem xét") && refund &&
		!containsAny(reply, "xem xét", "kiểm tra", "cân nhắc", "xác nhận") {
		return true
	}
	return false
}

var cloudminiIncidentPrice = regexp.MustCompile(`\b[1-9][0-9.,]*\s*(?:(?:đồng|vnđ|vnd|đ|k)(?:\s|$|[.,;:/!?])|/ip)`)

func cloudminiScheduledNoticeMissingAt(incident store.OperationalIncident, reply string, now time.Time) bool {
	when, err := time.Parse(time.RFC3339, incident.EventAt)
	if err != nil {
		return true
	}
	if !containsAny(reply, "ngưng", "ngừng", "dừng") || containsAny(reply, "đã ngưng", "đã ngừng", "đã dừng") {
		return true
	}
	// Check the event date without demanding a verbatim sentence. Keep the
	// offset in event_at so a local midnight is not shifted to yesterday.
	day, month := when.Day(), int(when.Month())
	if !containsAny(reply, when.Format("2006-01-02"), when.Format("02/01"), fmt.Sprintf("%d/%d", day, month), fmt.Sprintf("%d tháng %d", day, month), when.Format("02-01")) {
		return true
	}
	if now.Before(when) {
		return !containsAny(reply, "sẽ", "sắp", "dự kiến", "theo lịch")
	}
	// Passing the scheduled date is not proof that the network was shut down.
	return !containsAny(reply, "dự kiến", "theo lịch") || !containsAny(reply, "xác nhận", "kiểm tra") || containsAny(reply, "sẽ ngưng", "sẽ ngừng", "sắp ngưng", "sắp ngừng")
}
