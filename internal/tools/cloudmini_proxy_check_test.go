package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type cloudminiTestSecretsStore struct{ data map[string]string }

func (s *cloudminiTestSecretsStore) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}
func (s *cloudminiTestSecretsStore) Set(_ context.Context, key, value string) error {
	s.data[key] = value
	return nil
}
func (s *cloudminiTestSecretsStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}
func (s *cloudminiTestSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return s.data, nil
}

func TestCloudminiProxyCheckReturnsMatchForMatchingAccountEmail(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "private@example.com", nil, []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"191.101.251.120","expire":"2099-08-12","plan":"PrivateV4","user_email":"private@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if contains := string(got); contains == "" || !json.Valid([]byte(got)) {
		t.Fatalf("result = %q", got)
	}
	if !strings.Contains(got, `"account_email_matches":true`) {
		t.Fatalf("email match missing: %s", got)
	}
	if !strings.Contains(got, `"2099-08-12"`) {
		t.Fatalf("expire date should be visible when email matches: %s", got)
	}
	if !strings.Contains(got, `"cancellation_policy":"self_service"`) {
		t.Fatalf("PrivateV4 cancellation policy missing: %s", got)
	}
}

func TestCloudminiProxyCheckReturnsNoCancellationPolicyForResidentialStatic(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "109.166.49.164", "customer@example.com", nil, []byte(`{"error":false,"msg":"Success","data":[{"ip":"109.166.49.164","expire":"2099-09-14","plan":"Residential Static","user_email":"customer@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if !strings.Contains(got, `"cancellation_policy":"not_supported"`) {
		t.Fatalf("Residential Static policy missing: %s", got)
	}
	if !strings.Contains(got, "không hỗ trợ hủy hoặc hoàn tiền") {
		t.Fatalf("Residential Static instruction missing: %s", got)
	}
}

func TestCloudminiProxyCheckHandlesSingleObjectData(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "31.57.203.176", "lamithan@gmail.com", nil, []byte(`{"error":false,"msg":"Success","data":{"ip":"31.57.203.176","expire":"2099-09-11T03:59:16.106083Z","plan":"PrivateV4","user_email":"lamithan@gmail.com","region":"Illinois"}}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse single object: %v", err)
	}
	if !strings.Contains(got, "PrivateV4") || !strings.Contains(got, "Illinois") {
		t.Fatalf("result missing expected fields: %s", got)
	}
}

func TestCloudminiProxyCheckMarksNullExpiryAsDeleted(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "", nil, []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"191.101.251.120","expire":null,"plan":"PrivateV4","user_email":"private@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if !strings.Contains(got, `"expire":null`) {
		t.Fatalf("expiry must remain null: %s", got)
	}
	if !strings.Contains(got, `"service_status":"deleted"`) {
		t.Fatalf("deleted service status missing: %s", got)
	}
}

func TestCloudminiProxyCheckRedactsExpiryAndEmailWhenNoAccountEmail(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "46.203.160.119", "", nil, []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"46.203.160.119","expire":"2099-08-17","plan":"Residential Static","user_email":"mtanh97@gmail.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if strings.Contains(got, "mtanh97@gmail.com") {
		t.Fatalf("user_email must be redacted: %s", got)
	}
	if strings.Contains(got, "2099-08-17") {
		t.Fatalf("expiry must be redacted when no account_email supplied: %s", got)
	}
	if !strings.Contains(got, "BẮT BUỘC phải hỏi xin email") {
		t.Fatalf("status_note must prompt for email: %s", got)
	}
}

func TestCloudminiProxyCheckRedactsUnmatchedAccountEmail(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "userA@example.com", nil, []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"191.101.251.120","expire":"2099-09-11","plan":"PrivateV4","user_email":"userB@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if strings.Contains(got, "userB@example.com") {
		t.Fatalf("unmatched user_email must be redacted: %s", got)
	}
	if strings.Contains(got, "2099-09-11") {
		t.Fatalf("unmatched expire date must be redacted: %s", got)
	}
	if !strings.Contains(got, `"account_email_matches":false`) {
		t.Fatalf("account_email_matches false missing: %s", got)
	}
	if !strings.Contains(got, `"service_status":"unavailable"`) {
		t.Fatalf("service_status unavailable missing: %s", got)
	}
	if !strings.Contains(got, "IP hiện không còn khả dụng.") {
		t.Fatalf("status_note missing: %s", got)
	}
}

func TestCloudminiProxyCheckCallsFixedEndpoint(t *testing.T) {
	secrets := &cloudminiTestSecretsStore{data: map[string]string{cloudminiProxyTokenKey: "secret"}}
	tool := NewCloudminiProxyCheckTool(secrets)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/admin/check_live_proxy" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Token secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"error":false,"msg":"Success","data":{"ip":"51.194.203.22","live":true}}`))
	}))
	defer server.Close()
	tool.client = server.Client()
	tool.baseURL = server.URL + "/api/v2/admin"
	ctx := WithToolAgentKey(context.Background(), "linh-nhi-support-lead")
	ctx = WithBuiltinToolSettings(ctx, BuiltinToolSettings{"cloudmini_proxy_check": []byte(`{"allowed_agent_keys":["linh-nhi-support-lead"]}`)})

	result := tool.Execute(ctx, map[string]any{"ip": "51.194.203.22", "operation": "live_check"})
	if result.IsError || !strings.Contains(result.ForLLM, `"live":true`) {
		t.Fatalf("result = %#v", result)
	}
}

func TestCloudminiProxyCheckFlagsConfiguredResellerOnlyAfterEmailMatch(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "reseller@example.com", []string{"reseller@example.com"}, []byte(`{"error":false,"msg":"Success","data":[{"ip":"191.101.251.120","expire":"2099-08-12","plan":"PrivateV4","user_email":"reseller@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if !strings.Contains(got, `"is_reseller_vip":true`) {
		t.Fatalf("configured reseller flag missing: %s", got)
	}
	if !strings.Contains(got, `"cancellation_policy":"admin_review"`) {
		t.Fatalf("reseller cancellation policy missing: %s", got)
	}
	if !strings.Contains(got, "ƯU TIÊN RESELLER BẮT BUỘC") {
		t.Fatalf("reseller cancellation instruction missing: %s", got)
	}

	got, err = sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "reseller@example.com", []string{"reseller@example.com"}, []byte(`{"error":false,"msg":"Success","data":[{"ip":"191.101.251.120","expire":"2099-08-12","plan":"PrivateV4","user_email":"other@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if strings.Contains(got, `"is_reseller_vip":true`) {
		t.Fatalf("unmatched account must not receive reseller privileges: %s", got)
	}
}

func TestCloudminiProxyCheckMarksPastExpiryAsExpired(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "customer@example.com", nil, []byte(`{"error":false,"msg":"Success","data":[{"ip":"191.101.251.120","expire":"2020-01-01","plan":"PrivateV4","user_email":"customer@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if !strings.Contains(got, `"service_status":"expired"`) {
		t.Fatalf("expired service status missing: %s", got)
	}
}

func TestCloudminiProxyCheckDistinguishesBudgetResidentialStatic(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "109.166.48.123", "customer@example.com", nil, []byte(`{"error":false,"msg":"Success","data":[{"ip":"109.166.48.123","expire":null,"plan":"Budget Residential Static","user_email":"customer@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if !strings.Contains(got, `"plan_family":"budget_residential_static"`) {
		t.Fatalf("budget plan family missing: %s", got)
	}
	if strings.Contains(got, "25.000đ/IP") {
		t.Fatalf("ordinary Residential Static recovery fee leaked into Budget plan: %s", got)
	}
}
