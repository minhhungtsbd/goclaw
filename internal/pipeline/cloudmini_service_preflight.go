package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
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
	if !requiresCloudminiServiceLookup(state) || s.deps.ExecuteToolCall == nil {
		return nil
	}

	state.Tool.AllowedTools = map[string]bool{cloudminiProxyCheckToolName: true}
	for index, ip := range cloudminiIPs(state.Input.Message) {
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
	return nil
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

func requiresCloudminiServiceLookup(state *RunState) bool {
	if state == nil || state.Input == nil || state.Input.SenderID == "system:admin_handoff" || !hasCloudminiProxyCheckTool(state.Think.Tools) {
		return false
	}
	message := strings.ToLower(state.Input.Message)
	if len(cloudminiIPs(message)) == 0 {
		return false
	}
	return containsAny(message,
		"lỗi", "không kết nối", "check live", "khôi phục", "gia hạn", "hủy", "đổi", "hoàn", "thay thế", "refund", "renew", "cancel", "replace")
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
