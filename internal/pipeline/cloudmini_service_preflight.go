package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const cloudminiProxyCheckToolName = "cloudmini_proxy_check"

var cloudminiIPCandidate = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var cloudminiEmailCandidate = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
var cloudminiDeclaredIPCount = regexp.MustCompile(`(?i)\b(\d{1,3})\s*(?:ip|proxy|vps)\b`)
var cloudminiOutageSectionStart = regexp.MustCompile(`(?mi)^#{1,6}\s*Subnet IPv4 ngưng hoạt động hoàn toàn\s*:?\s*$`)
var cloudminiNextHeading = regexp.MustCompile(`(?m)^#{1,6}\s+`)
var cloudminiCIDRCandidate = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)

// CloudminiServicePreflightStage loads service facts before the model answers a
// customer request about a Proxy IP. It applies only when the scoped agent has
// explicitly been granted cloudmini_proxy_check, so it cannot bypass tool policy.
type CloudminiServicePreflightStage struct {
	deps *PipelineDeps
}

func NewCloudminiServicePreflightStage(deps *PipelineDeps) *CloudminiServicePreflightStage {
	return &CloudminiServicePreflightStage{deps: deps}
}

func (s *CloudminiServicePreflightStage) Name() string { return "cloudmini_service_preflight" }

func (s *CloudminiServicePreflightStage) Execute(ctx context.Context, state *RunState) error {
	if !isCloudminiServiceRequest(state) || s.deps.ExecuteToolCall == nil {
		return nil
	}
	ips := resolveCloudminiRequestIPs(state)
	state.Cloudmini.RequestIPs = append([]string(nil), ips...)
	appendCloudminiOperationalSubnetNotice(state, ips)
	// ContextStage intentionally treats its initial tool snapshot as best effort
	// for token accounting. A transient filter error must not silently disable a
	// mandatory support check, so refresh the list before deciding to skip.
	if !hasCloudminiProxyCheckTool(state.Think.Tools) && s.deps.BuildFilteredTools != nil {
		toolDefs, err := s.deps.BuildFilteredTools(state)
		if err != nil {
			slog.Warn("cloudmini service preflight tool refresh failed", "error", err)
		} else {
			state.Think.Tools = toolDefs
		}
	}
	if !hasCloudminiProxyCheckTool(state.Think.Tools) {
		slog.Warn("cloudmini service preflight skipped: tool not available", "ip_count", len(cloudminiIPs(state.Input.Message)))
		return nil
	}

	state.Tool.AllowedTools = map[string]bool{cloudminiProxyCheckToolName: true}
	accountEmail := latestCloudminiCustomerEmailForState(state)
	for index, ip := range ips {
		arguments := map[string]any{
			"ip":        ip,
			"operation": "service_info",
		}
		if accountEmail != "" {
			arguments["account_email"] = accountEmail
		}
		tc := providers.ToolCall{
			ID:        cloudminiPreflightCallID(state.RunID, "service_info", ip, index),
			Name:      cloudminiProxyCheckToolName,
			Arguments: arguments,
		}
		if s.deps.AuthorizeToolCall != nil {
			if ok, reason := s.deps.AuthorizeToolCall(ctx, state, tc); !ok {
				return fmt.Errorf("authorize %s: %s", cloudminiProxyCheckToolName, reason)
			}
		}

		// Keep the OpenAI-compatible transcript valid: each tool result must
		// immediately follow an assistant message that contains the matching call.
		state.Messages.AppendPending(providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{tc}})
		messages, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute %s: %w", cloudminiProxyCheckToolName, err)
		}
		for _, message := range messages {
			state.Messages.AppendPending(message)
		}
		captureCloudminiServiceFacts(state, messages)
		state.Tool.TotalToolCalls++

		if requiresCloudminiProxyLiveCheck(state, ip) && !cloudminiServiceDeleted(messages) {
			liveCheck := providers.ToolCall{
				ID:   cloudminiPreflightCallID(state.RunID, "live_check", ip, index),
				Name: cloudminiProxyCheckToolName,
				Arguments: map[string]any{
					"ip":        ip,
					"operation": "live_check",
				},
			}
			state.Messages.AppendPending(providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{liveCheck}})
			liveMessages, err := s.deps.ExecuteToolCall(ctx, state, liveCheck)
			if err != nil {
				return fmt.Errorf("execute %s: %w", cloudminiProxyCheckToolName, err)
			}
			for _, message := range liveMessages {
				state.Messages.AppendPending(message)
			}
			state.Tool.TotalToolCalls++
		}
	}
	refreshCloudminiDeterministicState(state, accountEmail)
	appendCloudminiRequestScope(state, ips)
	appendCloudminiResponseGuard(state, accountEmail)
	return nil
}

