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
	adminHandoffDefaultInfo = "Da, bo phan ky thuat can anh cung cap them thong tin de tiep tuc kiem tra. Anh gui giup em ma don hang hoac anh loi hien tai nhe a."
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
		send("Ban khong duoc phep quan ly Admin handoff trong nhom nay.")
		return
	}

	handoffs, err := c.adminHandoffStore.ListPending(ctx, c.TenantID(), c.Name(), chatIDStr, 20)
	if err != nil {
		slog.Warn("telegram admin handoff list failed", "error", err)
		send("Khong the tai danh sach handoff. Vui long thu lai.")
		return
	}
	if len(handoffs) == 0 {
		send("Khong co Admin handoff dang cho xu ly.")
		return
	}

	var text strings.Builder
	text.WriteString("ADMIN HANDOFF DANG CHO XU LY\n\n")
	rows := make([][]telego.InlineKeyboardButton, 0, len(handoffs)*2)
	for _, handoff := range handoffs {
		text.WriteString(fmt.Sprintf("%s | %s\n%s\n\n", handoffReference(handoff.ID), handoff.SourceChannel+"/"+handoff.SourceChatID, truncateStr(handoff.Summary, 180)))
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: handoffReference(handoff.ID) + " Hoan tat", CallbackData: "ah:done:" + handoff.ID.String()},
			{Text: "Can bo sung", CallbackData: "ah:info:" + handoff.ID.String()},
		})
	}
	text.WriteString("Dung nut de gui mau. Hoac dung /handoff_done <case> <noi dung gui khach>.")
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
		send("Ban khong duoc phep quan ly Admin handoff trong nhom nay.")
		return
	}
	caseID, customerMessage, ok := parseAdminHandoffCommand(text)
	if !ok {
		send("Cu phap: /handoff_done <CMH-XXXXXXXX>. Co the them noi dung gui khach neu can.")
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
		send("Khong the hoan tat case. Co the case da duoc xu ly truoc do.")
		return
	}
	if customerMessage != "" {
		c.sendAdminHandoffCustomerMessage(completed, customerMessage)
		send(fmt.Sprintf("%s da hoan tat va da gui thong bao ve %s/%s.", handoffReference(completed.ID), completed.SourceChannel, completed.SourceChatID))
		return
	}
	if err := c.queueAdminHandoffCompletion(ctx, completed); err != nil {
		slog.Error("telegram admin handoff agent completion queue failed", "error", err, "case", completed.ID)
		send("Case da duoc danh dau hoan tat nhung khong the dua vao hang doi phan hoi cua Linh Nhi. Vui long lien he ky thuat.")
		return
	}
	send(fmt.Sprintf("%s da hoan tat. Linh Nhi dang soan thong bao va gui ve dung cuoc tro chuyen cua khach.", handoffReference(completed.ID)))
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
		send("Ban khong duoc phep quan ly Admin handoff trong nhom nay.")
		return
	}
	caseID, customerMessage, ok := parseAdminHandoffCommand(text)
	if !ok {
		send("Cu phap: /handoff_need_info <CMH-XXXXXXXX> <noi dung gui khach>")
		return
	}
	handoff, err := c.findPendingAdminHandoff(ctx, chatIDStr, caseID)
	if err != nil {
		send(err.Error())
		return
	}
	c.sendAdminHandoffCustomerMessage(handoff, customerMessage)
	send(fmt.Sprintf("Da gui yeu cau bo sung thong tin cho %s. Case van o trang thai cho xu ly.", handoffReference(handoff.ID)))
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
		c.sendAdminCallbackReply(ctx, chatID, "Ban khong duoc phep quan ly Admin handoff trong nhom nay.")
		return
	}
	caseID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	handoff, err := c.findPendingAdminHandoff(ctx, chatIDStr, caseID.String())
	if err != nil {
		c.sendAdminCallbackReply(ctx, chatID, "Case khong tim thay hoac da duoc xu ly.")
		return
	}
	if parts[1] == "info" {
		c.sendAdminHandoffCustomerMessage(handoff, adminHandoffDefaultInfo)
		c.sendAdminCallbackReply(ctx, chatID, handoffReference(handoff.ID)+" da gui yeu cau bo sung thong tin. Case van dang cho xu ly.")
		return
	}
	completed, err := c.adminHandoffStore.MarkCompleted(ctx, handoff.ID, "[agent-generated]")
	if err != nil {
		c.sendAdminCallbackReply(ctx, chatID, "Khong the hoan tat case. Co the case da duoc xu ly truoc do.")
		return
	}
	if err := c.queueAdminHandoffCompletion(ctx, completed); err != nil {
		slog.Error("telegram admin handoff callback completion queue failed", "error", err, "case", completed.ID)
		c.sendAdminCallbackReply(ctx, chatID, "Case da duoc danh dau hoan tat nhung khong the dua vao hang doi phan hoi cua Linh Nhi.")
		return
	}
	c.sendAdminCallbackReply(ctx, chatID, handoffReference(completed.ID)+" da hoan tat. Linh Nhi dang soan thong bao cho khach.")
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
		return nil, fmt.Errorf("khong the tai danh sach handoff")
	}
	normalized := strings.ToLower(strings.TrimSpace(reference))
	normalized = strings.TrimPrefix(normalized, "cmh-")
	var match *store.AdminHandoff
	for i := range handoffs {
		id := handoffs[i].ID.String()
		if id == normalized || strings.HasPrefix(id, normalized) {
			if match != nil {
				return nil, fmt.Errorf("case %q khong duy nhat; dung day du ma case", reference)
			}
			match = &handoffs[i]
		}
	}
	if match == nil {
		return nil, fmt.Errorf("khong tim thay case %q dang cho xu ly", reference)
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
