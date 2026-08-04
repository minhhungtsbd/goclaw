package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// AntigravityCLIProvider invokes the locally installed agy CLI. GoClaw keeps
// the conversation state; agy receives an assembled prompt for each turn.
// This intentionally does not expose GoClaw tools because agy does not emit
// GoClaw-compatible structured tool calls.
type AntigravityCLIProvider struct {
	name         string
	cliPath      string
	defaultModel string
	baseWorkDir  string
	sessionMu    sync.Map
}

type AntigravityCLIOption func(*AntigravityCLIProvider)

func WithAntigravityCLIName(name string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) { if name != "" { p.name = name } }
}

func WithAntigravityCLIModel(model string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) { if model != "" { p.defaultModel = model } }
}

func WithAntigravityCLIWorkDir(dir string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) { if dir != "" { p.baseWorkDir = dir } }
}

func NewAntigravityCLIProvider(cliPath string, opts ...AntigravityCLIOption) *AntigravityCLIProvider {
	if cliPath == "" { cliPath = "agy" }
	p := &AntigravityCLIProvider{name: "antigravity-cli", cliPath: cliPath, defaultModel: "", baseWorkDir: filepath.Join(defaultCLIWorkDir(), "antigravity")}
	for _, opt := range opts { opt(p) }
	return p
}

func (p *AntigravityCLIProvider) Name() string { return p.name }
func (p *AntigravityCLIProvider) DefaultModel() string { return p.defaultModel }
func (p *AntigravityCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{Streaming: false, ToolCalling: false, StreamWithTools: false, Vision: true, MaxContextWindow: 200_000, TokenizerID: "cl100k_base"}
}

func (p *AntigravityCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	unlock := p.lockSession(sessionKey)
	defer unlock()
	workDir, err := p.workDir(sessionKey)
	if err != nil { return nil, err }
	prompt, err := p.buildPrompt(workDir, req.Messages)
	if err != nil { return nil, err }
	args := []string{"-p", prompt, "--output-format", "json"}
	model := req.Model
	if model == "" { model = p.defaultModel }
	if model != "" { args = append(args, "--model", model) }
	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	// Do not leak gateway credentials (DSN, API keys, gateway token) into the
	// autonomous CLI subprocess. It only needs its isolated HOME and PATH.
	cmd.Env = []string{"HOME=/app", "PATH=/usr/local/bin:/usr/bin:/bin"}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil { return nil, fmt.Errorf("antigravity-cli: %w (stderr: %s)", err, strings.TrimSpace(stderr.String())) }
	var result struct {
		Status string `json:"status"`
		Response string `json:"response"`
		Usage struct { InputTokens int `json:"input_tokens"`; OutputTokens int `json:"output_tokens"`; TotalTokens int `json:"total_tokens"` } `json:"usage"`
	}
	if err := json.Unmarshal(output, &result); err != nil { return nil, fmt.Errorf("antigravity-cli: invalid JSON response: %w", err) }
	if result.Status != "SUCCESS" { return nil, fmt.Errorf("antigravity-cli: status %q", result.Status) }
	if result.Response == "" { return nil, fmt.Errorf("antigravity-cli: empty response") }
	return &ChatResponse{Content: result.Response, FinishReason: "stop", Usage: &Usage{PromptTokens: result.Usage.InputTokens, CompletionTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens}}, nil
}

func (p *AntigravityCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	resp, err := p.Chat(ctx, req)
	if err != nil { return nil, err }
	onChunk(StreamChunk{Content: resp.Content, Done: true})
	return resp, nil
}

func (p *AntigravityCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex); m.Lock(); return m.Unlock
}

func (p *AntigravityCLIProvider) workDir(sessionKey string) (string, error) {
	name := deriveSessionUUID(sessionKey).String()
	dir := filepath.Join(p.baseWorkDir, name)
	if err := os.MkdirAll(dir, 0700); err != nil { return "", fmt.Errorf("antigravity-cli: create workspace: %w", err) }
	return dir, nil
}

func (p *AntigravityCLIProvider) buildPrompt(workDir string, messages []Message) (string, error) {
	var b strings.Builder
	for _, m := range messages {
		if m.Content == "" && len(m.Images) == 0 { continue }
		fmt.Fprintf(&b, "[%s]\n%s\n\n", m.Role, m.Content)
		for i, image := range m.Images {
			path, err := writeAntigravityImage(workDir, i, image)
			if err != nil { return "", err }
			fmt.Fprintf(&b, "Image attached for this message: %s\n", path)
		}
	}
	b.WriteString("Respond to the user. Do not modify files, submit forms, log in, or perform destructive actions unless the user explicitly requests it.")
	return b.String(), nil
}

func writeAntigravityImage(workDir string, index int, image ImageContent) (string, error) {
	if image.URL != "" { return image.URL, nil }
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil { return "", fmt.Errorf("antigravity-cli: decode image: %w", err) }
	ext := ".bin"
	if strings.HasPrefix(image.MimeType, "image/") { ext = "." + strings.TrimPrefix(image.MimeType, "image/") }
	path := filepath.Join(workDir, fmt.Sprintf("input-%d%s", index, ext))
	if err := os.WriteFile(path, data, 0600); err != nil { return "", fmt.Errorf("antigravity-cli: write image: %w", err) }
	return path, nil
}
