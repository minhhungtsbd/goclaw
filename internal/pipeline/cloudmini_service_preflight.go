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
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cloudminiincident"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const cloudminiProxyCheckToolName = "cloudmini_proxy_check"

var cloudminiIPCandidate = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var cloudminiEmailCandidate = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
var cloudminiDeclaredIPCount = regexp.MustCompile(`(?i)\b(\d{1,3})\s*(?:ip|proxy|vps)\b`)
var cloudminiHostnameCandidate = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\b`)

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
	hosts := resolveCloudminiRequestHosts(state)
	state.Cloudmini.RequestIPs = append([]string(nil), ips...)
	state.Cloudmini.RequestHosts = append([]string(nil), hosts...)
	appendCloudminiOperationalSubnetNotice(state, ips)
	appendCloudminiResidentialVNContext(state, hosts)
	if len(ips) == 0 {
		return nil
	}
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
		if cloudminiPreflightBudgetExhausted(s.deps, state) {
			slog.Warn("cloudmini service preflight stopped at tool budget", "checked", index, "ip_count", len(ips))
			break
		}
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
				slog.Warn("cloudmini service preflight unauthorized", "ip", ip, "reason", reason)
				break
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
		if captureCloudminiServiceFacts(state, messages) == 0 {
			upsertCloudminiUnavailableFact(state, ip)
		}
		state.Tool.TotalToolCalls++

		if requiresCloudminiProxyLiveCheck(state, ip) && !cloudminiServiceDeleted(messages) && !cloudminiPreflightBudgetExhausted(s.deps, state) {
			liveArguments := map[string]any{
				"ip":        ip,
				"operation": "live_check",
			}
			if accountEmail != "" {
				liveArguments["account_email"] = accountEmail
			}
			liveCheck := providers.ToolCall{
				ID:        cloudminiPreflightCallID(state.RunID, "live_check", ip, index),
				Name:      cloudminiProxyCheckToolName,
				Arguments: liveArguments,
			}
			if ok, reason := validateCloudminiCurrentRequestToolCall(state, liveCheck); !ok {
				slog.Warn("cloudmini live preflight blocked", "ip", ip, "reason", reason)
				continue
			}
			if s.deps.AuthorizeToolCall != nil {
				if ok, reason := s.deps.AuthorizeToolCall(ctx, state, liveCheck); !ok {
					slog.Warn("cloudmini live preflight unauthorized", "ip", ip, "reason", reason)
					continue
				}
			}
			markCloudminiLiveCheckAttempt(state, ip)
			state.Messages.AppendPending(providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{liveCheck}})
			liveMessages, err := s.deps.ExecuteToolCall(ctx, state, liveCheck)
			if err != nil {
				return fmt.Errorf("execute %s: %w", cloudminiProxyCheckToolName, err)
			}
			for _, message := range liveMessages {
				state.Messages.AppendPending(message)
			}
			captureCloudminiLiveCheck(state, liveMessages)
			state.Tool.TotalToolCalls++
		}
	}
	refreshCloudminiDeterministicState(state, accountEmail)
	appendCloudminiRequestScope(state, ips)
	appendCloudminiResponseGuard(state, accountEmail)
	return nil
}

func cloudminiPreflightBudgetExhausted(deps *PipelineDeps, state *RunState) bool {
	return deps != nil && deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls >= deps.Config.MaxToolCalls
}

// prepareCloudminiToolState initializes only deterministic request scope after
// the LLM has chosen a Cloudmini-related tool. It never calls a tool itself.
// This preserves LLM-led support while preventing stale IPs from long sessions
// from leaking into a check or Admin handoff.
func prepareCloudminiToolState(state *RunState, call providers.ToolCall) {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" ||
		(call.Name != cloudminiProxyCheckToolName && call.Name != "escalate_to_admin") ||
		(len(state.Cloudmini.RequestIPs) > 0 || len(state.Cloudmini.RequestHosts) > 0) {
		return
	}
	ips := resolveCloudminiRequestIPs(state)
	hosts := resolveCloudminiRequestHosts(state)
	if len(ips) == 0 && len(hosts) == 0 {
		return
	}
	state.Cloudmini.RequestIPs = append([]string(nil), ips...)
	state.Cloudmini.RequestHosts = append([]string(nil), hosts...)
	appendCloudminiOperationalSubnetNotice(state, ips)
	appendCloudminiRequestScope(state, ips)
	appendCloudminiResidentialVNContext(state, hosts)
}

// recordCloudminiProxyCheckResult consumes facts from a service_info call that
// the LLM selected. ToolStage invokes it immediately after the tool result has
// been processed so the next LLM iteration sees deterministic email, ownership,
// live-check, scope, and handoff guards without an automatic backend API call.
func recordCloudminiProxyCheckResult(state *RunState, call providers.ToolCall, messages []providers.Message) {
	if state == nil || call.Name != cloudminiProxyCheckToolName {
		return
	}
	if strings.TrimSpace(cloudminiToolStringArg(call.Arguments, "operation")) == "live_check" {
		markCloudminiLiveCheckAttempt(state, cloudminiToolStringArg(call.Arguments, "ip"))
		captureCloudminiLiveCheck(state, messages)
		return
	}
	if strings.TrimSpace(cloudminiToolStringArg(call.Arguments, "operation")) != "service_info" {
		return
	}
	if captureCloudminiServiceFacts(state, messages) == 0 {
		upsertCloudminiUnavailableFact(state, cloudminiToolStringArg(call.Arguments, "ip"))
	}
	accountEmail := latestCloudminiCustomerEmailForState(state)
	refreshCloudminiDeterministicState(state, accountEmail)
	appendCloudminiResponseGuard(state, accountEmail)
}

func upsertCloudminiUnavailableFact(state *RunState, ip string) {
	if state == nil || strings.TrimSpace(ip) == "" {
		return
	}
	ip = strings.TrimSpace(ip)
	fact := CloudminiServiceFact{IP: ip, Status: "unavailable"}
	for index := range state.Cloudmini.ServiceFacts {
		if state.Cloudmini.ServiceFacts[index].IP == ip {
			state.Cloudmini.ServiceFacts[index] = fact
			return
		}
	}
	state.Cloudmini.ServiceFacts = append(state.Cloudmini.ServiceFacts, fact)
}

func refreshCloudminiDeterministicState(state *RunState, accountEmail string) {
	if state == nil {
		return
	}
	state.Cloudmini.EmailRequired = false
	state.Cloudmini.EmailMismatch = false
	for _, fact := range state.Cloudmini.ServiceFacts {
		if fact.Status == "email_required" || (accountEmail == "" && fact.Status != "unavailable") {
			state.Cloudmini.EmailRequired = true
		}
		// A deleted IP is intentionally exempt for recovery: its old owner is
		// irrelevant and the supplied email is the destination account. For an
		// existing service, a false match is never evidence of another owner.
		if fact.Status == "not_verified" ||
			(accountEmail != "" && fact.Status != "deleted" && !fact.AccountEmailMatches) {
			state.Cloudmini.EmailMismatch = true
			break
		}
	}
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

// appendCloudminiResidentialVNContext records the explicit exception for the
// Residential VN product. Its connection identifier is a hostname, so the
// IPv4-only Cloudmini APIs cannot be a prerequisite for customer support.
func appendCloudminiResidentialVNContext(state *RunState, hosts []string) {
	if state == nil || len(hosts) == 0 {
		return
	}
	system := state.Messages.System()
	if strings.Contains(system.Content, "[CLOUDMINI RESIDENTIAL VN HOSTNAME - BẮT BUỘC]") {
		return
	}
	system.Content += "\n\n[CLOUDMINI RESIDENTIAL VN HOSTNAME - BẮT BUỘC]\n" +
		"Định danh Residential VN của yêu cầu hiện tại: " + strings.Join(hosts, ", ") + ". " +
		"Gói này dùng hostname thay cho IPv4 dạng số. Không yêu cầu khách cung cấp IP dạng số và không gọi cloudmini_proxy_check cho hostname vì service_info/live_check có thể không hỗ trợ. " +
		"Với câu hỏi cấu hình, hướng dẫn dùng hostname ở trường Host/IP và dùng đúng port trong cột Proxy Port; không dùng user/pass khách đã gửi. " +
		"Nếu khách báo chậm, không vào được website, lỗi thực tế hoặc yêu cầu thay proxy mà hướng dẫn an toàn không giải quyết được, được phép chuyển Admin/Kỹ thuật ngay bằng hostname và email Cloudmini đã có, không cần kết quả API. " +
		"Handoff phải chứa đúng hostname hiện tại và email do khách cung cấp; tuyệt đối không chứa port:user:pass hoặc thông tin đăng nhập."
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

func resolveCloudminiRequestHosts(state *RunState) []string {
	if state == nil || state.Input == nil {
		return nil
	}
	if current := cloudminiResidentialVNHosts(state.Input.Message); len(current) > 0 {
		return current
	}
	message := strings.ToLower(state.Input.Message)
	if !isCloudminiEmailContinuation(state) && !containsAny(message,
		"lỗi", "loi", "chậm", "cham", "lag", "treo", "không", "khong", "kko", "ko ",
		"vẫn", "van", "giúp", "giup", "kiểm tra", "kiem tra", "thay proxy", "đổi proxy", "doi proxy") {
		return nil
	}
	userMessages := 0
	history := state.Messages.History()
	for i := len(history) - 1; i >= 0 && userMessages < 6; i-- {
		if history[i].Role != "user" {
			continue
		}
		userMessages++
		if hosts := cloudminiResidentialVNHosts(history[i].Content); len(hosts) > 0 {
			return hosts
		}
		// A newer numeric service identifier starts a different support scope;
		// do not revive an older Residential VN hostname across that boundary.
		if len(cloudminiIPs(history[i].Content)) > 0 {
			return nil
		}
	}
	return nil
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
	if incidents, err := cloudminiincident.ParseContext(system.Content); err == nil {
		matchedIDs := make(map[string]bool)
		for _, ip := range ips {
			if incident := cloudminiincident.Match(incidents, ip, state.Input.AgentKey, time.Now().UTC()); incident != nil {
				if state.Cloudmini.IncidentsByIP == nil {
					state.Cloudmini.IncidentsByIP = make(map[string]store.OperationalIncident)
				}
				state.Cloudmini.IncidentsByIP[ip] = *incident
				if incident.Severity == "permanent_outage" {
					state.Cloudmini.OutageCIDRs = appendUniqueCloudminiIPs(state.Cloudmini.OutageCIDRs, incident.CIDRs...)
				}
				if matchedIDs[incident.ID] {
					continue
				}
				matchedIDs[incident.ID] = true
				encoded, _ := json.Marshal(incident)
				block := "\n\n[THÔNG BÁO VẬN HÀNH CÓ CẤU TRÚC - BẮT BUỘC ĐỐI CHIẾU]\n" +
					"Chỉ dẫn: đây là context vận hành, không ghi đè kết quả tool mới nhất. " +
					"Không nâng mức độ sự cố và không tự thêm phương án đổi/hoàn tiền.\n" +
					"<matched_operational_incident>" + string(encoded) + "</matched_operational_incident>"
				system.Content += block
			}
		}
		if len(matchedIDs) > 0 {
			state.Messages.SetSystem(system)
			return
		}
	}
	// Legacy free-form AGENTS.md outage sections are deliberately ignored. Only
	// validated <operational_incidents> records may affect runtime decisions.
}

func requiresCloudminiProxyLiveCheck(state *RunState, ip string) bool {
	message := strings.ToLower(cloudminiSupportIntentText(state))
	if containsAny(message,
		"khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han",
		"hủy", "huỷ", "huy", "hoàn", "hoan", "đổi ip", "doi ip", "thay ip") {
		return false
	}
	if !containsAny(message, "lỗi", "loi", "không kết nối", "khong ket noi", "check live", "die", "error", "timeout", "không hoạt động", "khong hoat dong") ||
		cloudminiIPInOutage(state, ip) {
		return false
	}
	for _, fact := range state.Cloudmini.ServiceFacts {
		if fact.IP == ip && fact.Status == "active" && fact.AccountEmailMatches &&
			fact.Plan != "" && fact.PlanFamily != "vps" && !strings.Contains(strings.ToLower(fact.Plan), "vps") {
			return true
		}
	}
	return false
}

func cloudminiIPInOutage(state *RunState, value string) bool {
	if state == nil {
		return false
	}
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	for _, raw := range state.Cloudmini.OutageCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func captureCloudminiLiveCheck(state *RunState, messages []providers.Message) int {
	if state == nil {
		return 0
	}
	captured := 0
	for _, message := range messages {
		if message.Role != "tool" || message.IsError {
			continue
		}
		var response struct {
			LiveCheck struct {
				IP   string `json:"ip"`
				Live *bool  `json:"live"`
			} `json:"live_check"`
		}
		if json.Unmarshal([]byte(message.Content), &response) != nil || response.LiveCheck.IP == "" || response.LiveCheck.Live == nil {
			continue
		}
		if state.Cloudmini.LiveChecks == nil {
			state.Cloudmini.LiveChecks = make(map[string]bool)
		}
		state.Cloudmini.LiveChecks[response.LiveCheck.IP] = *response.LiveCheck.Live
		captured++
	}
	return captured
}

func markCloudminiLiveCheckAttempt(state *RunState, ip string) {
	if state == nil || strings.TrimSpace(ip) == "" {
		return
	}
	if state.Cloudmini.LiveAttempts == nil {
		state.Cloudmini.LiveAttempts = make(map[string]bool)
	}
	state.Cloudmini.LiveAttempts[strings.TrimSpace(ip)] = true
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
			status := strings.TrimSpace(service.ServiceStatus)
			// expire may be redacted to null for email_required/not_verified.
			// Only infer deleted for legacy tool payloads that omit service_status;
			// an explicit sanitized status is always authoritative.
			if status == "" && strings.TrimSpace(string(service.Expire)) == "null" {
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

func isCloudminiServiceRequest(state *RunState) bool {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" {
		return false
	}
	message := strings.ToLower(state.Input.Message)
	if isCloudminiEmailContinuation(state) {
		return true
	}
	if len(resolveCloudminiRequestHosts(state)) > 0 {
		return true
	}
	if len(cloudminiIPs(message)) == 0 {
		return false
	}
	return containsAny(message,
		"cloudmini", "proxy", "vps", "kiểm tra", "kiem tra", "check", "lỗi", "loi", "error",
		"không kết nối", "khong ket noi", "không hoạt động", "khong hoat dong", "timeout", "die",
		"khôi phục", "khoi phuc", "phục hồi", "phuc hoi", "gia hạn", "gia han", "hủy", "huỷ", "huy",
		"hoàn tiền", "hoan tien", "đổi ip", "doi ip", "thay ip")
}

func isCloudminiEmailContinuation(state *RunState) bool {
	if state == nil || state.Input == nil || len(cloudminiIPs(state.Input.Message)) > 0 ||
		len(cloudminiResidentialVNHosts(state.Input.Message)) > 0 ||
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
				return len(cloudminiIPs(history[j].Content)) > 0 || len(cloudminiResidentialVNHosts(history[j].Content)) > 0
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
		if state.Messages.History()[i].Role == "user" &&
			(len(cloudminiIPs(state.Messages.History()[i].Content)) > 0 || len(cloudminiResidentialVNHosts(state.Messages.History()[i].Content)) > 0) {
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
				"Không nói IP thuộc tài khoản khác, chủ sở hữu khác, email không khớp, IP đang LIVE, chuyển nhượng hoặc đề xuất mua IP mới. Không tiết lộ dữ liệu đối chiếu nội bộ và không gọi live_check. Bắt buộc gọi escalate_to_admin với đúng IP và email Cloudmini khách đã cung cấp để Admin kiểm tra trực tiếp. Chỉ được xác nhận đã chuyển sau khi tool thành công và phải kèm mã Ticket thật."
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

func cloudminiResidentialVNHosts(message string) []string {
	seen := make(map[string]struct{})
	hosts := make([]string, 0, 1)
	for _, candidate := range cloudminiHostnameCandidate.FindAllString(message, -1) {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate), "."))
		if host != "resvn.net" && !strings.HasSuffix(host, ".resvn.net") {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
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
	currentIPs := state.Cloudmini.RequestIPs
	if len(currentIPs) == 0 {
		currentIPs = cloudminiIPs(state.Input.Message)
	}
	currentHosts := state.Cloudmini.RequestHosts
	if len(currentHosts) == 0 {
		currentHosts = cloudminiResidentialVNHosts(state.Input.Message)
	}
	if len(currentIPs) == 0 && len(currentHosts) == 0 {
		return true, ""
	}
	allowedIPs := make(map[string]struct{}, len(currentIPs))
	for _, ip := range currentIPs {
		allowedIPs[ip] = struct{}{}
	}
	allowedHosts := make(map[string]struct{}, len(currentHosts))
	for _, host := range currentHosts {
		allowedHosts[strings.ToLower(host)] = struct{}{}
	}

	switch strings.TrimSpace(tc.Name) {
	case cloudminiProxyCheckToolName:
		if len(currentIPs) == 0 && len(currentHosts) > 0 {
			return false, "Residential VN dùng hostname; không gọi service_info/live_check và không yêu cầu khách cung cấp IPv4 dạng số"
		}
		ip, _ := tc.Arguments["ip"].(string)
		parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil || !parsed.Is4() || !containsCloudminiIP(allowedIPs, parsed.String()) {
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
			if cloudminiIPInOutage(state, parsed.String()) {
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
		intent := strings.ToLower(cloudminiSupportIntentText(state))
		needsOperationalReview := containsAny(intent,
			"lỗi", "loi", "không kết nối", "khong ket noi", "không vào được", "khong vao duoc",
			"không hoạt động", "khong hoat dong", "die", "error", "timeout", "manual", "kỹ thuật", "ky thuat",
			"chậm", "cham", "lag", "treo", "không load", "khong load", "thay proxy", "đổi proxy", "doi proxy",
			"khôi phục", "khoi phuc", "gia hạn", "gia han", "hủy", "huỷ", "huy", "hoàn tiền", "hoan tien")
		referencedIPs := cloudminiHandoffIPs(tc.Arguments)
		referencedHosts := cloudminiHandoffResidentialVNHosts(tc.Arguments)
		if len(referencedIPs) == 0 && len(referencedHosts) == 0 {
			return false, "Admin handoff cho yêu cầu Cloudmini hiện tại phải chứa đúng IP hoặc hostname Residential VN hiện tại"
		}
		if len(currentIPs) > 0 && len(referencedIPs) == 0 {
			return false, "Admin handoff cho yêu cầu Cloudmini hiện tại phải chứa IP trong tin nhắn khách vừa gửi"
		}
		if cloudminiIncidentBlocksHandoff(state, referencedIPs) {
			return false, "thông báo vận hành đã match IP này không cho phép tạo Admin handoff; phải giải thích đúng thông báo cho khách"
		}
		if !needsOperationalReview && !cloudminiIncidentsAllowHandoff(state, referencedIPs) {
			return false, "chỉ tạo Admin handoff khi khách báo lỗi/cần xử lý kỹ thuật hoặc incident cho phép handoff"
		}
		for _, ip := range referencedIPs {
			if !containsCloudminiIP(allowedIPs, ip) {
				return false, "Admin handoff chỉ được chứa IP trong tin nhắn Cloudmini hiện tại; không dùng danh sách IP cũ"
			}
		}
		if blocked, reason := cloudminiHandoffNeedsMoreLiveTriage(state, referencedIPs, intent); blocked {
			return false, reason
		}
		if requiresAllCloudminiIPs(state.Input.Message) {
			for _, ip := range currentIPs {
				if !containsCloudminiString(referencedIPs, ip) {
					return false, "Admin handoff cho yêu cầu nhiều IP phải chứa đầy đủ toàn bộ IP trong tin nhắn khách vừa gửi"
				}
			}
		}
		if len(currentHosts) > 0 {
			if len(referencedHosts) == 0 {
				return false, "Admin handoff Residential VN phải chứa hostname hiện tại; không yêu cầu IPv4 dạng số"
			}
			for _, host := range referencedHosts {
				if _, ok := allowedHosts[strings.ToLower(host)]; !ok {
					return false, "Admin handoff chỉ được chứa hostname Residential VN trong yêu cầu hiện tại"
				}
			}
			for _, host := range currentHosts {
				if !containsCloudminiString(referencedHosts, strings.ToLower(host)) {
					return false, "Admin handoff Residential VN phải chứa đầy đủ hostname hiện tại"
				}
			}
			customerEmail := latestCloudminiCustomerEmailForState(state)
			if customerEmail == "" {
				return false, "phải xin email tài khoản Cloudmini trước khi chuyển Admin cho Residential VN"
			}
			if !containsCloudminiString(cloudminiHandoffEmails(tc.Arguments), customerEmail) {
				return false, "Admin handoff Residential VN phải dùng đúng email Cloudmini do khách cung cấp"
			}
		}
	}
	return true, ""
}

func cloudminiHandoffNeedsMoreLiveTriage(state *RunState, ips []string, intent string) (bool, string) {
	if state == nil || cloudminiIncidentsAllowHandoff(state, ips) ||
		!containsAny(intent, "lỗi", "loi", "error", "không kết nối", "khong ket noi", "die", "timeout") ||
		containsAny(intent, "đã thử", "da thu", "warp", "4g", "5g", "mạng khác", "mang khac", "restart", "khởi động", "khoi dong", "ứng dụng khác", "ung dung khac", "xóa cache", "xoa cache") {
		return false, ""
	}
	for _, ip := range ips {
		for _, fact := range state.Cloudmini.ServiceFacts {
			if fact.IP != ip || fact.Status != "active" || !fact.AccountEmailMatches || fact.PlanFamily == "vps" {
				continue
			}
			live, checked := state.Cloudmini.LiveChecks[ip]
			if !checked {
				if state.Cloudmini.LiveAttempts[ip] {
					// The required check ran but returned no usable status. This is a
					// tool/service failure and may be escalated with that evidence.
					continue
				}
				return true, "phải hoàn tất live_check cho Proxy active đã xác minh trước khi tạo Admin handoff lỗi kết nối"
			}
			if live {
				return true, "Proxy đang LIVE; phải giải thích trạng thái và hướng dẫn bước chẩn đoán phù hợp trước khi chuyển Admin"
			}
		}
	}
	return false, ""
}

func cloudminiIncidentsAllowHandoff(state *RunState, ips []string) bool {
	if state == nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		incident, ok := state.Cloudmini.IncidentsByIP[ip]
		if !ok || !incident.AllowsAdminHandoff {
			return false
		}
	}
	return true
}

func cloudminiIncidentBlocksHandoff(state *RunState, ips []string) bool {
	if state == nil {
		return false
	}
	for _, ip := range ips {
		if incident, matched := state.Cloudmini.IncidentsByIP[ip]; matched && !incident.AllowsAdminHandoff {
			return true
		}
	}
	return false
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

func cloudminiHandoffResidentialVNHosts(args map[string]any) []string {
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
	var hosts []string
	for _, value := range values {
		for _, host := range cloudminiResidentialVNHosts(value) {
			if _, exists := seen[host]; !exists {
				seen[host] = struct{}{}
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}

func cloudminiHandoffEmails(args map[string]any) []string {
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
	var emails []string
	for _, value := range values {
		for _, email := range cloudminiEmailCandidate.FindAllString(value, -1) {
			email = strings.ToLower(email)
			if _, exists := seen[email]; !exists {
				seen[email] = struct{}{}
				emails = append(emails, email)
			}
		}
	}
	return emails
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