// prepareCloudminiToolState initializes only deterministic request scope after
// the LLM has chosen a Cloudmini-related tool. It never calls a tool itself.
// This preserves LLM-led support while preventing stale IPs from long sessions
// from leaking into a check or Admin handoff.
func prepareCloudminiToolState(state *RunState, call providers.ToolCall) {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" ||
		(call.Name != cloudminiProxyCheckToolName && call.Name != "escalate_to_admin") ||
		len(state.Cloudmini.RequestIPs) > 0 {
		return
	}
	ips := resolveCloudminiRequestIPs(state)
	if len(ips) == 0 {
		return
	}
	state.Cloudmini.RequestIPs = append([]string(nil), ips...)
	appendCloudminiOperationalSubnetNotice(state, ips)
	appendCloudminiRequestScope(state, ips)
}

// recordCloudminiProxyCheckResult consumes facts from a service_info call that
// the LLM selected. ToolStage invokes it immediately after the tool result has
// been processed so the next LLM iteration sees deterministic email, ownership,
// live-check, scope, and handoff guards without an automatic backend API call.
func recordCloudminiProxyCheckResult(state *RunState, call providers.ToolCall, messages []providers.Message) {
	if state == nil || call.Name != cloudminiProxyCheckToolName ||
		strings.TrimSpace(cloudminiToolStringArg(call.Arguments, "operation")) != "service_info" {
		return
	}
	if captureCloudminiServiceFacts(state, messages) == 0 {
		return
	}
	accountEmail := latestCloudminiCustomerEmailForState(state)
	refreshCloudminiDeterministicState(state, accountEmail)
	appendCloudminiResponseGuard(state, accountEmail)
}

func refreshCloudminiDeterministicState(state *RunState, accountEmail string) {
	if state == nil {
		return
	}
	state.Cloudmini.EmailRequired = accountEmail == "" && len(state.Cloudmini.ServiceFacts) > 0
	state.Cloudmini.EmailMismatch = false
	for _, fact := range state.Cloudmini.ServiceFacts {
		// A deleted IP is intentionally exempt for recovery: its old owner is
		// irrelevant and the supplied email is the destination account. For an
		// existing service, a false match is never evidence of another owner.
		if fact.Status == "not_verified" || fact.Status == "unavailable" ||
			(accountEmail != "" && fact.Status != "deleted" && !fact.AccountEmailMatches) {
			state.Cloudmini.EmailMismatch = true
			break
		}
	}
	state.Cloudmini.HandoffRequired = requiresDeterministicCloudminiHandoff(state)
}

func cloudminiToolStringArg(arguments map[string]any, key string) string {
	value, _ := arguments[key].(string)
	return strings.TrimSpace(value)
}

func cloudminiPreflightCallID(runID, operation, ip string, index int) string {
	raw := fmt.Sprintf("%s:%s:%s:%d", runID, operation, ip, index)
	hash := sha256.Sum256([]byte(raw))
	return "call_" + hex.EncodeToString(hash[:])[:35]
}

// appendCloudminiRequestScope pins a multi-IP support run to the customer's
// current list. Long-running support sessions can otherwise make an LLM reuse
// an older proxy list after it has received the current service facts.
func appendCloudminiRequestScope(state *RunState, ips []string) {
	if len(ips) == 0 {
		return
	}
	system := state.Messages.System()
	scopeSource := "tin nhắn khách vừa gửi"
	if len(cloudminiIPs(state.Input.Message)) != len(ips) {
		scopeSource = "yêu cầu tiếp nối đã được đối chiếu đúng số lượng với lượt khách liền trước"
	}
	system.Content += "\n\n[PHẠM VI YÊU CẦU CLOUDMINI HIỆN TẠI]\n" +
		"Chỉ xử lý các IP thuộc " + scopeSource + ": " + strings.Join(ips, ", ") + ".\n" +
		"Không dùng, không kiểm tra lại và không đưa IP từ các danh sách cũ trong lịch sử vào phản hồi hoặc Admin handoff. " +
		"Nếu cần chuyển Admin cho yêu cầu nhiều IP này, ticket phải chứa đúng toàn bộ các IP trên."
	if state.Cloudmini.ScopeAmbiguous {
		system.Content += " Khách nêu số lượng IP không khớp dữ liệu hiện có; phải hỏi lại danh sách, không tự suy đoán IP cũ."
	}
	state.Messages.SetSystem(system)
}

