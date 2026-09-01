package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCloudminiServicePreflightCallsServiceInfoBeforeLLM(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Kiểm tra Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini: customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			if tc.Arguments["operation"] == "live_check" {
				return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"live_check":{"ip":"94.103.56.231","live":true}}`}}, nil
			}
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"94.103.56.231","service_status":"active","plan":"PrivateV4","plan_family":"proxy","account_email_matches":true}]}`}}, nil
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

func TestCloudminiResidentialVNHostnameSkipsIPv4APIPreflight(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy ipv4-vt-04.resvn.net tải YouTube rất chậm"}, nil, "", nil)
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "base"})
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email Cloudmini: customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			t.Fatal("Residential VN hostname must skip IPv4-only service_info/live_check")
			return nil, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if got := strings.Join(state.Cloudmini.RequestHosts, ","); got != "ipv4-vt-04.resvn.net" {
		t.Fatalf("RequestHosts = %q", got)
	}
	if content := state.Messages.System().Content; !strings.Contains(strings.ToLower(content), "không yêu cầu khách cung cấp ip dạng số") ||
		!strings.Contains(content, "được phép chuyển Admin/Kỹ thuật ngay") {
		t.Fatalf("Residential VN runtime context missing: %s", content)
	}
}

func TestCloudminiResidentialVNContinuationReusesRecentHostname(t *testing.T) {
	state := NewRunState(&RunInput{Message: "vẫn chưa load nổi YouTube, thay proxy giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "ipv4-vt-04.resvn.net:18118:user:secret"},
		{Role: "assistant", Content: "Anh thử lại giúp em."},
	})
	if !isCloudminiServiceRequest(state) {
		t.Fatal("Residential VN follow-up was not recognized as the same support request")
	}
	if got := strings.Join(resolveCloudminiRequestHosts(state), ","); got != "ipv4-vt-04.resvn.net" {
		t.Fatalf("resolved hosts = %q", got)
	}
}

func TestCloudminiResidentialVNHostnameRequiresExactDomainSuffix(t *testing.T) {
	if hosts := cloudminiResidentialVNHosts("ipv4-vt-04.resvn.net.evil.example"); len(hosts) != 0 {
		t.Fatalf("lookalike hostname accepted: %#v", hosts)
	}
}

