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

func TestCloudminiServicePreflightReusesCustomerEmail(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 48.45.161.144 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{
		{Role: "assistant", Content: "Admin nói email admin@example.com"},
		{Role: "user", Content: "Email Cloudmini của em là customer@example.com"},
	})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var call providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			call = tc
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"deleted"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if got, _ := call.Arguments["account_email"].(string); got != "customer@example.com" {
		t.Fatalf("account_email = %q, want customer email", got)
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
		"summary":     "Khôi phục proxy 48.45.161.144",
		"identifiers": []any{"customer@example.com", "48.45.161.144"},
	}}
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, partial); ok {
		t.Fatal("partial multi-IP recovery handoff should be rejected")
	}
	complete := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{
		"summary":     "Khôi phục proxy 48.45.161.144 và 48.45.161.122",
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

func TestCloudminiServicePreflightUsesUniqueBoundedIDsAcrossRuns(t *testing.T) {
	var ids []string
	for _, runID := range []string{"run-one", "run-two"} {
		state := NewRunState(&RunInput{RunID: runID, Message: "Kiểm tra 178.218.146.11 lỗi"}, nil, "", nil)
		state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
		stage := NewCloudminiServicePreflightStage(&PipelineDeps{
			ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
				ids = append(ids, tc.ID)
				return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"active"}]}`}}, nil
			},
		})
		if err := stage.Execute(context.Background(), state); err != nil {
			t.Fatalf("preflight: %v", err)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("preflight IDs = %#v, want unique IDs", ids)
	}
	for _, id := range ids {
		if len(id) > 40 {
			t.Fatalf("preflight ID too long: %q", id)
		}
	}
}

func TestCloudminiServicePreflightFocusesMatchingOperationalSubnetFromAgents(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-subnet", Message: "178.218.146.11 lỗi ạ"}, nil, "", nil)
	state.Messages.SetSystem(providers.Message{Role: "system", Content: `base

# Subnet IPv4 ngưng hoạt động hoàn toàn:
178.218.146.0/24
167.253.142.0/24
- Trạng thái: Dải IP ngưng hoạt động theo kế hoạch.
- Khi khách hỏi: hỗ trợ thay thế miễn phí hoặc hoàn tiền.

## Quy tắc khác
không liên quan`})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"active","account_email_matches":true}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	content := state.Messages.System().Content
	if !strings.Contains(content, "THÔNG BÁO VẬN HÀNH KHỚP IP HIỆN TẠI") ||
		!strings.Contains(content, "178.218.146.0/24") || !strings.Contains(content, "thay thế miễn phí hoặc hoàn tiền") {
		t.Fatalf("matching operational notice was not focused: %s", content)
	}
	if len(state.Cloudmini.OutageCIDRs) != 1 || state.Cloudmini.OutageCIDRs[0] != "178.218.146.0/24" {
		t.Fatalf("matched CIDRs = %#v", state.Cloudmini.OutageCIDRs)
	}
}

func TestCloudminiServicePreflightResolvesExplicitContinuationCount(t *testing.T) {
	state := NewRunState(&RunInput{
		RunID:   "run-continuation",
		Message: "195.114.201.29 80.174.109.184 khôi phục 4 IP này với admin",
	}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "Khôi phục 213.182.196.136 và 213.182.196.112"},
		{Role: "assistant", Content: "Em đang kiểm tra"},
	})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var checked []string
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			checked = append(checked, tc.Arguments["ip"].(string))
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"deleted"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	want := []string{"213.182.196.136", "213.182.196.112", "195.114.201.29", "80.174.109.184"}
	if strings.Join(checked, ",") != strings.Join(want, ",") {
		t.Fatalf("checked = %#v, want %#v", checked, want)
	}
	if state.Cloudmini.ScopeAmbiguous {
		t.Fatal("exact continuation count should not be ambiguous")
	}
}

func TestCloudminiServicePreflightRequiresHandoffForVerifiedDeletedRecovery(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-recovery", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":null,"service_status":"active","account_email_matches":true}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !state.Cloudmini.HandoffRequired {
		t.Fatal("verified recovery for expire=null service must require Admin handoff")
	}
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].Status != "deleted" {
		t.Fatalf("service facts = %#v", state.Cloudmini.ServiceFacts)
	}
}

func TestCloudminiServicePreflightChecksLiveAfterServiceIdentifiesProxy(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-live", Message: "94.103.56.231 lỗi ạ"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var operations []string
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			operation := tc.Arguments["operation"].(string)
			operations = append(operations, operation)
			content := `{"services":[{"ip":"94.103.56.231","plan":"PrivateV4","expire":"2026-09-02T00:00:00Z","service_status":"active"}]}`
			if operation == "live_check" {
				content = `{"success":true,"live":true}`
			}
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: content}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if strings.Join(operations, ",") != "service_info,live_check" {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestCloudminiServicePreflightDoesNotForceCancellationHandoffWhenUnsupported(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-cancel", Message: "Hủy IP 94.103.56.231 nhưng web báo lỗi không thể hủy"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"94.103.56.231","plan":"Residential Static","expire":"2026-09-02T00:00:00Z","service_status":"active","account_email_matches":true,"cancellation_policy":"not_supported"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if state.Cloudmini.HandoffRequired {
		t.Fatal("not_supported cancellation must be answered from policy without forced handoff")
	}
}
