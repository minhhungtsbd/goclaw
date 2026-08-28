package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const cloudminiProxyCheckToolName = "cloudmini_proxy_check"

var cloudminiIPCandidate = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

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
	ips := cloudminiIPs(state.Input.Message)
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
	for index, ip := range ips {
		tc := providers.ToolCall{
			ID:   fmt.Sprintf("cloudmini-service-preflight-%d", index+1),
			Name: cloudminiProxyCheckToolName,
			Arguments: map[string]any{
				"ip":        ip,
				"operation": "service_info",
			},
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
		state.Tool.TotalToolCalls++

		if requiresCloudminiProxyLiveCheck(state.Input.Message) && !cloudminiServiceDeleted(messages) {
			liveCheck := providers.ToolCall{
				ID:   fmt.Sprintf("cloudmini-live-preflight-%d", index+1),
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
	appendCloudminiRequestScope(state, ips)
	return nil
}

// appendCloudminiRequestScope pins a multi-IP support run to the customer's
// current list. Long-running support sessions can otherwise make an LLM reuse
// an older proxy list after it has received the current service facts.
func appendCloudminiRequestScope(state *RunState, ips []string) {
	if len(ips) == 0 {
		return
	}
	system := state.Messages.System()
	system.Content += "\n\n[PHẠM VI YÊU CẦU CLOUDMINI HIỆN TẠI]\n" +
		"Chỉ xử lý các IP có trong tin nhắn khách vừa gửi: " + strings.Join(ips, ", ") + ".\n" +
		"Không dùng, không kiểm tra lại và không đưa IP từ các danh sách cũ trong lịch sử vào phản hồi hoặc Admin handoff. " +
		"Nếu cần chuyển Admin cho yêu cầu nhiều IP này, ticket phải chứa đúng toàn bộ các IP trên."
	state.Messages.SetSystem(system)
}

func requiresCloudminiProxyLiveCheck(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "proxy") && containsAny(message, "lỗi", "không kết nối", "check live", "die")
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
	current := cloudminiIPs(state.Input.Message)
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