func TestCloudminiResidentialVNHandoffUsesHostnameAndCustomerEmail(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy ipv4-vt-04.resvn.net không load YouTube, thay giúp em"}, nil, "", nil)
	state.Cloudmini.RequestHosts = []string{"ipv4-vt-04.resvn.net"}
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Tài khoản customer@example.com"}})
	checkCall := providers.ToolCall{Name: cloudminiProxyCheckToolName, Arguments: map[string]any{
		"operation": "service_info", "ip": "ipv4-vt-04.resvn.net",
	}}
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, checkCall); ok || !strings.Contains(reason, "không gọi") {
		t.Fatalf("hostname API call = %v, reason=%q", ok, reason)
	}
	handoff := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{
		"service": "Proxy Residential VN", "summary": "Proxy ipv4-vt-04.resvn.net không tải được YouTube, khách yêu cầu kiểm tra hoặc thay proxy",
		"identifiers": []any{"ipv4-vt-04.resvn.net", "customer@example.com"},
	}}
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, handoff); !ok {
		t.Fatalf("valid Residential VN handoff rejected: %s", reason)
	}
	handoff.Arguments["identifiers"] = []any{"ipv4-vt-05.resvn.net", "customer@example.com"}
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, handoff); ok {
		t.Fatal("handoff with an old/different Residential VN hostname was accepted")
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

func TestValidateCloudminiCurrentRequestToolCallHonorsIncidentHandoffFlag(t *testing.T) {
	const ip = "147.189.140.177"
	newState := func(allows bool) *RunState {
		state := NewRunState(&RunInput{Message: "Proxy " + ip + " đang lỗi kết nối"}, nil, "", nil)
		state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{
			IP: ip, Status: "active", PlanFamily: "private_v4", AccountEmailMatches: true,
		}}
		state.Cloudmini.LiveAttempts = map[string]bool{ip: true}
		state.Cloudmini.IncidentsByIP = map[string]store.OperationalIncident{
			ip: {Severity: "temporary_issue", AllowsAdminHandoff: allows, CustomerMessage: "Dải này đang lỗi tạm thời."},
		}
		return state
	}
	call := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{
		"summary": "Proxy " + ip + " lỗi kết nối; live_check không trả trạng thái",
	}}

	if ok, reason := validateCloudminiCurrentRequestToolCall(newState(false), call); ok || !strings.Contains(reason, "không cho phép") {
		t.Fatalf("handoff disallowed by incident = %v, reason=%q", ok, reason)
	}
	if ok, reason := validateCloudminiCurrentRequestToolCall(newState(true), call); !ok {
		t.Fatalf("handoff allowed by incident was rejected: %s", reason)
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

func TestCloudminiServicePreflightSkipsLiveCheckForUnverifiedService(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Kiểm tra Proxy 37.221.109.121 lỗi kết nối"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
		calls = append(calls, tc)
		return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"service_status":"not_verified","plan":"PrivateV4"}]}`}}, nil
	}})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["operation"] != "service_info" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestCloudminiServicePreflightRecordsUnavailableFactOnToolFailure(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 37.221.109.121 lỗi kết nối"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
		return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `Cloudmini proxy check failed to reach the service`, IsError: true}}, nil
	}})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].IP != "37.221.109.121" || state.Cloudmini.ServiceFacts[0].Status != "unavailable" {
		t.Fatalf("service facts = %#v", state.Cloudmini.ServiceFacts)
	}
	if state.Cloudmini.EmailRequired {
		t.Fatal("a tool outage must not be misreported as missing customer email")
	}
	if state.Cloudmini.EmailMismatch {
		t.Fatal("a tool outage must not be misreported as an account mismatch")
	}
	reply := adminHandoffCustomerConfirmationWithFacts(state, "Ticket-000304")
	if !strings.Contains(reply, "công cụ kiểm tra chưa trả dữ liệu") {
		t.Fatalf("tool outage explanation missing: %s", reply)
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

func TestCloudminiServicePreflightSkipsUnrelatedIPAddress(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Máy chủ DNS hiện tại là 8.8.8.8"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
		t.Fatal("unrelated IP must not invoke Cloudmini tool")
		return nil, nil
	}})
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

func TestCloudminiServicePreflightIgnoresLegacyOperationalSubnetFromAgents(t *testing.T) {
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
	if strings.Contains(content, "THÔNG BÁO VẬN HÀNH KHỚP IP HIỆN TẠI") {
		t.Fatalf("legacy free-form notice affected runtime: %s", content)
	}
	if len(state.Cloudmini.OutageCIDRs) != 0 {
		t.Fatalf("legacy free-form CIDRs were trusted: %#v", state.Cloudmini.OutageCIDRs)
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

func TestCloudminiServicePreflightKeepsExplicitStatusWhenExpiryIsRedacted(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-recovery", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email tài khoản Cloudmini của em là customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":null,"service_status":"active","account_email_matches":true}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].Status != "active" {
		t.Fatalf("service facts = %#v", state.Cloudmini.ServiceFacts)
	}
}

func TestCloudminiServicePreflightDoesNotTurnNotVerifiedIntoDeleted(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-not-verified", Message: "37.221.109.121 lỗi kết nối"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
		return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"37.221.109.121","plan":"PrivateV4","expire":null,"service_status":"not_verified","account_email_matches":false}]}`}}, nil
	}})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].Status != "not_verified" {
		t.Fatalf("service facts = %#v", state.Cloudmini.ServiceFacts)
	}
}

func TestCloudminiServicePreflightRequiresEmailBeforeRecoveryDecision(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-recovery-email", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"Residential Static","expire":null,"service_status":"active"}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !state.Cloudmini.EmailRequired {
		t.Fatalf("email gate state = %#v", state.Cloudmini)
	}
	if !strings.Contains(state.Messages.System().Content, "Chỉ yêu cầu email tài khoản") {
		t.Fatal("email gate instruction missing")
	}
}

func TestCloudminiServicePreflightRechecksPreviousIPAfterEmailReply(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-email-continuation", Message: "customer@example.com"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "Khôi phục 178.218.146.11 giúp em"},
		{Role: "assistant", Content: "Anh cho em xin email tài khoản Cloudmini nhé."},
	})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []providers.ToolCall
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc)
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":"2099-09-01T00:00:00Z","service_status":"active","account_email_matches":false}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["ip"] != "178.218.146.11" || calls[0].Arguments["account_email"] != "customer@example.com" {
		t.Fatalf("calls = %#v", calls)
	}
	if !strings.Contains(state.Messages.System().Content, "Không nói IP thuộc tài khoản khác") {
		t.Fatalf("mismatch guard missing: email_mismatch=%v system=%q", state.Cloudmini.EmailMismatch, state.Messages.System().Content)
	}
}