func resolveCloudminiRequestIPs(state *RunState) []string {
	current := cloudminiIPs(state.Input.Message)
	if len(current) == 0 {
		if !isCloudminiEmailContinuation(state) {
			return nil
		}
		for i := len(state.Messages.History()) - 1; i >= 0; i-- {
			if state.Messages.History()[i].Role != "user" {
				continue
			}
			if previous := cloudminiIPs(state.Messages.History()[i].Content); len(previous) > 0 {
				return previous
			}
		}
		return nil
	}
	matches := cloudminiDeclaredIPCount.FindStringSubmatch(state.Input.Message)
	if len(matches) != 2 {
		return current
	}
	declared, err := strconv.Atoi(matches[1])
	if err != nil || declared <= len(current) {
		return current
	}
	lower := strings.ToLower(state.Input.Message)
	if !containsAny(lower, "này", "thêm", "nữa", "tiếp") {
		state.Cloudmini.ScopeAmbiguous = true
		return current
	}

	var previous []string
	for i := len(state.Messages.History()) - 1; i >= 0; i-- {
		if state.Messages.History()[i].Role != "user" {
			continue
		}
		previous = cloudminiIPs(state.Messages.History()[i].Content)
		break
	}
	combined := appendUniqueCloudminiIPs(previous, current...)
	if len(combined) != declared {
		state.Cloudmini.ScopeAmbiguous = true
		return current
	}
	return combined
}

func appendUniqueCloudminiIPs(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	result := make([]string, 0, len(base)+len(values))
	for _, value := range append(append([]string(nil), base...), values...) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendCloudminiOperationalSubnetNotice(state *RunState, ips []string) {
	system := state.Messages.System()
	start := cloudminiOutageSectionStart.FindStringIndex(system.Content)
	if start == nil {
		return
	}
	sectionEnd := len(system.Content)
	afterHeading := system.Content[start[1]:]
	if next := cloudminiNextHeading.FindStringIndex(afterHeading); next != nil {
		sectionEnd = start[1] + next[0]
	}
	section := strings.TrimSpace(system.Content[start[0]:sectionEnd])
	var matched []string
	for _, candidate := range cloudminiCIDRCandidate.FindAllString(section, -1) {
		prefix, err := netip.ParsePrefix(candidate)
		if err != nil {
			continue
		}
		for _, value := range ips {
			ip, err := netip.ParseAddr(value)
			if err == nil && prefix.Contains(ip) {
				matched = appendUniqueCloudminiIPs(matched, candidate)
				break
			}
		}
	}
	if len(matched) == 0 {
		return
	}
	state.Cloudmini.OutageCIDRs = matched
	system.Content += "\n\n[THÔNG BÁO VẬN HÀNH KHỚP IP HIỆN TẠI - BẮT BUỘC ÁP DỤNG]\n" +
		"IP khách gửi thuộc subnet: " + strings.Join(matched, ", ") + ".\n" + section + "\n" +
		"Phải trả lời theo thông báo này. Không thay bằng hướng dẫn Check Live chung, không nói đã chuyển Admin và không tạo mã ticket nếu chưa gọi thành công `escalate_to_admin`."
	state.Messages.SetSystem(system)
}

func requiresCloudminiProxyLiveCheck(state *RunState, ip string) bool {
	message := strings.ToLower(cloudminiSupportIntentText(state))
	if !containsAny(message, "lỗi", "loi", "không kết nối", "khong ket noi", "check live", "die") ||
		len(state.Cloudmini.OutageCIDRs) > 0 {
		return false
	}
	if strings.Contains(message, "proxy") {
		return true
	}
	for _, fact := range state.Cloudmini.ServiceFacts {
		if fact.IP == ip && fact.Plan != "" && !strings.Contains(strings.ToLower(fact.Plan), "vps") {
			return true
		}
	}
	return false
}

func cloudminiServiceDeleted(messages []providers.Message) bool {
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		var response struct {
			Services []struct {
				ServiceStatus string `json:"service_status"`
			} `json:"services"`
		}
		if json.Unmarshal([]byte(message.Content), &response) != nil {
			continue
		}
		for _, service := range response.Services {
			if service.ServiceStatus == "deleted" {
				return true
			}
		}
	}
	return false
}

