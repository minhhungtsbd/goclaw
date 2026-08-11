package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	cloudminiProxyAPIBaseURL = "https://client.cloudmini.net/api/v2/admin"
	cloudminiProxyTokenKey   = "tools.cloudmini_proxy.api_token"
	defaultCloudminiTimeout  = 15 * time.Second
	maxCloudminiResponseSize = 128 << 10
)

// CloudminiProxyCheckTool queries the two fixed Cloudmini admin endpoints.
// The credential is stored encrypted and never accepted as a tool argument.
type CloudminiProxyCheckTool struct {
	secrets store.ConfigSecretsStore
	client  *http.Client
	baseURL string
}

func NewCloudminiProxyCheckTool(secrets store.ConfigSecretsStore) *CloudminiProxyCheckTool {
	return &CloudminiProxyCheckTool{secrets: secrets, baseURL: cloudminiProxyAPIBaseURL}
}

func (t *CloudminiProxyCheckTool) Name() string { return "cloudmini_proxy_check" }

func (t *CloudminiProxyCheckTool) Description() string {
	return "Check Cloudmini Proxy or VPS service details, expiry, plan, account email, or live GeoIP for one IP. REQUIRED before replying or escalating when a customer provides an IP for a fault, recovery, renewal, cancellation, replacement, refund, or configuration request: call service_info first to identify the plan and expiry. For a Proxy connection fault, call live_check after service_info unless the service is deleted. Use only for customer support after the customer provides the IP or it is already present in the conversation."
}

func (t *CloudminiProxyCheckTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ip": map[string]any{
				"type": "string", "description": "The IPv4 or IPv6 proxy address to check.",
			},
			"operation": map[string]any{
				"type": "string", "enum": []string{"service_info", "live_check"},
				"description": "service_info checks Cloudmini service/plan/expiry. live_check checks live GeoIP details.",
			},
			"account_email": map[string]any{
				"type": "string", "description": "Optional Cloudmini account email already supplied by the customer. service_info compares it with the service record.",
			},
		},
		"required": []string{"ip", "operation"},
	}
}

func (t *CloudminiProxyCheckTool) Execute(ctx context.Context, args map[string]any) *Result {
	if !t.allowedForAgent(ctx) {
		return ErrorResult("Cloudmini proxy check is not enabled for this agent")
	}
	if t.secrets == nil {
		return ErrorResult("Cloudmini proxy check is not configured")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(argString(args, "ip")))
	if err != nil {
		return ErrorResult("ip must be a valid IPv4 or IPv6 address")
	}
	operation := strings.TrimSpace(argString(args, "operation"))
	endpoint := ""
	switch operation {
	case "service_info":
		endpoint = "check_services"
	case "live_check":
		endpoint = "check_live_proxy"
	default:
		return ErrorResult("operation must be service_info or live_check")
	}
	token, err := t.secrets.Get(ctx, cloudminiProxyTokenKey)
	if err != nil || strings.TrimSpace(token) == "" {
		return ErrorResult("Cloudmini proxy check is not configured: add the Cloudmini API token in Built-in Tools settings")
	}

	requestURL := t.baseURL + "/" + endpoint + "?ip=" + url.QueryEscape(ip.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return ErrorResult("Cloudmini proxy check could not create the request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+strings.TrimSpace(token))

	client := t.client
	if client == nil {
		client = &http.Client{Timeout: t.timeout(ctx)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrorResult("Cloudmini proxy check failed to reach the service")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCloudminiResponseSize))
	if err != nil {
		return ErrorResult("Cloudmini proxy check could not read the service response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ErrorResult(fmt.Sprintf("Cloudmini proxy check returned HTTP %d", resp.StatusCode))
	}

	result, err := sanitizeCloudminiProxyResponse(operation, ip.String(), argString(args, "account_email"), body)
	if err != nil {
		return ErrorResult("Cloudmini proxy check returned an invalid response")
	}
	return SilentResult(result)
}

func (t *CloudminiProxyCheckTool) allowedForAgent(ctx context.Context) bool {
	var settings struct {
		AllowedAgentKeys []string `json:"allowed_agent_keys"`
	}
	if raw := BuiltinToolSettingsFromCtx(ctx)[t.Name()]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &settings)
	}
	if len(settings.AllowedAgentKeys) == 0 {
		// Agent-specific tool policy still controls visibility and invocation.
		return true
	}
	agentKey := ToolAgentKeyFromCtx(ctx)
	for _, allowed := range settings.AllowedAgentKeys {
		if agentKey != "" && agentKey == strings.TrimSpace(allowed) {
			return true
		}
	}
	return false
}

func (t *CloudminiProxyCheckTool) timeout(ctx context.Context) time.Duration {
	var settings struct {
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if raw := BuiltinToolSettingsFromCtx(ctx)[t.Name()]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &settings)
	}
	if settings.TimeoutSeconds < 3 || settings.TimeoutSeconds > 60 {
		return defaultCloudminiTimeout
	}
	return time.Duration(settings.TimeoutSeconds) * time.Second
}

func sanitizeCloudminiProxyResponse(operation, ip, accountEmail string, body []byte) (string, error) {
	var response struct {
		Error bool            `json:"error"`
		Msg   string          `json:"msg"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	result := map[string]any{"operation": operation, "ip": ip, "success": !response.Error, "message": response.Msg}
	if response.Error || len(response.Data) == 0 {
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	if operation == "service_info" {
		var items []cloudminiServiceInfo
		if err := json.Unmarshal(response.Data, &items); err != nil {
			return "", err
		}
		expectedEmail := strings.TrimSpace(accountEmail)
		for i := range items {
			items[i].setServiceStatus()
			if expectedEmail == "" || strings.TrimSpace(items[i].UserEmail) == "" {
				continue
			}
			matches := strings.EqualFold(strings.TrimSpace(items[i].UserEmail), expectedEmail)
			items[i].AccountEmailMatches = &matches
		}
		result["services"] = items
	} else {
		var live struct {
			IP        string `json:"ip"`
			Country   string `json:"country"`
			StateProv string `json:"state_prov"`
			City      string `json:"city"`
			Zipcode   string `json:"zipcode"`
		}
		if err := json.Unmarshal(response.Data, &live); err != nil {
			return "", err
		}
		result["live"] = live
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

// cloudminiServiceInfo intentionally keeps the account email in the internal
// tool result: support needs it for matching a customer-provided email and an
// Admin handoff. Agent instructions prohibit disclosing it to the customer.
type cloudminiServiceInfo struct {
	ID                  any    `json:"id"`
	IP                  string `json:"ip"`
	Expire              *string `json:"expire"`
	Plan                string `json:"plan"`
	UserEmail           string `json:"user_email,omitempty"`
	AccountEmailMatches *bool  `json:"account_email_matches,omitempty"`
	ServiceStatus       string `json:"service_status"`
	StatusNote          string `json:"status_note"`
}

func (s *cloudminiServiceInfo) setServiceStatus() {
	if s.Expire == nil {
		s.ServiceStatus = "deleted"
		s.StatusNote = "IP đã bị xóa và không còn gắn với dịch vụ nào."
		return
	}
	s.ServiceStatus = "linked"
	s.StatusNote = "IP vẫn đang gắn với dịch vụ."
}
