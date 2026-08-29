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
	accountEmail := latestCloudminiCustomerEmail(state.Messages.History())
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
	state.Cloudmini.HandoffRequired = requiresDeterministicCloudminiHandoff(state)
	appendCloudminiRequestScope(state, ips)
	return nil
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
	message := strings.ToLower(state.Input.Message)
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

func captureCloudminiServiceFacts(state *RunState, messages []providers.Message) {
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		var response struct {
			Services []struct {
				IP                  string          `json:"ip"`
				Plan                string          `json:"plan"`
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
			if string(service.Expire) == "null" {
				status = "deleted"
			}
			state.Cloudmini.ServiceFacts = append(state.Cloudmini.ServiceFacts, CloudminiServiceFact{
				IP:                  service.IP,
				Plan:                service.Plan,
				Status:              status,
				AccountEmailMatches: service.AccountEmailMatches,
				CancellationPolicy:  service.CancellationPolicy,
			})
		}
	}
}

func requiresDeterministicCloudminiHandoff(state *RunState) bool {
	if state.Cloudmini.ScopeAmbiguous || len(state.Cloudmini.ServiceFacts) == 0 {
		return false
	}
	lower := strings.ToLower(state.Input.Message)
	restore := containsAny(lower, "khôi phục", "khoi phuc", "phục hồi", "phuc hoi")
	customerEmailKnown := latestCloudminiCustomerEmail(state.Messages.History()) != ""
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
		allVPS = allVPS && strings.Contains(strings.ToLower(fact.Plan), "vps")
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
	return len(cloudminiIPs(message)) > 0
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
