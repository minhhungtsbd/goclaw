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

	result, err := sanitizeCloudminiProxyResponse(operation, ip.String(), argString(args, "account_email"), t.settings(ctx).ResellerEmails, body)
	if err != nil {
		return ErrorResult("Cloudmini proxy check returned an invalid response")
	}
	return SilentResult(result)
}

func (t *CloudminiProxyCheckTool) allowedForAgent(ctx context.Context) bool {
	settings := t.settings(ctx)
	if len(settings.AllowedAgentKeys) == 0 {
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
	settings := t.settings(ctx)
	if settings.TimeoutSeconds < 3 || settings.TimeoutSeconds > 60 {
		return defaultCloudminiTimeout
	}
	return time.Duration(settings.TimeoutSeconds) * time.Second
}

type cloudminiProxyCheckSettings struct {
	AllowedAgentKeys []string `json:"allowed_agent_keys"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	ResellerEmails   []string `json:"reseller_emails"`
}

func (t *CloudminiProxyCheckTool) settings(ctx context.Context) cloudminiProxyCheckSettings {
	var settings cloudminiProxyCheckSettings
	if raw := BuiltinToolSettingsFromCtx(ctx)[t.Name()]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &settings)
	}
	return settings
}

func isResellerEmail(email string, allowed []string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, candidate := range allowed {
		if email != "" && email == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func isResidentialStaticPlan(plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	return strings.Contains(p, "residential static")
}

func isVPSPlan(plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	vpsKeywords := []string{"vps", "custom", "mini", "promo", "yt", "server", "dedicated"}
	for _, kw := range vpsKeywords {
		if strings.Contains(p, kw) {
			return true
		}
	}
	if strings.HasPrefix(p, "nn") {
		return true
	}
	return false
}

// cancellationPolicy is returned only after the customer has verified their
// account email. It makes the cancellation decision explicit so the agent does
// not turn a dashboard error into an unnecessary Admin handoff.
func cancellationPolicy(plan string) (string, string) {
	p := strings.ToLower(strings.TrimSpace(plan))

	switch {
	case strings.Contains(p, "budgetv4"),
		strings.Contains(p, "budget v4"),
		strings.Contains(p, "privatev6"),
		strings.Contains(p, "private v6"),
		strings.Contains(p, "residential static"),
		strings.Contains(p, "residential vn"),
		strings.Contains(p, "rotating residential"),
		strings.HasPrefix(p, "nn"):
		return "not_supported", "Gói này không hỗ trợ hủy hoặc hoàn tiền theo nhu cầu. Lỗi nút hủy trên web không tạo thành yêu cầu Admin; phải thông báo đúng chính sách gói và không nói yêu cầu đang chờ xử lý."
	case strings.Contains(p, "privatev4"),
		strings.Contains(p, "private v4"),
		strings.Contains(p, "custom"),
		strings.Contains(p, "mini"),
		strings.Contains(p, "promo"),
		strings.Contains(p, "yt"):
		return "self_service", "Gói này cho phép khách tự hủy trong trang quản lý. Nếu khách đã xác minh đúng email và vẫn có lỗi hủy thực tế trên web, có thể tạo Admin handoff để kiểm tra thao tác; không hứa hoàn tất hoặc mức hoàn trước khi có kết quả."
	default:
		return "review_required", "Chưa xác định được chính sách hủy tự động cho gói này. Không tạo Admin handoff chỉ vì lỗi nút hủy; cần đối chiếu chính sách Cloudmini trước."
	}
}

func getServiceClassificationNote(plan string) string {
	if isVPSPlan(plan) {
		return " [PHÂN LOẠI: DỊCH VỤ VPS (Gói: " + plan + "). BẮT BUỘC xử lý theo Nhánh 2 (VPS). TUYỆT ĐỐI KHÔNG gọi tool live_check.]"
	}
	return " [PHÂN LOẠI: DỊCH VỤ PROXY (Gói: " + plan + "). Xử lý theo Nhánh 1 (Proxy).]"
}

func sanitizeCloudminiProxyResponse(operation, ip, accountEmail string, resellerEmails []string, body []byte) (string, error) {
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
			var single cloudminiServiceInfo
			if errSingle := json.Unmarshal(response.Data, &single); errSingle == nil {
				items = []cloudminiServiceInfo{single}
			} else {
				return "", err
			}
		}
		expectedEmail := strings.TrimSpace(accountEmail)
		for i := range items {
			items[i].setServiceStatus()
			rawUserEmail := strings.TrimSpace(items[i].UserEmail)
			items[i].UserEmail = "" // Redact system account email from LLM view

			classNote := getServiceClassificationNote(items[i].Plan)
			isResStatic := isResidentialStaticPlan(items[i].Plan)

			if expectedEmail != "" {
				if rawUserEmail != "" {
					matches := strings.EqualFold(rawUserEmail, expectedEmail)
					items[i].AccountEmailMatches = &matches

					if matches {
						isReseller := isResellerEmail(rawUserEmail, resellerEmails)
						if isReseller {
							items[i].IsReseller = &isReseller
							items[i].IsResellerVIP = &isReseller
						}
						if isReseller {
							items[i].StatusNote = "IP thuộc tài khoản Reseller của khách hàng." + classNote + " Được phép ưu tiên hỗ trợ và chuyển Admin theo yêu cầu."
						} else if items[i].ServiceStatus == "deleted" {
							if isResStatic {
								items[i].StatusNote = "IP đã bị xóa đúng tài khoản của khách hàng." + classNote + " [QUY TẮC KHÔI PHỤC GÓI RESIDENTIAL STATIC]: BẮT BUỘC thông báo khách phí khôi phục IP cũ là 25.000đ/IP (nếu còn tài nguyên IP cũ) và YÊU CẦU KHÁCH NẠP ĐỦ SỐ DƯ TÀI KHOẢN CLOUDMINI = TỔNG (GIÁ CƯỚC PROXY + PHÍ KHÔI PHỤC 25.000đ/IP) để Admin tiến hành khôi phục thủ công."
							} else {
								items[i].StatusNote = "IP đã bị xóa đúng tài khoản của khách hàng." + classNote + " Thông báo dịch vụ đã bị xóa/hết hạn và chuyển Admin kiểm tra khả năng khôi phục."
							}
						} else {
							items[i].StatusNote = "IP đang hoạt động đúng tài khoản của khách hàng." + classNote
							items[i].CancellationPolicy, items[i].CancellationInstruction = cancellationPolicy(items[i].Plan)
						}
					} else {
						if items[i].ServiceStatus == "linked" || items[i].ServiceStatus == "active" {
							items[i].ServiceStatus = "unavailable"
							items[i].StatusNote = "IP hiện không còn khả dụng." + classNote
							items[i].Expire = nil // Redact expiry when email does not match
						}
					}
				}
			} else {
				if items[i].ServiceStatus == "linked" || items[i].ServiceStatus == "active" {
					items[i].ServiceStatus = "active"
					items[i].StatusNote = "IP đang có thông tin dịch vụ." + classNote + " YÊU CẦU BẮT BUỘC BƯỚC 2: Nếu trong toàn bộ cuộc trò chuyện CHƯA CÓ email của khách hàng, BẮT BUỘC phải hỏi xin email tài khoản Cloudmini của khách trước hết. KHÔNG ĐƯỢC đưa ra hướng dẫn kỹ thuật chuyên sâu hay kết luận hỗ trợ khi chưa có email."
					items[i].Expire = nil // Redact expiry until email is supplied & verified
				} else if items[i].ServiceStatus == "deleted" {
					if isResStatic {
						items[i].StatusNote = "IP đã bị xóa." + classNote + " [QUY TẮC KHÔI PHỤC GÓI RESIDENTIAL STATIC]: BẮT BUỘC thông báo khách phí khôi phục IP cũ là 25.000đ/IP (nếu còn tài nguyên IP cũ) và YÊU CẦU KHÁCH NẠP ĐỦ SỐ DƯ TÀI KHOẢN CLOUDMINI = TỔNG (GIÁ CƯỚC PROXY + PHÍ KHÔI PHỤC 25.000đ/IP) để Admin tiến hành khôi phục thủ công."
					} else {
						items[i].StatusNote = "IP đã bị xóa." + classNote + " Thông báo dịch vụ đã bị xóa/hết hạn và chuyển Admin kiểm tra khả năng khôi phục."
					}
				}
			}
		}
		result["services"] = items
	} else {
		isLive := !response.Error
		if len(response.Data) > 0 {
			var rawMap map[string]any
			if err := json.Unmarshal(response.Data, &rawMap); err == nil {
				if l, ok := rawMap["live"].(bool); ok {
					isLive = l
				} else if s, ok := rawMap["status"].(string); ok {
					sLower := strings.ToLower(s)
					if sLower == "die" || sLower == "dead" || sLower == "failed" || sLower == "error" {
						isLive = false
					} else if sLower == "live" || sLower == "ok" || sLower == "active" {
						isLive = true
					}
				} else if lStr, ok := rawMap["live"].(string); ok {
					if strings.ToLower(lStr) == "false" || strings.ToLower(lStr) == "die" {
						isLive = false
					}
				}
			}
		}

		statusText := "LIVE (Proxy đang kết nối hoạt động bình thường)"
		if !isLive {
			statusText = "DIE (Proxy gián đoạn hoặc không kết nối được)"
		}

		result["live_check"] = map[string]any{
			"ip":     ip,
			"live":   isLive,
			"status": statusText,
		}
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

type cloudminiServiceInfo struct {
	IP                      string  `json:"ip"`
	Plan                    string  `json:"plan"`
	UserEmail               string  `json:"user_email,omitempty"`
	Expire                  *string `json:"expire"`
	Region                  string  `json:"region"`
	ServiceStatus           string  `json:"service_status"`
	StatusNote              string  `json:"status_note,omitempty"`
	AccountEmailMatches     *bool   `json:"account_email_matches,omitempty"`
	IsReseller              *bool   `json:"is_reseller,omitempty"`
	IsResellerVIP           *bool   `json:"is_reseller_vip,omitempty"`
	CancellationPolicy      string  `json:"cancellation_policy,omitempty"`
	CancellationInstruction string  `json:"cancellation_instruction,omitempty"`
}

type cloudminiLiveCheck struct {
	IP       string `json:"ip"`
	Live     bool   `json:"live"`
	HttpCode int    `json:"http_code"`
}

func (s *cloudminiServiceInfo) setServiceStatus() {
	if s.Expire == nil {
		s.ServiceStatus = "deleted"
		s.StatusNote = "IP đã bị xóa và không còn gắn với dịch vụ nào."
		return
	}
	s.ServiceStatus = "active"
	s.StatusNote = "IP đang có thông tin dịch vụ."
}
