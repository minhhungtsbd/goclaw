package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestToolStageCapturesCloudminiStateAfterLLMServiceInfo(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-state", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini của em là customer@example.com"}})
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID: "call-service-info", Name: cloudminiProxyCheckToolName,
		Arguments: map[string]any{"ip": "178.218.146.11", "operation": "service_info", "account_email": "customer@example.com"},
	}}}
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, call providers.ToolCall) ([]providers.Message, error) {
		return []providers.Message{{Role: "tool", ToolCallID: call.ID, Content: `{"operation":"service_info","services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":null,"service_status":"deleted"}]}`}}, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := strings.Join(state.Cloudmini.RequestIPs, ","); got != "178.218.146.11" {
		t.Fatalf("RequestIPs = %q", got)
	}
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].Status != "deleted" {
		t.Fatalf("ServiceFacts = %#v", state.Cloudmini.ServiceFacts)
	}
	if state.Cloudmini.EmailRequired || state.Cloudmini.EmailMismatch {
		t.Fatalf("Cloudmini guards = %#v", state.Cloudmini)
	}
}

func TestToolStageCapturesEmailGateAfterLLMServiceInfo(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-email-gate", Message: "Gia hạn 178.218.146.11 giúp em"}, nil, "", nil)
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID: "call-email-gate", Name: cloudminiProxyCheckToolName,
		Arguments: map[string]any{"ip": "178.218.146.11", "operation": "service_info"},
	}}}
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, call providers.ToolCall) ([]providers.Message, error) {
		return []providers.Message{{Role: "tool", ToolCallID: call.ID, Content: `{"operation":"service_info","services":[{"ip":"178.218.146.11","service_status":"email_required"}]}`}}, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !state.Cloudmini.EmailRequired {
		t.Fatalf("Cloudmini guards = %#v", state.Cloudmini)
	}
	if !strings.Contains(state.Messages.System().Content, "CLOUDMINI EMAIL GATE") {
		t.Fatal("email gate was not injected for the next LLM iteration")
	}
}

func TestToolStageCapturesConfiguredEmailAdminHandoff(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-admin-email", Message: "Kiểm tra 178.218.146.11 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini của em là priority@example.com"}})
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID: "call-admin-email", Name: cloudminiProxyCheckToolName,
		Arguments: map[string]any{"ip": "178.218.146.11", "operation": "service_info", "account_email": "priority@example.com"},
	}}}
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, call providers.ToolCall) ([]providers.Message, error) {
		return []providers.Message{{Role: "tool", ToolCallID: call.ID, Content: `{"operation":"service_info","services":[{"ip":"178.218.146.11","plan":"PrivateV4","plan_family":"private_v4","service_status":"active","account_email_matches":true,"admin_handoff_required":true}]}`}}, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !state.Cloudmini.AdminHandoffRequired || len(state.Cloudmini.ServiceFacts) != 1 ||
		!state.Cloudmini.ServiceFacts[0].AdminHandoffRequired {
		t.Fatalf("configured email handoff state = %#v", state.Cloudmini)
	}
	if !strings.Contains(state.Messages.System().Content, "CLOUDMINI EMAIL NGOẠI LỆ") {
		t.Fatal("configured email handoff instruction was not injected")
	}
}

func TestToolStageRejectsLiveCheckWithoutVerifiedServiceInfo(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-live-guard", Message: "Proxy 178.218.146.11 bị lỗi"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini của em là customer@example.com"}})
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID: "call-live-too-early", Name: cloudminiProxyCheckToolName,
		Arguments: map[string]any{"ip": "178.218.146.11", "operation": "live_check", "account_email": "customer@example.com"},
	}}}
	executed := false
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(context.Context, *RunState, providers.ToolCall) ([]providers.Message, error) {
		executed = true
		return nil, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if executed {
		t.Fatal("live_check executed before verified service_info")
	}
	pending := state.Messages.Pending()
	if len(pending) != 1 || !pending[0].IsError || !strings.Contains(pending[0].Content, "service_info") {
		t.Fatalf("blocked result = %#v", pending)
	}
}

func TestToolStageAllowsLiveCheckAfterVerifiedProxyServiceInfoInSameTurn(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-live-sequence", Message: "Proxy 178.218.146.11 bị lỗi"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini của em là customer@example.com"}})
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{
		{ID: "call-service-first", Name: cloudminiProxyCheckToolName, Arguments: map[string]any{"ip": "178.218.146.11", "operation": "service_info", "account_email": "customer@example.com"}},
		{ID: "call-live-second", Name: cloudminiProxyCheckToolName, Arguments: map[string]any{"ip": "178.218.146.11", "operation": "live_check", "account_email": "customer@example.com"}},
	}}
	var operations []string
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, call providers.ToolCall) ([]providers.Message, error) {
		operation := cloudminiToolStringArg(call.Arguments, "operation")
		operations = append(operations, operation)
		content := `{"operation":"service_info","services":[{"ip":"178.218.146.11","plan":"PrivateV4","plan_family":"private_v4","expire":"2099-01-01T00:00:00Z","service_status":"active","account_email_matches":true}]}`
		if operation == "live_check" {
			content = `{"operation":"live_check","live_check":{"ip":"178.218.146.11","live":true}}`
		}
		return []providers.Message{{Role: "tool", ToolCallID: call.ID, Content: content}}, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := strings.Join(operations, ","); got != "service_info,live_check" {
		t.Fatalf("operations = %q", got)
	}
}

func TestToolStageRejectsInventedCloudminiAccountEmail(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cloudmini-email-truth", Message: "Kiểm tra 178.218.146.11 giúp em"}, nil, "", nil)
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID: "call-invented-email", Name: cloudminiProxyCheckToolName,
		Arguments: map[string]any{"ip": "178.218.146.11", "operation": "service_info", "account_email": "invented@example.com"},
	}}}
	executed := false
	stage := NewToolStage(&PipelineDeps{ExecuteToolCall: func(context.Context, *RunState, providers.ToolCall) ([]providers.Message, error) {
		executed = true
		return nil, nil
	}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if executed {
		t.Fatal("tool executed with an email not supplied by the customer")
	}
}
