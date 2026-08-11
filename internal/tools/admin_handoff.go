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
	return "Create and deliver an internal support handoff to the administrator group configured for this agent. Use only when Admin or technical action is required. The configured destination cannot be changed by this tool call. Report to the customer that the case was transferred only after this tool succeeds."
}

func (t *AdminHandoffTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type": "string", "description": "Concise description of the required manual action and observed issue.",
			},
			"priority": map[string]any{
				"type": "string", "enum": []string{"normal", "high", "urgent"}, "description": "Use urgent only for a customer-blocking or time-sensitive incident.",
			},
			"service": map[string]any{
				"type": "string", "description": "Service type and package when known, for example VPS-Custom Singapore.",
			},
			"identifiers": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"}, "description": "Relevant order IDs, IPs, or account email if needed for the handoff. Never include passwords, tokens, cookies, OTPs, or API keys.",
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
	if len([]rune(summary)) > 3000 {
		return ErrorResult("summary must be at most 3000 characters")
	}

	priority := argString(args, "priority")
	if priority == "" {
		priority = "normal"
	}
	if priority != "normal" && priority != "high" && priority != "urgent" {
		return ErrorResult("priority must be normal, high, or urgent")
	}

	identifiers := stringSlice(args["identifiers"])
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
	message := formatAdminHandoff(ctx, handoff.ID, priority, strings.TrimSpace(argString(args, "service")), summary, identifiers)
	if err := t.sender(ctx, cfg.Channel, cfg.ChatID, message); err != nil {
		// A failed update notification must not close the existing pending case.
		if handoff.ID == newCaseID {
			if markErr := t.store.MarkDeliveryFailed(ctx, handoff.ID); markErr != nil {
				return ErrorResult(fmt.Sprintf("admin handoff delivery failed: %v (also failed to mark case delivery failure: %v)", err, markErr))
			}
		}
		return ErrorResult(fmt.Sprintf("admin handoff delivery failed: %v", err))
	}
	return SilentResult(fmt.Sprintf(`{"status":"sent","destination":"admin_handoff","case_id":"CMH-%s"}`, strings.ToUpper(handoff.ID.String()[:8])))
}

var adminHandoffIPPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// adminHandoffDedupeKey groups pending manual work for the same customer IP.
// Non-IP requests intentionally remain separate because their relationship is ambiguous.
func adminHandoffDedupeKey(sourceChannel, sourceChatID, summary string, identifiers []string) string {
	for _, candidate := range append(append([]string{}, identifiers...), summary) {
		for _, rawIP := range adminHandoffIPPattern.FindAllString(candidate, -1) {
			ip, err := netip.ParseAddr(rawIP)
			if err == nil {
				return sourceChannel + "\x1f" + sourceChatID + "\x1f" + ip.String()
			}
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

func formatAdminHandoff(ctx context.Context, id uuid.UUID, priority, service, summary string, identifiers []string) string {
	var b strings.Builder
	b.WriteString("[CLOUDMINI ADMIN HANDOFF]\n")
	b.WriteString("Case: CMH-")
	b.WriteString(strings.ToUpper(id.String()[:8]))
	b.WriteString("\n")
	b.WriteString("Priority: ")
	b.WriteString(strings.ToUpper(priority))
	b.WriteString("\n")
	if service != "" {
		b.WriteString("Service: ")
		b.WriteString(service)
		b.WriteString("\n")
	}
	if len(identifiers) > 0 {
		b.WriteString("Identifiers: ")
		b.WriteString(strings.Join(identifiers, ", "))
		b.WriteString("\n")
	}
	b.WriteString("Source: ")
	b.WriteString(ToolChannelFromCtx(ctx))
	b.WriteString(" / ")
	b.WriteString(ToolChatIDFromCtx(ctx))
	b.WriteString("\n")
	b.WriteString("Time (UTC): ")
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString("\n\nRequest:\n")
	b.WriteString(summary)
	return b.String()
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