func captureCloudminiServiceFacts(state *RunState, messages []providers.Message) int {
	captured := 0
	for _, message := range messages {
		if message.Role != "tool" || message.IsError {
			continue
		}
		var response struct {
			Services []struct {
				IP                  string          `json:"ip"`
				Plan                string          `json:"plan"`
				PlanFamily          string          `json:"plan_family"`
				ServiceStatus       string          `json:"service_status"`
				AccountEmailMatches bool            `json:"account_email_matches"`
				CancellationPolicy  string          `json:"cancellation_policy"`
				Expire              json.RawMessage `json:"expire"`
			} `json:"services"`
		}
		if json.Unmarshal([]byte(message.Content), &response) != nil {
			continue
		}
		for _, service := range response.Services {
			status := service.ServiceStatus
			if strings.TrimSpace(string(service.Expire)) == "null" {
				status = "deleted"
			}
			fact := CloudminiServiceFact{
				IP:                  service.IP,
				Plan:                service.Plan,
				PlanFamily:          service.PlanFamily,
				Status:              status,
				AccountEmailMatches: service.AccountEmailMatches,
				CancellationPolicy:  service.CancellationPolicy,
			}
			replaced := false
			if fact.IP != "" {
				for index := range state.Cloudmini.ServiceFacts {
					if state.Cloudmini.ServiceFacts[index].IP == fact.IP {
						state.Cloudmini.ServiceFacts[index] = fact
						replaced = true
						break
					}
				}
			}
			if !replaced {
				state.Cloudmini.ServiceFacts = append(state.Cloudmini.ServiceFacts, fact)
			}
			captured++
		}
	}
	return captured
}

func requiresDeterministicCloudminiHandoff(state *RunState) bool {
	if state.Cloudmini.ScopeAmbiguous || len(state.Cloudmini.ServiceFacts) == 0 {
		return false
	}
	lower := strings.ToLower(cloudminiSupportIntentText(state))
	restore := containsAny(lower, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi")
	customerEmailKnown := latestCloudminiCustomerEmailForState(state) != ""
	allVerified := true
	anyDeleted := false
	allCancellationEligible := true
	allVPS := true
	for _, fact := range state.Cloudmini.ServiceFacts {
		// A deleted IP is no longer attached to any active service. For a restore
		// request, the supplied customer email identifies the destination account;
		// it is not required to match the former service owner.
		identityEligible := fact.AccountEmailMatches ||
			(restore && fact.Status == "deleted" && customerEmailKnown)
		allVerified = allVerified && identityEligible
		anyDeleted = anyDeleted || fact.Status == "deleted"
		allCancellationEligible = allCancellationEligible &&
			fact.CancellationPolicy != "" && fact.CancellationPolicy != "not_supported"
		allVPS = allVPS && (fact.PlanFamily == "vps" || strings.Contains(strings.ToLower(fact.Plan), "vps"))
	}
	if !allVerified {
		return false
	}
	if restore && anyDeleted {
		return true
	}
	cancelFailed := containsAny(lower, "hủy", "huỷ", "huy") &&
		containsAny(lower, "lỗi", "loi", "không được", "khong duoc", "không thể", "khong the", "thất bại", "that bai")
	if cancelFailed && allCancellationEligible {
		return true
	}
	if containsAny(lower, "nâng cấp", "nang cap") && allVPS {
		return true
	}
	replaceOrRefund := containsAny(lower, "thay thế", "thay the", "đổi ip", "doi ip", "hoàn tiền", "hoan tien")
	return replaceOrRefund && len(state.Cloudmini.OutageCIDRs) > 0
}

func isCloudminiServiceRequest(state *RunState) bool {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" {
		return false
	}
	message := strings.ToLower(state.Input.Message)
	return len(cloudminiIPs(message)) > 0 || isCloudminiEmailContinuation(state)
}

func isCloudminiEmailContinuation(state *RunState) bool {
	if state == nil || state.Input == nil || len(cloudminiIPs(state.Input.Message)) > 0 ||
		len(cloudminiEmailCandidate.FindAllString(state.Input.Message, -1)) == 0 {
		return false
	}
	remainder := strings.TrimSpace(cloudminiEmailCandidate.ReplaceAllString(state.Input.Message, ""))
	remainder = strings.Trim(remainder, " .,:;!?-()[]{}")
	if len(strings.Fields(remainder)) > 3 {
		return false
	}
	history := state.Messages.History()
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "assistant" {
			continue
		}
		askedForEmail := containsAny(strings.ToLower(history[i].Content), "email", "e-mail", "địa chỉ mail", "địa chỉ email")
		if !askedForEmail {
			return false
		}
		for j := i - 1; j >= 0; j-- {
			if history[j].Role == "user" {
				return len(cloudminiIPs(history[j].Content)) > 0
			}
		}
		return false
	}
	return false
}