func TestCloudminiResponseGuardBlocksOwnershipInference(t *testing.T) {
	state := NewRunState(&RunInput{Message: "customer@example.com"}, nil, "", nil)
	state.Cloudmini.EmailMismatch = true
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "Khôi phục 92.113.182.109 giúp em"},
		{Role: "assistant", Content: "Anh cho em xin email tài khoản Cloudmini nhé."},
	})
	if !cloudminiResponseViolatesGuard(state, "IP này thuộc tài khoản khác nên không thể khôi phục.") {
		t.Fatal("ownership inference must be rejected")
	}
	if !cloudminiResponseViolatesGuard(state, "Dạ, hiện tại em chưa thể hỗ trợ khôi phục hoặc gia hạn IP này ạ.") {
		t.Fatal("a text-only recovery response must be rejected until Admin handoff succeeds")
	}
	state.Tool.AdminHandoffTicket = "Ticket-000401"
	if cloudminiResponseViolatesGuard(state, cloudminiEmailMismatchReply(state, state.Tool.AdminHandoffTicket)) {
		t.Fatal("canonical response with a real Admin ticket must be allowed")
	}
}

func TestCloudminiEmailMismatchReplyIncludesVerifiedTicketOnlyAfterHandoff(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 92.113.182.109 giúp em"}, nil, "", nil)
	state.Cloudmini.EmailMismatch = true
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{
		IP: "92.113.182.109", Status: "not_verified", AccountEmailMatches: false,
	}}
	withoutTicket := cloudminiEmailMismatchReply(state, "")
	if !strings.Contains(withoutTicket, "IP 92.113.182.109 hiện tại chưa thể xác minh được thông tin") {
		t.Fatalf("neutral mismatch reply = %q", withoutTicket)
	}
	if strings.Contains(strings.ToLower(withoutTicket), "chuyển case") {
		t.Fatalf("reply claimed handoff without a ticket: %q", withoutTicket)
	}
	withTicket := cloudminiEmailMismatchReply(state, "Ticket-000402")
	if !strings.Contains(withTicket, "đã chuyển case này cho Admin kiểm tra trực tiếp") ||
		!strings.Contains(withTicket, "Ticket-000402") {
		t.Fatalf("ticket confirmation = %q", withTicket)
	}
}

func TestCloudminiEmailMismatchFallbackDoesNotInventRecoveryIntent(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 92.113.182.109 đang lỗi kết nối"}, nil, "", nil)
	state.Cloudmini.EmailMismatch = true
	reply := cloudminiSafeGuardResponse(state)
	if strings.Contains(strings.ToLower(reply), "khôi phục") || strings.Contains(strings.ToLower(reply), "gia hạn") {
		t.Fatalf("non-recovery fallback invented a recovery intent: %q", reply)
	}
}

func TestAdminHandoffResponseGuardRequiresOneTicketReply(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Khôi phục 92.113.182.109 giúp em"}, nil, "", nil)
	state.Tool.AdminHandoffTicket = "Ticket-000276"
	state.Tool.AdminHandoffCustomerReplyRequired = true
	if !adminHandoffResponseViolatesGuard(state, "Dạ em đã chuyển yêu cầu đến Admin ạ.") {
		t.Fatal("final response without the ticket must be rejected")
	}
	if adminHandoffResponseViolatesGuard(state, "Dạ em đã ghi nhận và chuyển yêu cầu. Mã Ticket-000276 của anh là Ticket-000276 ạ.") {
		t.Fatal("final response containing the ticket must be accepted")
	}
}

func TestCloudminiServicePreflightTreatsDeletedRecoveryEmailAsDestination(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-deleted-other-owner", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email nhận khôi phục là customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":null,"service_status":"deleted","account_email_matches":false}]}`}}, nil
		},
	})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if state.Cloudmini.EmailMismatch {
		t.Fatal("deleted IP restore email is a destination account, not a former-owner match")
	}
}

func TestCloudminiServicePreflightStillRejectsActiveServiceEmailMismatch(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-active-other-owner", Message: "Khôi phục 178.218.146.11 giúp em"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "Email của em là customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"178.218.146.11","plan":"PrivateV4","expire":"2099-09-01T00:00:00Z","service_status":"active","account_email_matches":false}]}`}}, nil
		},
	})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !state.Cloudmini.EmailMismatch {
		t.Fatal("active service must still require an account email match")
	}
}

func TestCloudminiServicePreflightChecksLiveAfterServiceIdentifiesProxy(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-live", Message: "94.103.56.231 lỗi ạ"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var operations []string
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			operation := tc.Arguments["operation"].(string)
			operations = append(operations, operation)
			content := `{"services":[{"ip":"94.103.56.231","plan":"PrivateV4","plan_family":"proxy","expire":"2026-09-02T00:00:00Z","service_status":"active","account_email_matches":true}]}`
			if operation == "live_check" {
				content = `{"live_check":{"ip":"94.103.56.231","live":true}}`
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

func TestCloudminiServicePreflightAuthorizesLiveCheck(t *testing.T) {
	state := NewRunState(&RunInput{RunID: "run-live-auth", Message: "Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "customer@example.com"}})
	state.Think.Tools = []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: cloudminiProxyCheckToolName}}}
	var calls []string
	stage := NewCloudminiServicePreflightStage(&PipelineDeps{
		AuthorizeToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) (bool, string) {
			if tc.Arguments["operation"] == "live_check" {
				return false, "denied in test"
			}
			return true, ""
		},
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			calls = append(calls, tc.Arguments["operation"].(string))
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: `{"services":[{"ip":"94.103.56.231","plan":"PrivateV4","plan_family":"proxy","service_status":"active","account_email_matches":true}]}`}}, nil
		},
	})
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if strings.Join(calls, ",") != "service_info" {
		t.Fatalf("unauthorized live_check executed: %#v", calls)
	}
}

