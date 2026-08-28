package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestCloudminiServicePreflightCallsServiceInfoBeforeLLM(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Kiểm tra Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"linked","plan":"PrivateV4"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 2 || calls[0].Name != cloudminiProxyCheckToolName || calls[0].Arguments["operation"] != "service_info" || calls[1].Arguments["operation"] != "live_check" {
		t.Fatalf("calls = %#v", calls)
	}
	if got := state.Messages.Pending(); len(got) != 4 || got[0].Role != "assistant" || got[1].Role != "tool" || got[2].Role != "assistant" || got[3].Role != "tool" {
		t.Fatalf("pending messages = %#v", got)
	}
}

func TestCloudminiServicePreflightPinsCurrentMultiIPScope(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 48.45.161.144 và 48.45.161.122 giúp em"}, nil, "", nil)
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "base"})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"deleted"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	content := state.Messages.System().Content
	if !strings.Contains(content, "48.45.161.144, 48.45.161.122") || !strings.Contains(content, "Không dùng") {
		t.Fatalf("current request scope missing: %s", content)
	}
}

func TestValidateCloudminiCurrentRequestToolCallRejectsOldIP(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 48.45.161.144 và 48.45.161.122 giúp em"}, nil, "", nil)
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, providers.ToolCall{
		Name: cloudminiProxyCheckToolName, Arguments: map[string]any{"ip": "46.34.39.86"},
	}); ok {
		t.Fatal("old IP check should be rejected")
	}
}

func TestValidateCloudminiCurrentRequestToolCallRequiresCompleteRecoveryHandoff(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 48.45.161.144 và 48.45.161.122 giúp em"}, nil, "", nil)
	partial := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{
		"summary": "Khôi phục proxy 48.45.161.144",
		"identifiers": []any{"customer@example.com", "48.45.161.144"},
	}}
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, partial); ok {
		t.Fatal("partial multi-IP recovery handoff should be rejected")
	}
	complete := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{
		"summary": "Khôi phục proxy 48.45.161.144 và 48.45.161.122",
		"identifiers": []any{"customer@example.com", "48.45.161.144", "48.45.161.122"},
	}}
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, complete); !ok {
		t.Fatalf("complete handoff rejected: %s", reason)
	}
}

func TestCloudminiServicePreflightSkipsLiveCheckForDeletedService(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Kiểm tra Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"deleted"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["operation"] != "service_info" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestCloudminiServicePreflightSkipsNonSupportMessage(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy ở quốc gia nào?"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			t.Fatal("tool must not run")
			return nil, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
}

func TestCloudminiServicePreflightChecksVPSWithoutLiveCheck(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Gia hạn VPS IP 203.0.113.10 giúp anh"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"linked","plan":"VPS-Custom"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["operation"] != "service_info" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestCloudminiServicePreflightRefreshesMissingToolSnapshot(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Gia hạn VPS IP 203.0.113.10"}, nil, "", nil)
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}, nil
		},
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"linked"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["operation"] != "service_info" {
		t.Fatalf("calls = %#v", calls)
	}
}