func cloudminiSupportIntentText(state *RunState) string {
	if state == nil || state.Input == nil || !isCloudminiEmailContinuation(state) {
		if state == nil || state.Input == nil {
			return ""
		}
		return state.Input.Message
	}
	for i := len(state.Messages.History()) - 1; i >= 0; i-- {
		if state.Messages.History()[i].Role == "user" && len(cloudminiIPs(state.Messages.History()[i].Content)) > 0 {
			return state.Messages.History()[i].Content + "\n" + state.Input.Message
		}
	}
	return state.Input.Message
}

func appendCloudminiResponseGuard(state *RunState, accountEmail string) {
	if state == nil || len(state.Cloudmini.ServiceFacts) == 0 {
		return
	}
	system := state.Messages.System()
	if (accountEmail == "" || state.Cloudmini.EmailRequired) && !strings.Contains(system.Content, "[CLOUDMINI EMAIL GATE - BẮT BUỘC]") {
		system.Content += "\n\n[CLOUDMINI EMAIL GATE - BẮT BUỘC]\n" +
			"Khách chưa cung cấp email tài khoản Cloudmini trong hội thoại. Chỉ yêu cầu email tài khoản để xác minh; không nêu plan, region, expire, trạng thái dịch vụ, phí, khả năng khôi phục/gia hạn, không hướng dẫn chuyên sâu và không tạo Admin handoff. Nếu khách vừa gửi email tiếp nối, dùng kết quả service_info hiện tại thay vì hỏi lại."
	}
	if state.Cloudmini.EmailMismatch && !strings.Contains(system.Content, "[CLOUDMINI KHÔI PHỤC KHÔNG XÁC MINH ĐƯỢC - BẮT BUỘC]") {
		lower := strings.ToLower(cloudminiSupportIntentText(state))
		if containsAny(lower, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han") {
			system.Content += "\n\n[CLOUDMINI KHÔI PHỤC KHÔNG XÁC MINH ĐƯỢC - BẮT BUỘC]\n" +
				"Không nói IP thuộc tài khoản khác, chủ sở hữu khác, email không khớp, IP đang LIVE, chuyển nhượng hoặc đề xuất mua IP mới. Không tiết lộ dữ liệu đối chiếu nội bộ. Chỉ trả lời ngắn gọn bằng tiếng Việt rằng hiện chưa thể hỗ trợ khôi phục hoặc gia hạn IP này. Không gọi live_check và không tạo Admin handoff."
		}
	}
	state.Messages.SetSystem(system)
}

func hasCloudminiProxyCheckTool(tools []providers.ToolDefinition) bool {
	for _, tool := range tools {
		if tool.Function != nil && tool.Function.Name == cloudminiProxyCheckToolName {
			return true
		}
	}
	return false
}

func cloudminiIPs(message string) []string {
	seen := make(map[string]struct{})
	ips := make([]string, 0, 1)
	for _, candidate := range cloudminiIPCandidate.FindAllString(message, -1) {
		ip, err := netip.ParseAddr(candidate)
		if err != nil || !ip.Is4() {
			continue
		}
		value := ip.String()
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		ips = append(ips, value)
	}
	return ips
}

// latestCloudminiCustomerEmail reuses an email the customer already supplied,
// without trusting assistant or Admin messages that may mention other accounts.
func latestCloudminiCustomerEmail(history []providers.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		matches := cloudminiEmailCandidate.FindAllString(history[i].Content, -1)
		if len(matches) > 0 {
			return strings.ToLower(matches[len(matches)-1])
		}
	}
	return ""
}

