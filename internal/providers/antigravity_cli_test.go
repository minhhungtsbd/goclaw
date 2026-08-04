package providers

import (
	"strings"
	"testing"
)

func TestParseAntigravityCLIResponse(t *testing.T) {
	resp, err := parseAntigravityCLIResponse([]byte(`{
		"status":"SUCCESS",
		"response":"Xin chao",
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`))
	if err != nil {
		t.Fatalf("parseAntigravityCLIResponse() error = %v", err)
	}
	if resp.Content != "Xin chao" || resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestParseAntigravityCLIResponseRejectsEmptyOutput(t *testing.T) {
	_, err := parseAntigravityCLIResponse([]byte(`{"status":"SUCCESS","response":""}`))
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestAntigravityCLIBuildPromptWritesImage(t *testing.T) {
	dir := t.TempDir()
	p := NewAntigravityCLIProvider("agy")
	prompt, err := p.buildPrompt([]Message{{
		Role:    "user",
		Content: "Read this image",
		Images: []ImageContent{{
			MimeType: "image/png",
			Data:     "aGVsbG8=",
		}},
	}}, dir)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "input-000-000.png") {
		t.Fatalf("prompt does not reference materialized image: %q", prompt)
	}
}
