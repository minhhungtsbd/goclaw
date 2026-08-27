package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type testAdminHandoffStore struct {
	created     *store.AdminHandoff
	mergeResult *store.AdminHandoff
}

func (s *testAdminHandoffStore) Create(_ context.Context, handoff *store.AdminHandoff) error {
	if handoff.TicketNumber == 0 {
		handoff.TicketNumber = 123456
	}
	s.created = handoff
	return nil
}
func (s *testAdminHandoffStore) CreateOrMerge(_ context.Context, handoff *store.AdminHandoff) (*store.AdminHandoff, error) {
	if s.mergeResult != nil {
		return s.mergeResult, nil
	}
	if handoff.TicketNumber == 0 {
		handoff.TicketNumber = 123456
	}
	s.created = handoff
	return handoff, nil
}

func TestAdminHandoffToolPreservesCustomerRoutingMetadata(t *testing.T) {
	handoffStore := &testAdminHandoffStore{}
	tool := NewAdminHandoffTool(handoffStore)
	tool.SetChannelSender(func(context.Context, string, string, string) error { return nil })
	var gotMetadata map[string]string
	tool.SetChannelMetadataSender(func(_ context.Context, channel, chatID, content string, metadata map[string]string) error {
		if channel != "facebook" || chatID != "customer-1" || !strings.Contains(content, "Ticket-123456") {
			t.Fatalf("unexpected customer confirmation: %s/%s %q", channel, chatID, content)
		}
		gotMetadata = metadata
		return nil
	})
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
		AgentID:     uuid.New(),
		OtherConfig: json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram","chat_id":"-5570031702"}}`),
	})
	ctx = WithToolChannel(ctx, "facebook")
	ctx = WithToolChatID(ctx, "customer-1")
	ctx = store.WithRunContext(ctx, &store.RunContext{OutboundMetadata: map[string]string{"fb_mode": "messenger"}})

	result := tool.Execute(ctx, map[string]any{
		"summary":     "Kiểm tra Proxy 191.101.251.120",
		"service":     "Proxy PrivateV4",
		"identifiers": []any{"191.101.251.120", "customer@example.com"},
	})
	if result.IsError || !result.Silent {
		t.Fatalf("result = %#v", result)
	}
	if gotMetadata["fb_mode"] != "messenger" {
		t.Fatalf("routing metadata = %#v", gotMetadata)
	}
}

func TestAdminHandoffToolDoesNotResendMergedCase(t *testing.T) {
	existingID := uuid.New()
	handoffStore := &testAdminHandoffStore{mergeResult: &store.AdminHandoff{ID: existingID, TicketNumber: 123456}}
	tool := NewAdminHandoffTool(handoffStore)
	tool.SetChannelSender(func(context.Context, string, string, string) error {
		t.Fatal("merged case must not resend to Admin or customer")
		return nil
	})
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
		AgentID:     uuid.New(),
		OtherConfig: json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram","chat_id":"-5570031702"}}`),
	})
	ctx = WithToolChannel(ctx, "facebook")
	ctx = WithToolChatID(ctx, "customer-1")

	result := tool.Execute(ctx, map[string]any{
		"summary":     "Khách bổ sung Proxy 191.101.251.120",
		"service":     "Proxy PrivateV4",
		"identifiers": []any{"191.101.251.120", "customer@example.com"},
	})
	if result.IsError || !result.Silent || !strings.Contains(result.ForLLM, "merged") {
		t.Fatalf("result = %#v", result)
	}
}
func (*testAdminHandoffStore) Get(context.Context, uuid.UUID) (*store.AdminHandoff, error) {
	return nil, nil
}
func (*testAdminHandoffStore) ListPending(context.Context, uuid.UUID, string, string, int) ([]store.AdminHandoff, error) {
	return nil, nil
}
func (*testAdminHandoffStore) MarkCompleted(context.Context, uuid.UUID, string) (*store.AdminHandoff, error) {
	return nil, nil
}
func (*testAdminHandoffStore) MarkDismissed(context.Context, uuid.UUID) (*store.AdminHandoff, error) {
	return nil, nil
}
func (*testAdminHandoffStore) MarkDeliveryFailed(context.Context, uuid.UUID) error { return nil }

