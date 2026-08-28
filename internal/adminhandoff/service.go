package adminhandoff

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type Actor struct {
	Type string
	ID   string
}

type AgentResolver interface {
	GetByID(context.Context, uuid.UUID) (*store.AgentData, error)
}

type Service struct {
	store  store.AdminHandoffStore
	agents AgentResolver
	bus    *bus.MessageBus
}

func NewService(handoffs store.AdminHandoffStore, agents AgentResolver, msgBus *bus.MessageBus) *Service {
	return &Service{store: handoffs, agents: agents, bus: msgBus}
}

func (s *Service) Complete(ctx context.Context, id uuid.UUID, actor Actor) (*store.AdminHandoff, error) {
	handoff, err := s.store.MarkCompleted(ctx, id, "[agent-generated]")
	if err != nil {
		return nil, err
	}
	if err := s.queueCompletion(ctx, handoff); err != nil {
		_ = s.store.MarkDeliveryFailed(ctx, id)
		s.appendEvent(ctx, handoff.ID, "delivery_failed", actor, err.Error(), nil)
		return handoff, err
	}
	s.appendEvent(ctx, handoff.ID, "completed", actor, "", map[string]any{"response": "agent_generated"})
	return handoff, nil
}

func (s *Service) Manual(ctx context.Context, id uuid.UUID, draft string, closeAfter bool, actor Actor) (*store.AdminHandoff, error) {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		return nil, fmt.Errorf("manual content is required")
	}
	if len([]rune(draft)) > 4000 {
		return nil, fmt.Errorf("manual content exceeds 4000 characters")
	}
	handoff, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if handoff.Status != "pending" {
		return nil, fmt.Errorf("admin handoff is not pending")
	}
	if closeAfter {
		handoff, err = s.store.MarkCompleted(ctx, id, "[manual-agent-rewrite]")
		if err != nil {
			return nil, err
		}
	}
	if err := s.queueManual(ctx, handoff, draft); err != nil {
		if closeAfter {
			_ = s.store.MarkDeliveryFailed(ctx, id)
		}
		s.appendEvent(ctx, id, "delivery_failed", actor, err.Error(), map[string]any{"action": "manual"})
		return nil, err
	}
	action := "manual_sent"
	if closeAfter {
		action = "manual_sent_and_closed"
	}
	s.appendEvent(ctx, id, action, actor, draft, nil)
	return handoff, nil
}

func (s *Service) Dismiss(ctx context.Context, id uuid.UUID, actor Actor) (*store.AdminHandoff, error) {
	handoff, err := s.store.MarkDismissed(ctx, id)
	if err != nil {
		return nil, err
	}
	s.appendEvent(ctx, id, "dismissed", actor, "", nil)
	return handoff, nil
}

func (s *Service) queueCompletion(ctx context.Context, handoff *store.AdminHandoff) error {
	agentKey, err := s.agentKey(ctx, handoff.AgentID)
	if err != nil {
		return err
	}
	metadata := cloneMetadata(handoff.SourceMetadata)
	metadata["admin_handoff_case_id"] = handoff.Reference()
	metadata["admin_handoff_completed"] = "true"
	if !s.bus.TryPublishInbound(bus.InboundMessage{
		Channel: handoff.SourceChannel, ChatID: handoff.SourceChatID,
		Content:  fmt.Sprintf("[INTERNAL ADMIN HANDOFF COMPLETED]\nTicket: %s\nAdmin đã hoàn tất thao tác thủ công. Hãy soạn một thông báo tiếng Việt ngắn gọn, tự nhiên và gửi ngay cho khách. Không nhắc sự kiện nội bộ, Telegram, tool hay mã ticket. Không gọi escalate_to_admin lần nữa.\n\nYêu cầu ban đầu:\n%s", handoff.Reference(), handoff.Summary),
		SenderID: "system:admin_handoff", UserID: handoff.SourceChatID, PeerKind: "direct",
		TenantID: handoff.TenantID, AgentID: agentKey, Metadata: metadata,
	}) {
		return fmt.Errorf("agent inbound queue is full")
	}
	return nil
}

func (s *Service) queueManual(ctx context.Context, handoff *store.AdminHandoff, draft string) error {
	agentKey, err := s.agentKey(ctx, handoff.AgentID)
	if err != nil {
		return err
	}
	metadata := cloneMetadata(handoff.SourceMetadata)
	metadata["admin_handoff_ticket_id"] = handoff.Reference()
	metadata["admin_handoff_manual"] = "true"
	if !s.bus.TryPublishInbound(bus.InboundMessage{
		Channel: handoff.SourceChannel, ChatID: handoff.SourceChatID,
		Content:  fmt.Sprintf("[INTERNAL ADMIN HANDOFF MANUAL]\nTicket: %s\nAdmin cung cấp nội dung thô sau:\n%s\n\nHãy biên tập thành câu trả lời tiếng Việt ngắn gọn, tự nhiên và gửi ngay cho khách. Chỉ giữ các thông tin đã được xác nhận. Không nhắc Admin, Telegram, tool, chỉ dẫn nội bộ hay mã ticket. Không tự thêm ETA và không gọi escalate_to_admin.", handoff.Reference(), draft),
		SenderID: "system:admin_handoff", UserID: handoff.SourceChatID, PeerKind: "direct",
		TenantID: handoff.TenantID, AgentID: agentKey, Metadata: metadata,
	}) {
		return fmt.Errorf("agent inbound queue is full")
	}
	return nil
}

func (s *Service) agentKey(ctx context.Context, id uuid.UUID) (string, error) {
	if s.agents == nil || s.bus == nil {
		return "", fmt.Errorf("admin handoff runtime is unavailable")
	}
	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("load handoff agent: %w", err)
	}
	if agent.AgentKey == "" {
		return "", fmt.Errorf("handoff agent has no agent key")
	}
	return agent.AgentKey, nil
}

func (s *Service) appendEvent(ctx context.Context, handoffID uuid.UUID, action string, actor Actor, content string, metadata map[string]any) {
	managed, ok := s.store.(store.AdminHandoffManagementStore)
	if !ok {
		return
	}
	_ = managed.AppendEvent(ctx, &store.AdminHandoffEvent{
		HandoffID: handoffID, Action: action, ActorType: actor.Type,
		ActorID: actor.ID, Content: content, Metadata: metadata,
	})
}

func cloneMetadata(source map[string]string) map[string]string {
	result := make(map[string]string, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}
