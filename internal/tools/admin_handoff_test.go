package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type testAdminHandoffStore struct{ created *store.AdminHandoff }

func (s *testAdminHandoffStore) Create(_ context.Context, handoff *store.AdminHandoff) error {
	s.created = handoff
	return nil
}
func (s *testAdminHandoffStore) CreateOrMerge(_ context.Context, handoff *store.AdminHandoff) (*store.AdminHandoff, error) {
	s.created = handoff
	return handoff, nil
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
	var channel, chatID, content string
	tool.SetChannelSender(func(_ context.Context, gotChannel, gotChatID, gotContent string) error {
		channel, chatID, content = gotChannel, gotChatID, gotContent
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
		"identifiers": []any{"#449329", "#449328"},
	})
	if result.IsError {
		t.Fatalf("Execute() error = %s", result.ForLLM)
	}
	if channel != "telegram" || chatID != "-5570031702" {
		t.Fatalf("sent to %s/%s, want configured destination", channel, chatID)
	}
	if !strings.Contains(content, "#449329") || !strings.Contains(content, "facebook / customer-1") || !strings.Contains(content, "Case: CMH-") {
		t.Fatalf("handoff content missing expected context: %s", content)
	}
	if handoffStore.created == nil || handoffStore.created.SourceMetadata["fb_mode"] != "messenger" {
		t.Fatalf("source metadata = %#v", handoffStore.created)
	}
}
