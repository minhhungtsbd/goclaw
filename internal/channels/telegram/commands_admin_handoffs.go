package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	adminHandoffDefaultInfo = "Dạ, bộ phận kỹ thuật cần anh cung cấp thêm thông tin để tiếp tục kiểm tra. Anh gửi giúp em mã đơn hàng hoặc ảnh lỗi hiện tại nhé ạ."
)

func (c *Channel) handleAdminHandoffsList(ctx context.Context, chatID int64, chatIDStr, senderID string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	send := func(text string) {
		msg := tu.Message(tu.ID(chatID), text)
		setThread(msg)
		if _, err := c.bot.SendMessage(ctx, msg); err != nil {
			slog.Warn("telegram admin handoff list reply failed", "error", err)
		}
	}

	if !c.adminHandoffAuthorized(ctx, chatIDStr, senderID, isGroup) {
		send("Bạn không được phép quản lý Admin handoff trong nhóm này.")
		return
	}

	handoffs, err := c.adminHandoffStore.ListPending(ctx, c.TenantID(), c.Name(), chatIDStr, 20)
	if err != nil {
		slog.Warn("telegram admin handoff list failed", "error", err)
		send("Không thể tải danh sách handoff. Vui lòng thử lại.")
		return
	}
	if len(handoffs) == 0 {
		send("Không có Admin handoff đang chờ xử lý.")
		return
	}

	var text strings.Builder
	text.WriteString("ADMIN HANDOFF ĐANG CHỜ XỬ LÝ\n\n")
	rows := make([][]telego.InlineKeyboardButton, 0, len(handoffs)*2)
	for _, handoff := range handoffs {
		text.WriteString(fmt.Sprintf("%s | %s\n%s\n\n", handoffReference(handoff.ID), handoff.SourceChannel+"/"+handoff.SourceChatID, truncateStr(handoff.Summary, 180)))
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: handoffReference(handoff.ID) + " Hoàn tất", CallbackData: "ah:done:" + handoff.ID.String()},
			{Text: "Cần bổ sung", CallbackData: "ah:info:" + handoff.ID.String()},
		})
	}
	text.WriteString("Dùng nút để gửi mẫu. Hoặc dùng /handoff_done <case> <nội dung gửi khách>.")
	msg := tu.Message(tu.ID(chatID), text.String())
	setThread(msg)
	msg.ReplyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
	if _, err := c.bot.SendMessage(ctx, msg); err != nil {
		slog.Warn("telegram admin handoff list send failed", "error", err)
	}
}

func (c *Channel) handleAdminHandoffDone(ctx context.Context, chatID int64, chatIDStr, senderID, text string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	send := func(reply string) {
		msg := tu.Message(tu.ID(chatID), reply)
		setThread(msg)
		if _, err := c.bot.SendMessage(ctx, msg); err != nil {
			slog.Warn("telegram admin handoff command reply failed", "error", err)
		}
	}
	if !c.adminHandoffAuthorized(ctx, chatIDStr, senderID, isGroup) {
		send("Bạn không được phép quản lý Admin handoff trong nhóm này.")
		return
	}
	caseID, customerMessage, ok := parseAdminHandoffCommand(text)
	if !ok {
		send("Cú pháp: /handoff_done <CMH-XXXXXXXX>. Có thể thêm nội dung gửi khách nếu cần.")
		return
	}
	handoff, err := c.findPendingAdminHandoff(ctx, chatIDStr, caseID)
	if err != nil {
		send(err.Error())
		return
	}
	completionMessage := customerMessage
	if completionMessage == "" {
		completionMessage = "[agent-generated]"
	}
	completed, err := c.adminHandoffStore.MarkCompleted(ctx, handoff.ID, completionMessage)
	if err != nil {
		slog.Warn("telegram admin handoff completion failed", "error", err, "case", handoff.ID)
		send("Không thể hoàn tất case. Có thể case đã được xử lý trước đó.")
		return
	}
	if customerMessage != "" {
		c.sendAdminHandoffCustomerMessage(completed, customerMessage)
		send(fmt.Sprintf("%s đã hoàn tất và đã gửi thông báo về %s/%s.", handoffReference(completed.ID), completed.SourceChannel, completed.SourceChatID))
		return
	}
	if err := c.queueAdminHandoffCompletion(ctx, completed); err != nil {
		slog.Error("telegram admin handoff agent completion queue failed", "error", err, "case", completed.ID)
		send("Case đã được đánh dấu hoàn tất nhưng không thể đưa vào hàng đợi phản hồi của Linh Nhi. Vui lòng liên hệ kỹ thuật.")
		return
	}
	send(fmt.Sprintf("%s đã hoàn tất. Linh Nhi đang soạn thông báo và gửi về đúng cuộc trò chuyện của khách.", handoffReference(completed.ID)))
}