func TestParseAdminHandoffConfig(t *testing.T) {
	cfg, ok := ParseAdminHandoffConfig(json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":" telegram ","chat_id":" -5570031702 ","admin_user_ids":["1602998514"," 1602998514 ",""]}}`))
	if !ok {
		t.Fatal("ParseAdminHandoffConfig() ok = false")
	}
	if cfg.Channel != "telegram" || cfg.ChatID != "-5570031702" {
		t.Fatalf("config = %+v", cfg)
	}
	if len(cfg.AdminUserIDs) != 1 || cfg.AdminUserIDs[0] != "1602998514" {
		t.Fatalf("admin user IDs = %+v", cfg.AdminUserIDs)
	}
	if _, ok := ParseAdminHandoffConfig(json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram"}}`)); ok {
		t.Fatal("incomplete config must be rejected")
	}
}

func TestAdminHandoffDedupeKey(t *testing.T) {
	got := adminHandoffDedupeKey("facebook", "customer-1", "Need restore 191.101.251.120", []string{"order #1"})
	if got != "facebook\x1fcustomer-1\x1f191.101.251.120" {
		t.Fatalf("dedupe key = %q", got)
	}
	if got := adminHandoffDedupeKey("facebook", "customer-1", "Need a package upgrade", nil); got != "" {
		t.Fatalf("dedupe key without IP = %q, want empty", got)
	}
}

func TestAdminHandoffToolSendsOnlyConfiguredDestination(t *testing.T) {
	handoffStore := &testAdminHandoffStore{}
	tool := NewAdminHandoffTool(handoffStore)
	type sentMessage struct{ channel, chatID, content string }
	var sent []sentMessage
	tool.SetChannelSender(func(_ context.Context, gotChannel, gotChatID, gotContent string) error {
		sent = append(sent, sentMessage{gotChannel, gotChatID, gotContent})
		return nil
	})
	tool.SetChannelTenantChecker(func(name string) (uuid.UUID, bool) {
		return store.MasterTenantID, name == "telegram"
	})
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
		AgentID:     uuid.New(),
		OtherConfig: json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram","chat_id":"-5570031702"}}`),
	})
	ctx = WithToolChannel(ctx, "facebook")
	ctx = WithToolChatID(ctx, "customer-1")
	ctx = store.WithRunContext(ctx, &store.RunContext{OutboundMetadata: map[string]string{"fb_mode": "messenger"}})

	result := tool.Execute(ctx, map[string]any{
		"summary":     "Check order processing",
		"priority":    "high",
		"service":     "VPS-Custom Singapore",
		"identifiers": []any{"#449329", "103.183.121.6", "customer@example.com"},
	})
	if result.IsError {
		t.Fatalf("Execute() error = %s", result.ForLLM)
	}
	if len(sent) != 2 {
		t.Fatalf("sent messages = %d, want 2", len(sent))
	}
	if sent[0].channel != "telegram" || sent[0].chatID != "-5570031702" {
		t.Fatalf("admin handoff sent to %s/%s, want configured destination", sent[0].channel, sent[0].chatID)
	}
	if !strings.Contains(sent[0].content, "#449329") || !strings.Contains(sent[0].content, "Mã ticket: Ticket-123456") || !strings.Contains(sent[0].content, "Ưu tiên: Cao") || strings.Contains(sent[0].content, "facebook / customer-1") {
		t.Fatalf("handoff content missing expected context: %s", sent[0].content)
	}
	if sent[1].channel != "facebook" || sent[1].chatID != "customer-1" || !strings.Contains(sent[1].content, "Ticket-123456") {
		t.Fatalf("customer confirmation = %#v", sent[1])
	}
	if handoffStore.created == nil || handoffStore.created.SourceMetadata["fb_mode"] != "messenger" {
		t.Fatalf("source metadata = %#v", handoffStore.created)
	}
}

func TestValidateAdminHandoffDetails(t *testing.T) {
	tests := []struct {
		name        string
		service     string
		summary     string
		identifiers []string
		wantErr     bool
	}{
		{name: "non-service requires email", summary: "Kiểm tra nạp tiền", wantErr: true},
		{name: "non-service with email", summary: "Kiểm tra nạp tiền", identifiers: []string{"customer@example.com"}},
		{name: "proxy requires IP", service: "Proxy PrivateV4", summary: "Khách báo lỗi", identifiers: []string{"customer@example.com"}, wantErr: true},
		{name: "proxy requires email", service: "Proxy PrivateV4", summary: "Khách báo lỗi IP 191.101.251.120", identifiers: []string{"191.101.251.120"}, wantErr: true},
		{name: "proxy with required details", service: "Proxy PrivateV4", summary: "Khách báo lỗi", identifiers: []string{"191.101.251.120", "customer@example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAdminHandoffDetails(tt.service, tt.summary, tt.identifiers)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAdminHandoffDetails() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