func latestCloudminiCustomerEmailForState(state *RunState) string {
	if state == nil || state.Input == nil {
		return ""
	}
	history := append(append([]providers.Message(nil), state.Messages.History()...), providers.Message{
		Role:    "user",
		Content: state.Input.Message,
	})
	return latestCloudminiCustomerEmail(history)
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

// validateCloudminiCurrentRequestToolCall prevents a later LLM iteration from
// acting on an older IP list that happens to remain in a long session history.
// It is deliberately limited to Cloudmini's service check and Admin handoff;
// unrelated tools continue to use the normal tool-policy gate.
func validateCloudminiCurrentRequestToolCall(state *RunState, tc providers.ToolCall) (bool, string) {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" {
		return true, ""
	}
	current := state.Cloudmini.RequestIPs
	if len(current) == 0 {
		current = cloudminiIPs(state.Input.Message)
	}
	if len(current) == 0 {
		return true, ""
	}
	allowed := make(map[string]struct{}, len(current))
	for _, ip := range current {
		allowed[ip] = struct{}{}
	}

	switch strings.TrimSpace(tc.Name) {
	case cloudminiProxyCheckToolName:
		ip, _ := tc.Arguments["ip"].(string)
		parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !parsed.Is4() || !containsCloudminiIP(allowed, parsed.String()) {
			return false, "chỉ được kiểm tra IP trong tin nhắn Cloudmini hiện tại; không dùng IP từ lịch sử cuộc trò chuyện"
		}
		customerEmail := latestCloudminiCustomerEmailForState(state)
		providedEmail := cloudminiToolStringArg(tc.Arguments, "account_email")
		if providedEmail != "" && (customerEmail == "" || !strings.EqualFold(providedEmail, customerEmail)) {
			return false, "account_email phải là email Cloudmini do chính khách cung cấp trong hội thoại hiện tại"
		}
		if operation, _ := tc.Arguments["operation"].(string); operation == "live_check" {
			if customerEmail == "" || providedEmail == "" || !strings.EqualFold(providedEmail, customerEmail) {
				return false, "chỉ được gọi live_check với email Cloudmini do khách đã cung cấp"
			}
			if len(state.Cloudmini.OutageCIDRs) > 0 {
				return false, "không gọi live_check cho IP thuộc subnet đang có thông báo vận hành"
			}
			var verifiedProxy bool
			for _, fact := range state.Cloudmini.ServiceFacts {
				if fact.IP != parsed.String() {
					continue
				}
				if fact.Status != "active" || !fact.AccountEmailMatches {
					return false, "chỉ được gọi live_check sau khi service_info xác minh dịch vụ active và đúng email khách hàng"
				}
				if fact.PlanFamily == "" || fact.PlanFamily == "vps" {
					return false, "live_check chỉ áp dụng cho dịch vụ Proxy đã được service_info xác minh, không áp dụng cho VPS"
				}
				verifiedProxy = true
				break
			}
			if !verifiedProxy {
				return false, "phải gọi service_info và xác minh active, đúng email, đúng dịch vụ Proxy trước khi live_check"
			}
		}
	case "escalate_to_admin":
		referenced := cloudminiHandoffIPs(tc.Arguments)
		if len(referenced) == 0 {
			return false, "Admin handoff cho yêu cầu Cloudmini hiện tại phải chứa IP trong tin nhắn khách vừa gửi"
		}
		for _, ip := range referenced {
			if !containsCloudminiIP(allowed, ip) {
				return false, "Admin handoff chỉ được chứa IP trong tin nhắn Cloudmini hiện tại; không dùng danh sách IP cũ"
			}
		}
		if requiresAllCloudminiIPs(state.Input.Message) {
			for _, ip := range current {
				if !containsCloudminiString(referenced, ip) {
					return false, "Admin handoff cho yêu cầu nhiều IP phải chứa đầy đủ toàn bộ IP trong tin nhắn khách vừa gửi"
				}
			}
		}
	}
	return true, ""
}

func cloudminiHandoffIPs(args map[string]any) []string {
	var values []string
	for _, key := range []string{"service", "summary"} {
		if value, ok := args[key].(string); ok {
			values = append(values, value)
		}
	}
	switch identifiers := args["identifiers"].(type) {
	case []any:
		for _, identifier := range identifiers {
			if value, ok := identifier.(string); ok {
				values = append(values, value)
			}
		}
	case []string:
		values = append(values, identifiers...)
	}

	seen := make(map[string]struct{})
	var ips []string
	for _, value := range values {
		for _, ip := range cloudminiIPs(value) {
			if _, exists := seen[ip]; !exists {
				seen[ip] = struct{}{}
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

func containsCloudminiIP(ips map[string]struct{}, ip string) bool {
	_, ok := ips[ip]
	return ok
}

func containsCloudminiString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func requiresAllCloudminiIPs(message string) bool {
	message = strings.ToLower(message)
	return len(cloudminiIPs(message)) > 1 && containsAny(message,
		"khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han",
		"hủy", "huỷ", "huy", "đổi", "doi", "thay", "hoàn", "hoan")
}
