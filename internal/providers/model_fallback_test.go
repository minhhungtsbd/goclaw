package providers

import (
	"context"
	"errors"
	"testing"
)

type testFallbackProvider struct {
	name      string
	model     string
	err       error
	streamErr error
	response  *ChatResponse
	caps      *ProviderCapabilities
	calls     int
}

func (p *testFallbackProvider) Chat(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if p.response != nil {
		return p.response, nil
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) ChatStream(_ context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	p.calls++
	if p.streamErr != nil {
		if req.Model == "primary-model" {
			onChunk(StreamChunk{Content: "partial"})
		}
		return nil, p.streamErr
	}
	if p.response != nil {
		return p.response, nil
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) DefaultModel() string { return p.model }
func (p *testFallbackProvider) Name() string         { return p.name }
func (p *testFallbackProvider) Capabilities() ProviderCapabilities {
	if p.caps != nil {
		return *p.caps
	}
	return ProviderCapabilities{Streaming: true, ToolCalling: true, StreamWithTools: true, Vision: true}
}

func TestModelFallbackProviderFallsBackOnClassifiedError(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup model", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackAfterStreamChunk(t *testing.T) {
	streamErr := &HTTPError{Status: 429, Body: "rate limited"}
	primary := &testFallbackProvider{
		name:      "primary",
		model:     "primary-model",
		streamErr: streamErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	var chunks int
	_, err := provider.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) {
		chunks++
	})
	if err == nil {
		t.Fatal("ChatStream() error = nil, want primary stream error")
	}
	if chunks != 1 {
		t.Fatalf("chunks = %d, want 1", chunks)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 after partial stream", backup.calls)
	}
}

func TestModelFallbackProviderChatStreamWithHookReportsStreamedChunks(t *testing.T) {
	streamErr := &HTTPError{Status: 429, Body: "rate limited"}
	primary := &testFallbackProvider{
		name:      "primary",
		model:     "primary-model",
		streamErr: streamErr,
	}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, nil, 1, false)

	var streamed bool
	_, err := provider.ChatStreamWithHook(context.Background(), ChatRequest{}, func(StreamChunk) {}, func(context.Context, FallbackCandidate, ChatRequest) (FallbackAfterCall, error) {
		return func(_ *ChatResponse, _ error, info FallbackCallInfo) {
			streamed = info.Streamed
		}, nil
	})
	if err == nil {
		t.Fatal("ChatStreamWithHook() error = nil, want stream error")
	}
	if !streamed {
		t.Fatal("FallbackCallInfo.Streamed = false, want true after partial stream")
	}
}

func TestModelFallbackProviderFallsBackToSameModelOnDifferentProvider(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "shared-model",
		err:   &HTTPError{Status: 404, Body: "model not found"},
	}
	backup := &testFallbackProvider{name: "backup", model: "shared-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "shared-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "shared-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "shared-model" {
		t.Fatalf("Chat() content = %q, want shared model from backup", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackOnUnknownError(t *testing.T) {
	unknownErr := errors.New("request serialization failed")
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   unknownErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if !errors.Is(err, unknownErr) {
		t.Fatalf("Chat() error = %v, want original unknown error", err)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 for unknown error", backup.calls)
	}
}

func TestModelFallbackProviderContinuesAfterContentPolicyFallback(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	blocked := &testFallbackProvider{
		name:  "blocked",
		model: "blocked-model",
		err:   &HTTPError{Status: 400, Body: `{"error":{"code":"data_inspection_failed","message":"Input text data may contain inappropriate content."}}`},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "blocked", Provider: blocked, Model: "blocked-model"},
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup model", resp.Content)
	}
	if primary.calls != 1 || blocked.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d blocked=%d backup=%d, want 1/1/1", primary.calls, blocked.calls, backup.calls)
	}
}

