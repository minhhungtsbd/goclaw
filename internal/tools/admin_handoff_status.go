package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var adminHandoffTicketIDPattern = regexp.MustCompile(`(?i)^Ticket-(\d{6,})$`)

// AdminHandoffStatusTool verifies a historical public ticket without exposing
// internal handoff content. Lookup is restricted to the current customer route.
type AdminHandoffStatusTool struct {
	store store.AdminHandoffStore
}

func NewAdminHandoffStatusTool(handoffStore store.AdminHandoffStore) *AdminHandoffStatusTool {
	return &AdminHandoffStatusTool{store: handoffStore}
}

func (t *AdminHandoffStatusTool) Name() string { return "admin_handoff_status" }

func (t *AdminHandoffStatusTool) Description() string {
	return "Kiểm tra trạng thái hiện tại của một mã Ticket Admin đã có trong đúng cuộc trò chuyện khách hàng này. Bắt buộc gọi trước khi nói ticket cũ còn chờ, đã hoàn tất, đã đóng hoặc trước khi quyết định tạo yêu cầu thay thế. Tool chỉ đọc trạng thái và không tạo ticket mới."
}

func (t *AdminHandoffStatusTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ticket_id": map[string]any{
				"type": "string", "description": "Mã ticket đầy đủ, ví dụ Ticket-000282.",
			},
		},
		"required": []string{"ticket_id"},
	}
}

type adminHandoffStatusResult struct {
	TicketID    string   `json:"ticket_id"`
	Status      string   `json:"status"`
	Service     string   `json:"service,omitempty"`
	RelatedIP   []string `json:"related_ips,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	ClosedAt    string   `json:"closed_at,omitempty"`
	Instruction string   `json:"instruction"`
}

func (t *AdminHandoffStatusTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.store == nil {
		return ErrorResult("ticket status is unavailable")
	}
	ticketID := strings.TrimSpace(argString(args, "ticket_id"))
	match := adminHandoffTicketIDPattern.FindStringSubmatch(ticketID)
	if len(match) != 2 {
		return ErrorResult("ticket_id must use the format Ticket-000000")
	}
	ticketNumber, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || ticketNumber <= 0 {
		return ErrorResult("ticket_id must use the format Ticket-000000")
	}

	snap, ok := store.AgentAudioFromCtx(ctx)
	channel := strings.TrimSpace(ToolChannelFromCtx(ctx))
	chatID := strings.TrimSpace(ToolChatIDFromCtx(ctx))
	if !ok || snap.AgentID == uuid.Nil || store.TenantIDFromContext(ctx) == uuid.Nil || channel == "" || chatID == "" {
		return ErrorResult("ticket status is unavailable: source route is missing")
	}

	handoff, err := t.store.GetByTicketNumberForSource(ctx, ticketNumber, snap.AgentID, channel, chatID)
	if err != nil || handoff == nil {
		// Deliberately do not reveal whether the ticket exists in another tenant,
		// agent, or customer conversation.
		payload, marshalErr := json.Marshal(adminHandoffStatusResult{
			TicketID:    ticketID,
			Status:      "unavailable",
			Instruction: "Không thể xác minh ticket trong cuộc trò chuyện này. Không đoán ticket có tồn tại ở khách, agent hoặc kênh khác và không mô tả trạng thái của ticket.",
		})
		if marshalErr != nil {
			return ErrorResult("ticket status is unavailable")
		}
		return SilentResult(string(payload))
	}

	result := adminHandoffStatusResult{
		TicketID:  handoff.Reference(),
		Status:    handoff.Status,
		Service:   strings.TrimSpace(handoff.Service),
		RelatedIP: adminHandoffRelatedIPs(handoff),
		CreatedAt: handoff.CreatedAt.UTC().Format(time.RFC3339),
	}
	if handoff.CompletedAt != nil {
		result.ClosedAt = handoff.CompletedAt.UTC().Format(time.RFC3339)
	}
	switch handoff.Status {
	case "pending":
		result.Instruction = "Ticket vẫn đang chờ Admin xử lý. Không tạo ticket trùng và không nói ticket đã hoàn tất hoặc đã đóng."
	case "completed":
		result.Instruction = "Ticket đã hoàn tất. Chỉ dùng kết quả đã được xác nhận trong lịch sử khách hàng; không tự suy diễn nội dung xử lý."
	case "dismissed":
		result.Instruction = "Ticket đã đóng và không còn chờ xử lý. Nếu vấn đề vẫn còn, hãy kiểm tra lại trạng thái dịch vụ hiện tại rồi chỉ tạo ticket mới khi điều kiện support yêu cầu."
	case "delivery_failed":
		result.Instruction = "Ticket không gửi được đến Admin. Không nói yêu cầu đang chờ xử lý; hãy kiểm tra lại trạng thái dịch vụ và tạo yêu cầu mới nếu vẫn cần Admin."
	case "unavailable":
		result.Instruction = "Không thể xác minh ticket trong cuộc trò chuyện này. Không đoán trạng thái hoặc dữ liệu ở cuộc trò chuyện khác."
	default:
		return ErrorResult("ticket has an unsupported status")
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return ErrorResult(fmt.Sprintf("ticket status encoding failed: %v", err))
	}
	return SilentResult(string(payload))
}

func adminHandoffRelatedIPs(handoff *store.AdminHandoff) []string {
	if handoff == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, value := range append(append([]string{}, handoff.Identifiers...), handoff.Summary) {
		for _, rawIP := range adminHandoffIPPattern.FindAllString(value, -1) {
			ip, err := netip.ParseAddr(rawIP)
			if err == nil {
				seen[ip.String()] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for ip := range seen {
		result = append(result, ip)
	}
	sort.Strings(result)
	return result
}