func TestCloudminiLiveCheckUsesOutageScopePerIP(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 10.0.0.10 và 10.0.1.10 đều lỗi"}, nil, "", nil)
	state.Cloudmini.OutageCIDRs = []string{"10.0.0.0/24"}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "10.0.0.10", Plan: "PrivateV4", PlanFamily: "proxy", Status: "active", AccountEmailMatches: true},
		{IP: "10.0.1.10", Plan: "PrivateV4", PlanFamily: "proxy", Status: "active", AccountEmailMatches: true},
	}
	if requiresCloudminiProxyLiveCheck(state, "10.0.0.10") {
		t.Fatal("outage IP must skip live_check")
	}
	if !requiresCloudminiProxyLiveCheck(state, "10.0.1.10") {
		t.Fatal("unaffected IP must still require live_check")
	}
}

func TestCloudminiLiveCheckValidatorUsesOutageScopePerIP(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 10.0.0.10 và 10.0.1.10 đều lỗi"}, nil, "", nil)
	state.Cloudmini.RequestIPs = []string{"10.0.0.10", "10.0.1.10"}
	state.Cloudmini.OutageCIDRs = []string{"10.0.0.0/24"}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{
		{IP: "10.0.0.10", Plan: "PrivateV4", PlanFamily: "proxy", Status: "active", AccountEmailMatches: true},
		{IP: "10.0.1.10", Plan: "PrivateV4", PlanFamily: "proxy", Status: "active", AccountEmailMatches: true},
	}
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "customer@example.com"}})
	outageCall := providers.ToolCall{Name: cloudminiProxyCheckToolName, Arguments: map[string]any{
		"operation": "live_check", "ip": "10.0.0.10", "account_email": "customer@example.com",
	}}
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, outageCall); ok {
		t.Fatal("outage IP live_check must be rejected")
	}
	unaffectedCall := providers.ToolCall{Name: cloudminiProxyCheckToolName, Arguments: map[string]any{
		"operation": "live_check", "ip": "10.0.1.10", "account_email": "customer@example.com",
	}}
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, unaffectedCall); !ok {
		t.Fatalf("unaffected IP live_check rejected: %s", reason)
	}
}

func TestCloudminiHandoffRequiresTriageWhenProxyIsLive(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Cloudmini.RequestIPs = []string{"94.103.56.231"}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "94.103.56.231", PlanFamily: "proxy", Status: "active", AccountEmailMatches: true}}
	state.Cloudmini.LiveChecks = map[string]bool{"94.103.56.231": true}
	call := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{"summary": "Proxy 94.103.56.231 lỗi kết nối"}}
	if ok, _ := validateCloudminiCurrentRequestToolCall(state, call); ok {
		t.Fatal("LIVE proxy must be explained and triaged before handoff")
	}
	state.Input.Message += " khách đã thử WARP và 4G"
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, call); !ok {
		t.Fatalf("handoff after customer triage rejected: %s", reason)
	}
}

func TestCloudminiHandoffAllowsEscalationAfterLiveCheckToolFailure(t *testing.T) {
	state := NewRunState(&RunInput{Message: "Proxy 94.103.56.231 lỗi kết nối"}, nil, "", nil)
	state.Cloudmini.RequestIPs = []string{"94.103.56.231"}
	state.Cloudmini.ServiceFacts = []CloudminiServiceFact{{IP: "94.103.56.231", PlanFamily: "private_v4", Status: "active", AccountEmailMatches: true}}
	state.Cloudmini.LiveAttempts = map[string]bool{"94.103.56.231": true}
	call := providers.ToolCall{Name: "escalate_to_admin", Arguments: map[string]any{"summary": "Proxy 94.103.56.231 lỗi kết nối; live_check không trả trạng thái"}}
	if ok, reason := validateCloudminiCurrentRequestToolCall(state, call); !ok {
		t.Fatalf("handoff after live_check tool failure rejected: %s", reason)
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
	if len(state.Cloudmini.ServiceFacts) != 1 || state.Cloudmini.ServiceFacts[0].CancellationPolicy != "not_supported" {
		t.Fatalf("cancellation policy facts = %#v", state.Cloudmini.ServiceFacts)
	}
}
