package pipeline

import (
	"context"
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
	state := NewRunState(&RunInput{Message: "Proxy 94.103.56.231 ở quốc gia nào?"}, nil, "", nil)
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
