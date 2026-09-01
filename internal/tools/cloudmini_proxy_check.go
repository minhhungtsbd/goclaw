package tools

import (
	"context"
	"encoding/json"
	"errors"
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
	return "Look up facts for one Cloudmini IP when the customer asks about that specific Proxy or VPS service. service_info returns service facts for the LLM to interpret with the Cloudmini support skill; it is not a general-IP lookup. Use account_email when the customer has supplied it. live_check is only for an active, email-verified Proxy connection fault after service_info in the same case; it requires account_email and must never be used for policy questions, VPS, deleted, expired, email_required, or not_verified services."
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
				"description": "service_info checks Cloudmini service/plan/expiry. live_check checks Proxy live status only after verified active service_info; never use it for VPS, deleted, expired, email_required, or not_verified results.",
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
	if operation == "live_check" && strings.TrimSpace(argString(args, "account_email")) == "" {
		return ErrorResult("live_check requires the customer Cloudmini account_email after verified service_info")
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
		if operation == "live_check" {
			return cloudminiLiveCheckResult(ip.String(), false)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrorResult("Cloudmini service information check timed out")
		}
		return ErrorResult("Cloudmini proxy check failed to reach the service")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCloudminiResponseSize))
	if err != nil {
		if operation == "live_check" {
			return cloudminiLiveCheckResult(ip.String(), false)
		}
		return ErrorResult("Cloudmini proxy check could not read the service response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if operation == "live_check" {
			return cloudminiLiveCheckResult(ip.String(), false)
		}
		return ErrorResult(fmt.Sprintf("Cloudmini proxy check returned HTTP %d", resp.StatusCode))
	}

	result, err := sanitizeCloudminiProxyResponse(operation, ip.String(), argString(args, "account_email"), t.settings(ctx).ResellerEmails, body)
	if err != nil {
		if operation == "live_check" {
			return cloudminiLiveCheckResult(ip.String(), false)
		}
		return ErrorResult("Cloudmini proxy check returned an invalid response")
	}
	return SilentResult(result)
}

func cloudminiLiveCheckResult(ip string, live bool) *Result {
	payload, err := marshalCloudminiLiveCheckResult(ip, live)
	if err != nil {
		return ErrorResult("Cloudmini proxy check could not encode the result")
	}
	return SilentResult(payload)
}

func marshalCloudminiLiveCheckResult(ip string, live bool) (string, error) {
	status := "DIE (Proxy gián đoạn hoặc không kết nối được)"
	if live {
		status = "LIVE (Proxy đang kết nối hoạt động bình thường)"
	}
	payload, err := json.Marshal(map[string]any{
		"operation": "live_check",
		"ip":        ip,
		"success":   live,
		"live_check": map[string]any{
			"ip": ip, "live": live, "status": status,
		},
	})
	return string(payload), err
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
	return classifyCloudminiPlan(plan) == "residential_static"
}

