package adminhandoff

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCompleteQueuesAgentReplyToOriginalConversation(t *testing.T) {
	handoff := testHandoff()
	handoffs := &fakeHandoffStore{handoff: handoff}
	msgBus := bus.New()
	service := NewService(handoffs, fakeAgentResolver{agent: &store.AgentData{AgentKey: "linh-nhi"}}, msgBus)

	completed, err := service.Complete(context.Background(), handoff.ID, Actor{Type: "web", ID: "admin-1"})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	message, ok := msgBus.ConsumeInbound(context.Background())
	if !ok {
		t.Fatal("expected queued inbound message")
	}
	if message.Channel != handoff.SourceChannel || message.ChatID != handoff.SourceChatID || message.AgentID != "linh-nhi" {
		t.Fatalf("unexpected route: channel=%q chat=%q agent=%q", message.Channel, message.ChatID, message.AgentID)
	}
	if !strings.Contains(message.Content, handoff.Reference()) || !strings.Contains(message.Content, handoff.Summary) {
		t.Fatalf("queued content does not include ticket and summary: %q", message.Content)
	}
}

func TestManualRejectsNonPendingHandoff(t *testing.T) {
	handoff := testHandoff()
	handoff.Status = "completed"
	service := NewService(&fakeHandoffStore{handoff: handoff}, fakeAgentResolver{}, bus.New())

	if _, err := service.Manual(context.Background(), handoff.ID, "đã xử lý", false, Actor{}); err == nil {
		t.Fatal("Manual() error = nil, want non-pending error")
	}
}

func testHandoff() *store.AdminHandoff {
	return &store.AdminHandoff{
		ID: uuid.Must(uuid.NewV7()), TicketNumber: 123, TenantID: uuid.Must(uuid.NewV7()),
		AgentID: uuid.Must(uuid.NewV7()), SourceChannel: "cloudmini-net-page", SourceChatID: "customer-1",
		SourceMetadata: map[string]string{"page_id": "page-1"}, Summary: "Khôi phục proxy 1.2.3.4", Status: "pending",
	}
}

type fakeAgentResolver struct {
	agent *store.AgentData
	err   error
}

func (f fakeAgentResolver) GetByID(context.Context, uuid.UUID) (*store.AgentData, error) {
	return f.agent, f.err
}

type fakeHandoffStore struct {
	handoff *store.AdminHandoff
}

func (f *fakeHandoffStore) Create(context.Context, *store.AdminHandoff) error { return nil }
func (f *fakeHandoffStore) CreateOrMerge(context.Context, *store.AdminHandoff) (*store.AdminHandoff, error) {
	return f.handoff, nil
}
func (f *fakeHandoffStore) Get(context.Context, uuid.UUID) (*store.AdminHandoff, error) {
	return f.handoff, nil
}
func (f *fakeHandoffStore) GetByTicketNumberForSource(context.Context, int64, uuid.UUID, string, string) (*store.AdminHandoff, error) {
	return f.handoff, nil
}
func (f *fakeHandoffStore) ListPending(context.Context, uuid.UUID, string, string, int) ([]store.AdminHandoff, error) {
	return nil, nil
}
func (f *fakeHandoffStore) MarkCompleted(_ context.Context, _ uuid.UUID, message string) (*store.AdminHandoff, error) {
	f.handoff.Status = "completed"
	f.handoff.CompletionMessage = message
	return f.handoff, nil
}
func (f *fakeHandoffStore) MarkDismissed(context.Context, uuid.UUID) (*store.AdminHandoff, error) {
	f.handoff.Status = "dismissed"
	return f.handoff, nil
}
func (f *fakeHandoffStore) MarkDeliveryFailed(context.Context, uuid.UUID) error {
	f.handoff.Status = "delivery_failed"
	return nil
}