func (c *Channel) handleAdminHandoffNeedInfo(ctx context.Context, chatID int64, chatIDStr, senderID, text string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	send := func(reply string) {
		msg := tu.Message(tu.ID(chatID), reply)
		setThread(msg)
		if _, err := c.bot.SendMessage(ctx, msg); err != nil {
			slog.Warn("telegram admin handoff command reply failed", "error", err)
		}
	}
	if !c.adminHandoffAuthorized(ctx, chatIDStr, senderID, isGroup) {
		send("Bạn không được phép quản lý Admin handoff trong nhóm này.")
		return
	}
	caseID, customerMessage, ok := parseAdminHandoffCommand(text)
	if !ok {
		send("Cú pháp: /handoff_need_info <CMH-XXXXXXXX> <nội dung gửi khách>")
		return
	}
	handoff, err := c.findPendingAdminHandoff(ctx, chatIDStr, caseID)
	if err != nil {
		send(err.Error())
		return
	}
	c.sendAdminHandoffCustomerMessage(handoff, customerMessage)
	send(fmt.Sprintf("Đã gửi yêu cầu bổ sung thông tin cho %s. Case vẫn ở trạng thái chờ xử lý.", handoffReference(handoff.ID)))
}

func (c *Channel) handleAdminHandoffCallback(ctx context.Context, query *telego.CallbackQuery) {
	if query.Message == nil {
		return
	}
	parts := strings.Split(query.Data, ":")
	if len(parts) != 3 || (parts[1] != "done" && parts[1] != "info") {
		return
	}
	chatID := query.Message.GetChat().ID
	chatIDStr := fmt.Sprintf("%d", chatID)
	senderID := fmt.Sprintf("%d", query.From.ID)
	if !c.adminHandoffAuthorized(ctx, chatIDStr, senderID, true) {
		c.sendAdminCallbackReply(ctx, chatID, "Bạn không được phép quản lý Admin handoff trong nhóm này.")
		return
	}
	caseID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	handoff, err := c.findPendingAdminHandoff(ctx, chatIDStr, caseID.String())
	if err != nil {
		c.sendAdminCallbackReply(ctx, chatID, "Case không tìm thấy hoặc đã được xử lý.")
		return
	}
	if parts[1] == "info" {
		c.sendAdminHandoffCustomerMessage(handoff, adminHandoffDefaultInfo)
		c.sendAdminCallbackReply(ctx, chatID, handoffReference(handoff.ID)+" đã gửi yêu cầu bổ sung thông tin. Case vẫn đang chờ xử lý.")
		return
	}
	completed, err := c.adminHandoffStore.MarkCompleted(ctx, handoff.ID, "[agent-generated]")
	if err != nil {
		c.sendAdminCallbackReply(ctx, chatID, "Không thể hoàn tất case. Có thể case đã được xử lý trước đó.")
		return
	}
	if err := c.queueAdminHandoffCompletion(ctx, completed); err != nil {
		slog.Error("telegram admin handoff callback completion queue failed", "error", err, "case", completed.ID)
		c.sendAdminCallbackReply(ctx, chatID, "Case đã được đánh dấu hoàn tất nhưng không thể đưa vào hàng đợi phản hồi của Linh Nhi.")
		return
	}
	c.sendAdminCallbackReply(ctx, chatID, handoffReference(completed.ID)+" đã hoàn tất. Linh Nhi đang soạn thông báo cho khách.")
}

func (c *Channel) sendAdminCallbackReply(ctx context.Context, chatID int64, text string) {
	if _, err := c.bot.SendMessage(ctx, tu.Message(tu.ID(chatID), text)); err != nil {
		slog.Warn("telegram admin handoff callback reply failed", "error", err)
	}
}

