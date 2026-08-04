package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// AntigravityCLIProvider invokes the locally authenticated `agy` binary. OAuth
// remains owned by the CLI, so GoClaw does not store or proxy Google tokens.
type AntigravityCLIProvider struct {
	name         string
	cliPath      string
	defaultModel string
	baseWorkDir  string
	mu           sync.Mutex
	sessionMu    sync.Map
}

type AntigravityCLIOption func(*AntigravityCLIProvider)

func WithAntigravityCLIName(name string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

func WithAntigravityCLIModel(model string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) {
		p.defaultModel = strings.TrimSpace(model)
	}
}

func WithAntigravityCLIWorkDir(dir string) AntigravityCLIOption {
	return func(p *AntigravityCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

func NewAntigravityCLIProvider(cliPath string, opts ...AntigravityCLIOption) *AntigravityCLIProvider {
	if cliPath == "" {
		cliPath = "agy"
	}
	p := &AntigravityCLIProvider{
		name:        "antigravity-cli",
		cliPath:     cliPath,
		baseWorkDir: filepath.Join(defaultCLIWorkDir(), "antigravity"),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *AntigravityCLIProvider) Name() string         { return p.name }
func (p *AntigravityCLIProvider) DefaultModel() string { return p.defaultModel }

func (p *AntigravityCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        false,
		ToolCalling:      false, // AGY executes its own tools; GoClaw tool calls are not proxied.
		StreamWithTools:  false,
		Thinking:         true,
		Vision:           true,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

func (p *AntigravityCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	unlock := p.lockSession(sessionKey)
	defer unlock()

	workDir := p.ensureWorkDir(sessionKey)
	prompt, err := p.buildPrompt(req.Messages, workDir)
	if err != nil {
		return nil, err
	}

	args := []string{"--print", "--output-format", "json", "--print-timeout", "5m"}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.defaultModel
	}
	if model != "" && model != "default" {
		args = append(args, "--model", model)
	}
	if effort := strings.ToLower(strings.TrimSpace(extractStringOpt(req.Options, OptThinkingLevel))); effort == "low" || effort == "medium" || effort == "high" {
		args = append(args, "--effort", effort)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	slog.Debug("antigravity-cli exec", "path", p.cliPath, "model", model, "workdir", workDir)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("antigravity-cli: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseAntigravityCLIResponse(output)
}

func (p *AntigravityCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	resp, err := p.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Content != "" {
		onChunk(StreamChunk{Content: resp.Content})
	}
	onChunk(StreamChunk{Done: true})
	return resp, nil
}

func (p *AntigravityCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (p *AntigravityCLIProvider) ensureWorkDir(sessionKey string) string {
	dir := filepath.Join(p.baseWorkDir, sanitizePathSegment(sessionKey))
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("antigravity-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

func (p *AntigravityCLIProvider) buildPrompt(messages []Message, workDir string) (string, error) {
	var out strings.Builder
	out.WriteString("You are running as a GoClaw Antigravity provider. Follow the system instructions and answer the latest user request. Use native AGY browser and image-reading capabilities when needed. Do not emit fictional GoClaw function-call JSON.\n\n")
	for index, message := range messages {
		role := strings.ToUpper(message.Role)
		if role == "" {
			role = "USER"
		}
		fmt.Fprintf(&out, "[%s]\n%s\n", role, message.Content)
		for imageIndex, image := range message.Images {
			path, err := writeAntigravityImage(workDir, index, imageIndex, image)
			if err != nil {
				return "", err
			}
			if path != "" {
				fmt.Fprintf(&out, "Attached image for this message: %s\n", path)
			}
		}
	}
	return out.String(), nil
}

func writeAntigravityImage(workDir string, messageIndex, imageIndex int, image ImageContent) (string, error) {
	if image.Data == "" {
		return image.URL, nil
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		return "", fmt.Errorf("antigravity-cli: decode image: %w", err)
	}
	ext := ".bin"
	switch image.MimeType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	path := filepath.Join(workDir, fmt.Sprintf("input-%03d-%03d%s", messageIndex, imageIndex, ext))
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("antigravity-cli: write image: %w", err)
	}
	return path, nil
}

type antigravityCLIJSONResponse struct {
	Status   string `json:"status"`
	Response string `json:"response"`
	Usage    struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func parseAntigravityCLIResponse(data []byte) (*ChatResponse, error) {
	var result antigravityCLIJSONResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("antigravity-cli: decode response: %w", err)
	}
	if !strings.EqualFold(result.Status, "SUCCESS") {
		return nil, fmt.Errorf("antigravity-cli: command returned status %q", result.Status)
	}
	if strings.TrimSpace(result.Response) == "" {
		return nil, fmt.Errorf("antigravity-cli: empty response")
	}
	return &ChatResponse{
		Content:      result.Response,
		FinishReason: "stop",
		Usage: &Usage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
	}, nil
}