func classifyCloudminiPlan(plan string) string {
	p := strings.ToLower(strings.Join(strings.Fields(plan), " "))
	switch {
	case strings.Contains(p, "budget residential static"):
		return "budget_residential_static"
	case strings.Contains(p, "residential static"):
		return "residential_static"
	case strings.Contains(p, "budgetv4"), strings.Contains(p, "budget v4"):
		return "budget_v4"
	case strings.Contains(p, "privatev4"), strings.Contains(p, "private v4"):
		return "private_v4"
	case strings.Contains(p, "privatev6"), strings.Contains(p, "private v6"):
		return "private_v6"
	case isVPSPlan(plan):
		return "vps"
	default:
		return "other"
	}
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
	if operation == "live_check" {
		return marshalCloudminiLiveCheckResult(ip, cloudminiLiveResponseIsLive(response.Error, response.Data, ip))
	}
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
			items[i].PlanFamily = classifyCloudminiPlan(items[i].Plan)
			rawUserEmail := strings.TrimSpace(items[i].UserEmail)
			items[i].UserEmail = "" // Redact system account email from LLM view

			if expectedEmail == "" {
				// Do not let the model answer a recovery/renewal request from an
				// unverified service snapshot. Ask for the customer's email first.
				items[i].Plan = ""
				items[i].PlanFamily = ""
				items[i].Expire = nil
				items[i].Region = ""
				items[i].ServiceStatus = "email_required"
				items[i].StatusNote = "Cần email tài khoản Cloudmini của khách để xác minh trước khi tư vấn hoặc xử lý IP. Không kết luận trạng thái, gói, hạn, quyền sở hữu, khôi phục hoặc gia hạn khi chưa có email."
				items[i].AccountEmailMatches = nil
				continue
			}

			classNote := getServiceClassificationNote(items[i].Plan)
			isResStatic := isResidentialStaticPlan(items[i].Plan)
			if items[i].ServiceStatus == "deleted" {
				// Deleted IPs are detached from every active service. The customer's
				// email is the destination account for a restore request, not an
				// ownership check against the former service record.
				items[i].AccountEmailMatches = nil
				if isResStatic {
					items[i].StatusNote = "IP đã bị xóa và không còn gắn với dịch vụ nào; khi khách yêu cầu khôi phục, KHÔNG đối chiếu email chủ sở hữu cũ." + classNote + " [QUY TẮC KHÔI PHỤC GÓI RESIDENTIAL STATIC]: BẮT BUỘC thông báo khách phí khôi phục IP cũ là 25.000đ/IP (nếu còn tài nguyên IP cũ) và YÊU CẦU KHÁCH NẠP ĐỦ SỐ DƯ TÀI KHOẢN CLOUDMINI = TỔNG (GIÁ CƯỚC PROXY + PHÍ KHÔI PHỤC 25.000đ/IP) để Admin tiến hành khôi phục thủ công. Email khách cung cấp là tài khoản nhận khôi phục."
				} else {
					items[i].StatusNote = "IP đã bị xóa và không còn gắn với dịch vụ nào; khi khách yêu cầu khôi phục, KHÔNG đối chiếu email chủ sở hữu cũ." + classNote + " Email khách cung cấp là tài khoản nhận khôi phục. Thông báo dịch vụ đã bị xóa/hết hạn và chuyển Admin kiểm tra khả năng khôi phục."
				}
				continue
			}

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
							items[i].CancellationPolicy = "admin_review"
							items[i].CancellationInstruction = "ƯU TIÊN RESELLER BẮT BUỘC: Không hướng dẫn khách tự hủy, đổi, gia hạn hoặc khôi phục theo chính sách gói thông thường. Phải gọi escalate_to_admin để Admin xử lý thủ công sau khi email đã xác minh khớp."
						} else if items[i].ServiceStatus == "expired" {
							items[i].StatusNote = "Dịch vụ đã hết hạn đúng tài khoản của khách hàng nhưng vẫn còn bản ghi trên hệ thống." + classNote + " Hướng dẫn khách kiểm tra khả năng tự gia hạn trong trang quản lý; không coi đây là lỗi kết nối và không gọi live_check."
						} else {
							items[i].StatusNote = "Dịch vụ của IP còn hiệu lực và đã xác minh đúng tài khoản khách hàng." + classNote
							items[i].CancellationPolicy, items[i].CancellationInstruction = cancellationPolicy(items[i].Plan)
						}
					} else {
						if items[i].ServiceStatus == "linked" || items[i].ServiceStatus == "active" || items[i].ServiceStatus == "expired" || items[i].ServiceStatus == "unknown" {
							items[i].ServiceStatus = "not_verified"
							items[i].StatusNote = "Không thể xác minh dịch vụ theo email đã cung cấp. Không suy đoán nguyên nhân hoặc quyền sở hữu. Với yêu cầu khôi phục hoặc gia hạn, chỉ thông báo ngắn gọn rằng hiện chưa thể hỗ trợ." + classNote
							items[i].Expire = nil // Redact expiry when email does not match
						}
					}
				}
			}
		}
		result["services"] = items
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

func cloudminiLiveResponseIsLive(upstreamError bool, data json.RawMessage, requestedIP string) bool {
	if upstreamError || len(data) == 0 {
		return false
	}
	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil || len(rawMap) == 0 {
		return false
	}
	if value, exists := rawMap["live"]; exists {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(typed, "true") || strings.EqualFold(typed, "live")
		default:
			return false
		}
	}
	if value, exists := rawMap["status"]; exists {
		status, ok := value.(string)
		return ok && (strings.EqualFold(status, "live") || strings.EqualFold(status, "ok") || strings.EqualFold(status, "active"))
	}

	// The production endpoint normally signals a successful live check with
	// error=false and GeoIP data instead of a `live` boolean. Require the IP in
	// that payload to match the requested IP so unrelated non-empty data cannot
	// be promoted to LIVE.
	responseIP, ok := rawMap["ip"].(string)
	if !ok {
		return false
	}
	responseAddr, responseErr := netip.ParseAddr(strings.TrimSpace(responseIP))
	requestedAddr, requestedErr := netip.ParseAddr(strings.TrimSpace(requestedIP))
	return responseErr == nil && requestedErr == nil && responseAddr == requestedAddr
}

type cloudminiServiceInfo struct {
	IP                      string  `json:"ip"`
	Plan                    string  `json:"plan"`
	PlanFamily              string  `json:"plan_family"`
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
	expiresAt, ok := parseCloudminiExpiry(*s.Expire)
	if !ok {
		s.ServiceStatus = "unknown"
		s.StatusNote = "Không đọc được định dạng ngày hết hạn; không tự kết luận trạng thái dịch vụ."
		return
	}
	if expiresAt.Before(time.Now()) {
		s.ServiceStatus = "expired"
		s.StatusNote = "Dịch vụ đã hết hạn nhưng vẫn còn bản ghi trên hệ thống."
		return
	}
	s.ServiceStatus = "active"
	s.StatusNote = "IP đang có thông tin dịch vụ."
}

func parseCloudminiExpiry(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)); err == nil {
		return parsed.Add(24*time.Hour - time.Nanosecond), true
	}
	return time.Time{}, false
}
