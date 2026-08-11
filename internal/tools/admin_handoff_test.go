package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestParseAdminHandoffConfig(t *testing.T) {
	cfg, ok := ParseAdminHandoffConfig(json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":" telegram ","chat_id":" -5570031702 "}}`))
	if !ok {
		t.Fatal("ParseAdminHandoffConfig() ok = false")
	}
	if cfg.Channel != "telegram" || cfg.ChatID != "-5570031702" {
		t.Fatalf("config = %+v", cfg)
	}
	if _, ok := ParseAdminHandoffConfig(json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram"}}`)); ok {
		t.Fatal("incomplete config must be rejected")
	}
}

func TestAdminHandoffToolSendsOnlyConfiguredDestination(t *testing.T) {
	tool := NewAdminHandoffTool()
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
		AgentID: uuid.New(),
		OtherConfig: json.RawMessage(`{"admin_handoff":{"enabled":true,"channel":"telegram","chat_id":"-5570031702"}}`),
	})
	ctx = WithToolChannel(ctx, "facebook")
	ctx = WithToolChatID(ctx, "customer-1")

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
	if !strings.Contains(content, "#449329") || !strings.Contains(content, "facebook / customer-1") {
		t.Fatalf("handoff content missing expected context: %s", content)
	}
}