func TestModelFallbackProviderFallsBackOnCodexSafetyRefusalString(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "codex-digitop",
		model: "gpt-5.5",
		err:   errors.New("codex: response failed: Invalid prompt: we've limited access to this content for safety reasons"),
	}
	backup := &testFallbackProvider{name: "anthropic", model: "claude-sonnet-4-5"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "codex-digitop",
		Provider:     primary,
		Model:        "gpt-5.5",
	}, []FallbackCandidate{
		{ProviderName: "anthropic", Provider: backup, Model: "claude-sonnet-4-5"},
	}, 2, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "claude-sonnet-4-5" {
		t.Fatalf("Chat() content = %q, want fallback model response", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderMaxAttemptsCapsTotalAttempts(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 1, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("Chat() error = nil, want exhausted after primary only")
	}
	if primary.calls != 1 || backup.calls != 0 {
		t.Fatalf("calls primary=%d backup=%d, want 1/0", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderSkipsCandidateWithoutRequiredCapability(t *testing.T) {
	primaryCaps := ProviderCapabilities{Streaming: true, ToolCalling: false}
	primary := &testFallbackProvider{name: "primary", model: "primary-model", caps: &primaryCaps}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary", Provider: primary, Model: "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 1, true)

	resp, err := provider.Chat(context.Background(), ChatRequest{
		Tools: []ToolDefinition{{Type: "function", Function: &ToolFunctionSchema{Name: "lookup"}}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup response", resp.Content)
	}
	if primary.calls != 0 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 0/1", primary.calls, backup.calls)
	}

	resp, err = provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("text Chat() error = %v", err)
	}
	if resp.Content != "primary-model" || primary.calls != 1 {
		t.Fatalf("text Chat() content=%q primary calls=%d, want primary route", resp.Content, primary.calls)
	}
}

func TestModelFallbackProviderRejectsMissingRequiredToolCall(t *testing.T) {
	primary := &testFallbackProvider{
		name: "primary", model: "primary-model",
		response: &ChatResponse{Content: "I handled it", FinishReason: "stop"},
	}
	backup := &testFallbackProvider{
		name: "backup", model: "backup-model",
		response: &ChatResponse{
			FinishReason: "tool_calls",
			ToolCalls:    []ToolCall{{ID: "call_1", Name: "lookup", Arguments: map[string]any{"ip": "1.2.3.4"}}},
		},
	}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary", Provider: primary, Model: "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{
		Tools:   []ToolDefinition{{Type: "function", Function: &ToolFunctionSchema{Name: "lookup"}}},
		Options: map[string]any{OptToolChoice: "required"},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("Chat() tool calls = %#v, want lookup", resp.ToolCalls)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderRejectsUnknownToolAndEmptyResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *ChatResponse
	}{
		{name: "unknown tool", response: &ChatResponse{ToolCalls: []ToolCall{{ID: "call_1", Name: "invented"}}}},
		{name: "empty", response: &ChatResponse{}},
		{name: "orchestration placeholder", response: &ChatResponse{Content: "Got it, I'll incorporate that into what I'm working on."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := &testFallbackProvider{name: "primary", model: "primary-model", response: tt.response}
			provider := NewModelFallbackProvider(FallbackCandidate{
				ProviderName: "primary", Provider: primary, Model: "primary-model",
			}, nil, 0, false)
			_, err := provider.Chat(context.Background(), ChatRequest{
				Tools: []ToolDefinition{{Type: "function", Function: &ToolFunctionSchema{Name: "lookup"}}},
			})
			if err == nil {
				t.Fatal("Chat() error = nil, want invalid response error")
			}
			var summary *FailoverSummaryError
			if !errors.As(err, &summary) {
				t.Fatalf("Chat() error = %T, want FailoverSummaryError", err)
			}
			if len(summary.Attempts) != 1 || summary.Attempts[0].Classification.Reason != FailoverInvalidOutput {
				t.Fatalf("attempts = %#v, want invalid_response", summary.Attempts)
			}
		})
	}
}
