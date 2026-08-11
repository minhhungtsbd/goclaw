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

func TestCloudminiProxyCheckReturnsServiceEmailForInternalMatching(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "private@example.com", []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"191.101.251.120","expire":"2026-08-12","plan":"PrivateV4","user_email":"private@example.com"}]}`))
	if err != nil {
		t.Fatalf("sanitizeCloudminiProxyResponse: %v", err)
	}
	if contains := string(got); contains == "" || !json.Valid([]byte(got)) {
		t.Fatalf("result = %q", got)
	}
	if !strings.Contains(got, "private@example.com") {
		t.Fatalf("service email missing: %s", got)
	}
	if !strings.Contains(got, `"account_email_matches":true`) {
		t.Fatalf("email match missing: %s", got)
	}
}

func TestCloudminiProxyCheckMarksNullExpiryAsDeleted(t *testing.T) {
	got, err := sanitizeCloudminiProxyResponse("service_info", "191.101.251.120", "", []byte(`{"error":false,"msg":"Success","data":[{"id":1,"ip":"191.101.251.120","expire":null,"plan":"PrivateV4","user_email":"private@example.com"}]}`))
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
		_, _ = w.Write([]byte(`{"error":false,"msg":"Success","data":{"ip":"51.194.203.22","country":"New Zealand","state_prov":"Auckland","city":"Auckland","zipcode":"1010"}}`))
	}))
	defer server.Close()
	tool.client = server.Client()
	tool.baseURL = server.URL + "/api/v2/admin"
	ctx := WithToolAgentKey(context.Background(), "linh-nhi-support-lead")
	ctx = WithBuiltinToolSettings(ctx, BuiltinToolSettings{"cloudmini_proxy_check": []byte(`{"allowed_agent_keys":["linh-nhi-support-lead"]}`)})

	result := tool.Execute(ctx, map[string]any{"ip": "51.194.203.22", "operation": "live_check"})
	if result.IsError || !strings.Contains(result.ForLLM, "New Zealand") {
		t.Fatalf("result = %#v", result)
	}
}

func TestCloudminiProxyCheckAllowsAgentWhenNoToolRestrictionConfigured(t *testing.T) {
	tool := NewCloudminiProxyCheckTool(nil)
	ctx := WithToolAgentKey(context.Background(), "another-support-agent")
	ctx = WithBuiltinToolSettings(ctx, BuiltinToolSettings{"cloudmini_proxy_check": []byte(`{"timeout_seconds":15}`)})

	if !tool.allowedForAgent(ctx) {
		t.Fatal("tool should defer to the agent-specific allow-list when allowed_agent_keys is empty")
	}
}
