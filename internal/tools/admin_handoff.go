package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AdminHandoffConfig is stored in agents.other_config.admin_handoff. Keeping
// the destination in agent configuration prevents the model from choosing an
// arbitrary channel or chat ID.
type AdminHandoffConfig struct {
	Enabled      bool     `json:"enabled"`
	Channel      string   `json:"channel"`
	ChatID       string   `json:"chat_id"`
	AdminUserIDs []string `json:"admin_user_ids"`
}

// ParseAdminHandoffConfig reads the optional per-agent handoff destination.
func ParseAdminHandoffConfig(raw json.RawMessage) (AdminHandoffConfig, bool) {
	var bag struct {
		AdminHandoff AdminHandoffConfig `json:"admin_handoff"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &bag) != nil {
		return AdminHandoffConfig{}, false
	}
	bag.AdminHandoff.Channel = strings.TrimSpace(bag.AdminHandoff.Channel)
	bag.AdminHandoff.ChatID = strings.TrimSpace(bag.AdminHandoff.ChatID)
	bag.AdminHandoff.AdminUserIDs = normalizedAdminUserIDs(bag.AdminHandoff.AdminUserIDs)
	if !bag.AdminHandoff.Enabled || bag.AdminHandoff.Channel == "" || bag.AdminHandoff.ChatID == "" {
		return AdminHandoffConfig{}, false
	}
	return bag.AdminHandoff, true
}

// AdminHandoffTool sends an auditable, bounded support case to the configured
// internal admin group. It intentionally has no channel or target arguments.
type AdminHandoffTool struct {
	sender        ChannelSender
	tenantChecker ChannelTenantChecker
	store         store.AdminHandoffStore
}

func NewAdminHandoffTool(handoffStore store.AdminHandoffStore) *AdminHandoffTool {
	return &AdminHandoffTool{store: handoffStore}
}

func (t *AdminHandoffTool) SetChannelSender(sender ChannelSender) { t.sender = sender }
func (t *AdminHandoffTool) SetChannelTenantChecker(checker ChannelTenantChecker) {
	t.tenantChecker = checker
}

func (t *AdminHandoffTool) Name() string { return "escalate_to_admin" }

func (t *AdminHandoffTool) Description() string {
	return "Tạo và gửi yêu cầu xử lý nội bộ đến nhóm Admin đã cấu hình cho agent. Chỉ dùng khi cần Admin hoặc Kỹ thuật thao tác thủ công. Luôn có email tài khoản trong identifiers; nếu liên quan Proxy hoặc VPS thì phải có IP và email, riêng Residential VN dùng hostname *.resvn.net thay cho IP. Summary và identifiers phải viết ngắn gọn bằng tiếng Việt có dấu; không ghi mật khẩu, token, cookie, OTP, API key hoặc chuỗi host:port:user:pass. Tool chỉ gửi handoff đến nhóm Admin; sau khi tool thành công, hãy gộp mã Ticket và xác nhận ngắn gọn vào một response duy nhất gửi khách, không gọi tool hoặc gửi xác nhận riêng lần nữa."
}

func (t *AdminHandoffTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type": "string", "description": "Mô tả bằng tiếng Việt có dấu, ngắn gọn (2-4 câu) về việc cần xử lý và hiện trạng. Không viết bằng tiếng Anh.",
			},
			"priority": map[string]any{
				"type": "string", "enum": []string{"normal", "high", "urgent"}, "description": "Chỉ dùng urgent khi sự cố chặn hoàn toàn khách hàng hoặc cần xử lý gấp.",
			},
			"service": map[string]any{
				"type": "string", "description": "Loại dịch vụ và gói nếu đã xác định, viết bằng tiếng Việt có dấu.",
			},
			"identifiers": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"}, "description": "Luôn gồm email tài khoản. Nếu liên quan Proxy/VPS phải gồm IP và email; riêng Residential VN dùng hostname *.resvn.net thay cho IP. Có thể thêm mã đơn khi cần. Không ghi mật khẩu, token, cookie, OTP, API key hoặc chuỗi host:port:user:pass.",
			},
		},
		"required": []string{"summary"},
	}
}

func (t *AdminHandoffTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sender == nil {
		return ErrorResult("admin handoff is unavailable: channel sender is not configured")
	}
	if t.store == nil {
		return ErrorResult("admin handoff is unavailable: case store is not configured")
	}

	snap, ok := store.AgentAudioFromCtx(ctx)
	if !ok {
		return ErrorResult("admin handoff is unavailable: agent configuration is missing")
	}
	cfg, ok := ParseAdminHandoffConfig(snap.OtherConfig)
	if !ok {
		return ErrorResult("admin handoff is not configured for this agent")
	}
	if t.tenantChecker != nil {
		channelTenant, exists := t.tenantChecker(cfg.Channel)
		if !exists {
			return ErrorResult("configured admin handoff channel was not found")
		}
		ctxTenant := store.TenantIDFromContext(ctx)
		if channelTenant != uuid.Nil && ctxTenant != uuid.Nil && channelTenant != ctxTenant {
			return ErrorResult("configured admin handoff channel is not accessible from this tenant")
		}
	}

	summary := strings.TrimSpace(argString(args, "summary"))
	if summary == "" {
		return ErrorResult("summary is required")
	}
	if len([]rune(summary)) > 1200 {
		return ErrorResult("summary must be at most 1200 characters")
	}

	priority := argString(args, "priority")
	if priority == "" {
		priority = "normal"
	}
	if priority != "normal" && priority != "high" && priority != "urgent" {
		return ErrorResult("priority must be normal, high, or urgent")
	}

	identifiers := stringSlice(args["identifiers"])
	service := strings.TrimSpace(argString(args, "service"))
	if err := validateAdminHandoffDetails(service, summary, identifiers); err != nil {
		return ErrorResult(err.Error())
	}
	handoff := &store.AdminHandoff{
		ID:             uuid.New(),
		TenantID:       store.TenantIDFromContext(ctx),
		AgentID:        snap.AgentID,
		AdminChannel:   cfg.Channel,
		AdminChatID:    cfg.ChatID,
		SourceChannel:  ToolChannelFromCtx(ctx),
		SourceChatID:   ToolChatIDFromCtx(ctx),
		SourceMetadata: adminHandoffSourceMetadata(ctx),
		DedupeKey:      adminHandoffDedupeKey(ToolChannelFromCtx(ctx), ToolChatIDFromCtx(ctx), summary, identifiers),
		Priority:       priority,
		Service:        service,
		Identifiers:    identifiers,
		Summary:        summary,
		Status:         "pending",
		CreatedAt:      time.Now().UTC(),
	}
	if handoff.TenantID == uuid.Nil || handoff.SourceChannel == "" || handoff.SourceChatID == "" {
		return ErrorResult("admin handoff is unavailable: source route is missing")
	}
	newCaseID := handoff.ID
	stored, err := t.store.CreateOrMerge(ctx, handoff)
	if err != nil {
		return ErrorResult(fmt.Sprintf("admin handoff case creation failed: %v", err))
	}
	handoff = stored
	if handoff.ID != newCaseID {
		return SilentResult(fmt.Sprintf(`{"status":"merged","destination":"admin_handoff","ticket_id":%q,"instruction":"This information was merged into an existing pending ticket. Do not send another acknowledgement or call escalate_to_admin again."}`, handoff.Reference()))
	}

	message := formatAdminHandoff(handoff)
	if err := t.sender(ctx, cfg.Channel, cfg.ChatID, message); err != nil {
		// A failed update notification must not close the existing pending case.
		if handoff.ID == newCaseID {
			if markErr := t.store.MarkDeliveryFailed(ctx, handoff.ID); markErr != nil {
				return ErrorResult(fmt.Sprintf("admin handoff delivery failed: %v (also failed to mark case delivery failure: %v)", err, markErr))
			}
		}
		return ErrorResult(fmt.Sprintf("admin handoff delivery failed: %v", err))
	}
	return SilentResult(fmt.Sprintf(`{"status":"sent","destination":"admin_handoff","ticket_id":%q,"customer_notification":"deferred","instruction":"Handoff đã gửi thành công. Không gửi xác nhận riêng trong tool. Response cuối phải gộp một tin nhắn tiếng Việt ngắn gọn cho khách và bao gồm mã Ticket này."}`, handoff.Reference()))
}

var (
	adminHandoffIPPattern       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b|(?i)\b[0-9a-f]{0,4}:[0-9a-f:]+\b`)
	adminHandoffEmailPattern    = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	adminHandoffHostnamePattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\b`)
	adminHandoffSecretPattern   = regexp.MustCompile(`(?i)\b(?:password|pass(?:word)?|pwd|otp|token|cookie|api[ _-]?key|private[ _-]?key)\b|\b(?:(?:\d{1,3}\.){3}\d{1,3}|(?:[a-z0-9-]+\.)+[a-z]{2,}):\d{1,5}:[^:\s]+:[^:\s]+`)
)

func validateAdminHandoffDetails(service, summary string, identifiers []string) error {
	allDetails := append(append([]string{}, identifiers...), summary)
	for _, detail := range allDetails {
		if adminHandoffSecretPattern.MatchString(detail) {
			return fmt.Errorf("Admin handoff must not contain passwords, OTPs, tokens, cookies, API keys, or Proxy user/password strings; include only the IP/Residential VN hostname and Cloudmini account email")
		}
	}
	if !containsAdminHandoffEmail(allDetails) {
		return fmt.Errorf("email tài khoản là bắt buộc khi tạo Admin handoff; hãy xin hoặc dùng email đã có trong cuộc trò chuyện")
	}
	if isProxyOrVPSHandoff(service, summary) && !containsAdminHandoffServiceIdentifier(allDetails) {
		return fmt.Errorf("case Proxy hoặc VPS phải có IP và email tài khoản trước khi chuyển Admin; riêng Residential VN được dùng hostname *.resvn.net thay IP")
	}
	return nil
}

func isProxyOrVPSHandoff(service, summary string) bool {
	text := strings.ToLower(service + "\n" + summary)
	return strings.Contains(text, "proxy") || strings.Contains(text, "vps")
}

func containsAdminHandoffEmail(values []string) bool {
	for _, value := range values {
		if adminHandoffEmailPattern.FindString(value) != "" {
			return true
		}
	}
	return false
}

func containsAdminHandoffIP(values []string) bool {
	for _, value := range values {
		for _, rawIP := range adminHandoffIPPattern.FindAllString(value, -1) {
			if _, err := netip.ParseAddr(rawIP); err == nil {
				return true
			}
		}
	}
	return false
}

func containsAdminHandoffServiceIdentifier(values []string) bool {
	if containsAdminHandoffIP(values) {
		return true
	}
	for _, value := range values {
		if adminHandoffResidentialVNHost(value) != "" {
			return true
		}
	}
	return false
}

// adminHandoffDedupeKey groups pending manual work for the same customer IP.
// Non-IP requests intentionally remain separate because their relationship is ambiguous.
func adminHandoffDedupeKey(sourceChannel, sourceChatID, summary string, identifiers []string) string {
	values := append(append([]string{}, identifiers...), summary)
	for _, candidate := range values {
		for _, rawIP := range adminHandoffIPPattern.FindAllString(candidate, -1) {
			ip, err := netip.ParseAddr(rawIP)
			if err == nil {
				return sourceChannel + "\x1f" + sourceChatID + "\x1f" + ip.String()
			}
		}
	}
	for _, candidate := range values {
		if host := adminHandoffResidentialVNHost(candidate); host != "" {
			return sourceChannel + "\x1f" + sourceChatID + "\x1f" + host
		}
	}
	return ""
}

func adminHandoffResidentialVNHost(value string) string {
	for _, candidate := range adminHandoffHostnamePattern.FindAllString(value, -1) {
		host := strings.ToLower(candidate)
		if host == "resvn.net" || strings.HasSuffix(host, ".resvn.net") {
			return host
		}
	}
	return ""
}

func normalizedAdminUserIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func adminHandoffSourceMetadata(ctx context.Context) map[string]string {
	run := store.RunContextFromCtx(ctx)
	if run == nil || len(run.OutboundMetadata) == 0 {
		return map[string]string{}
	}
	metadata := make(map[string]string, len(run.OutboundMetadata))
	for key, value := range run.OutboundMetadata {
		metadata[key] = value
	}
	return metadata
}

func formatAdminHandoff(handoff *store.AdminHandoff) string {
	var b strings.Builder
	b.WriteString("[CLOUDMINI ADMIN HANDOFF]\n")
	b.WriteString("Mã ticket: ")
	b.WriteString(handoff.Reference())
	b.WriteString("\n")
	b.WriteString("Ưu tiên: ")
	b.WriteString(adminHandoffPriorityLabel(handoff.Priority))
	b.WriteString("\n")
	if handoff.Service != "" {
		b.WriteString("Dịch vụ: ")
		b.WriteString(handoff.Service)
		b.WriteString("\n")
	}
	if len(handoff.Identifiers) > 0 {
		b.WriteString("Thông tin: ")
		b.WriteString(strings.Join(handoff.Identifiers, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nYêu cầu xử lý:\n")
	b.WriteString(handoff.Summary)
	return b.String()
}

func adminHandoffPriorityLabel(priority string) string {
	switch priority {
	case "urgent":
		return "Khẩn"
	case "high":
		return "Cao"
	default:
		return "Bình thường"
	}
}

func stringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		value = strings.TrimSpace(value)
		if ok && value != "" {
			result = append(result, value)
		}
	}
	return result
}