func (c *Channel) sendAdminHandoffCustomerMessage(handoff *store.AdminHandoff, content string) {
	c.Bus().PublishOutbound(bus.OutboundMessage{
		Channel:  handoff.SourceChannel,
		ChatID:   handoff.SourceChatID,
		Content:  content,
		Metadata: cloneAdminHandoffMetadata(handoff.SourceMetadata),
		TenantID: handoff.TenantID,
		AgentID:  handoff.AgentID,
	})
}

func (c *Channel) queueAdminHandoffCompletion(ctx context.Context, handoff *store.AdminHandoff) error {
	if c.agentStore == nil {
		return fmt.Errorf("agent store is unavailable")
	}
	agent, err := c.agentStore.GetByID(ctx, handoff.AgentID)
	if err != nil {
		return fmt.Errorf("load handoff agent: %w", err)
	}
	if agent.AgentKey == "" {
		return fmt.Errorf("handoff agent has no agent key")
	}
	metadata := cloneAdminHandoffMetadata(handoff.SourceMetadata)
	metadata["admin_handoff_case_id"] = handoffReference(handoff.ID)
	metadata["admin_handoff_completed"] = "true"
	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  handoff.SourceChannel,
		ChatID:   handoff.SourceChatID,
		Content:  fmt.Sprintf("[INTERNAL ADMIN HANDOFF COMPLETED]\nCase: %s\nAdmin has completed the requested manual action. Send a concise, natural Vietnamese update to the customer now. Do not mention this internal event, Telegram, tools, or the case ID. Do not call escalate_to_admin again.\n\nOriginal request:\n%s", handoffReference(handoff.ID), handoff.Summary),
		SenderID: "system:admin_handoff",
		UserID:   handoff.SourceChatID,
		PeerKind: "direct",
		TenantID: handoff.TenantID,
		AgentID:  agent.AgentKey,
		Metadata: metadata,
	})
	return nil
}

func cloneAdminHandoffMetadata(source map[string]string) map[string]string {
	metadata := make(map[string]string, len(source)+2)
	for key, value := range source {
		metadata[key] = value
	}
	return metadata
}

func (c *Channel) adminHandoffAuthorized(ctx context.Context, chatID, senderID string, isGroup bool) bool {
	if !isGroup || c.adminHandoffStore == nil || c.agentStore == nil {
		return false
	}
	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		return false
	}
	agent, err := c.agentStore.GetByID(ctx, agentID)
	if err != nil {
		return false
	}
	config, ok := tools.ParseAdminHandoffConfig(agent.OtherConfig)
	if !ok || config.Channel != c.Name() || config.ChatID != chatID || len(config.AdminUserIDs) == 0 {
		return false
	}
	numericSenderID := strings.SplitN(senderID, "|", 2)[0]
	for _, allowedID := range config.AdminUserIDs {
		if allowedID == numericSenderID {
			return true
		}
	}
	return false
}

func (c *Channel) findPendingAdminHandoff(ctx context.Context, chatID, reference string) (*store.AdminHandoff, error) {
	handoffs, err := c.adminHandoffStore.ListPending(ctx, c.TenantID(), c.Name(), chatID, 100)
	if err != nil {
		return nil, fmt.Errorf("không thể tải danh sách handoff")
	}
	normalized := strings.ToLower(strings.TrimSpace(reference))
	normalized = strings.TrimPrefix(normalized, "cmh-")
	var match *store.AdminHandoff
	for i := range handoffs {
		id := handoffs[i].ID.String()
		if id == normalized || strings.HasPrefix(id, normalized) {
			if match != nil {
				return nil, fmt.Errorf("case %q không duy nhất; dùng đầy đủ mã case", reference)
			}
			match = &handoffs[i]
		}
	}
	if match == nil {
		return nil, fmt.Errorf("không tìm thấy case %q đang chờ xử lý", reference)
	}
	return match, nil
}

func parseAdminHandoffCommand(text string) (caseID, customerMessage string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	caseID = strings.TrimSpace(parts[1])
	if len(parts) == 3 {
		customerMessage = strings.TrimSpace(parts[2])
	}
	return caseID, customerMessage, caseID != ""
}

func handoffReference(id uuid.UUID) string {
	return "CMH-" + strings.ToUpper(id.String()[:8])
}
