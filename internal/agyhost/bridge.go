// Package agyhost exposes a local, OpenAI-compatible bridge for a host-native
// Antigravity CLI. It intentionally runs outside Docker because AGY is rejected
// by Google when executed from the deployment container.
package agyhost

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const defaultModel = "default"

var profileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Config struct {
	ListenAddr  string
	AGYPath     string
	ProfilesDir string
	Token       string
}

type Bridge struct {
	cfg      Config
	upgrader websocket.Upgrader
	mu       sync.Mutex
	active   map[string]struct{}
}

func New(cfg Config) (*Bridge, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("AGY host bridge token is required")
	}
	if cfg.AGYPath == "" {
		cfg.AGYPath = "agy"
	}
	if cfg.ProfilesDir == "" {
		cfg.ProfilesDir = "/var/lib/goclaw-agy/profiles"
	}
	if err := os.MkdirAll(cfg.ProfilesDir, 0o700); err != nil {
		return nil, fmt.Errorf("create AGY profiles directory: %w", err)
	}
	return &Bridge{
		cfg:      cfg,
		active:   make(map[string]struct{}),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}, nil
}

func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", b.auth(b.handleHealth))
	mux.HandleFunc("GET /v1/profiles/{profile}/status", b.auth(b.handleStatus))
	mux.HandleFunc("GET /v1/profiles/{profile}/models", b.auth(b.handleModels))
	mux.HandleFunc("POST /v1/profiles/{profile}/chat/completions", b.auth(b.handleChat))
	mux.HandleFunc("GET /v1/profiles/{profile}/terminal", b.auth(b.handleTerminal))
	return mux
}

func (b *Bridge) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(b.cfg.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (b *Bridge) profile(r *http.Request) (string, bool) {
	profile := r.PathValue("profile")
	return profile, profileRE.MatchString(profile)
}

func (b *Bridge) profileHome(profile string) string { return filepath.Join(b.cfg.ProfilesDir, profile) }

func (b *Bridge) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (b *Bridge) handleStatus(w http.ResponseWriter, r *http.Request) {
	profile, ok := b.profile(r)
	if !ok {
		http.Error(w, "invalid profile", http.StatusBadRequest)
		return
	}
	_, err := os.Stat(filepath.Join(b.profileHome(profile), ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "authenticated": err == nil, "model": defaultModel})
}

func (b *Bridge) handleModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := b.profile(r); !ok {
		http.Error(w, "invalid profile", http.StatusBadRequest)
		return
	}
	// AGY does not expose stable model IDs in non-interactive mode. The selected
	// model is managed through AGY's /model terminal command; default preserves it.
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]any{{"id": defaultModel, "object": "model", "owned_by": "antigravity-cli"}}})
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (b *Bridge) handleChat(w http.ResponseWriter, r *http.Request) {
	profile, ok := b.profile(r)
	if !ok {
		http.Error(w, "invalid profile", http.StatusBadRequest)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 310*time.Second)
	defer cancel()
	response, usage, err := b.runAGY(ctx, profile, req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"message": err.Error(), "type": "antigravity_host_error"}})
		return
	}
	id := fmt.Sprintf("agy-%d", time.Now().UnixNano())
	if req.Stream {
		b.writeStream(w, id, nonEmpty(req.Model, defaultModel), response)
		return
	}
	out := map[string]any{"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": nonEmpty(req.Model, defaultModel), "choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": response}, "finish_reason": "stop"}}, "usage": usage}
	writeJSON(w, http.StatusOK, out)
}

func (b *Bridge) writeStream(w http.ResponseWriter, id, model, response string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "delta": map[string]string{"content": response}, "finish_reason": nil}}}
	data, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(w, "data: %s\\n\\n", data)
	final := map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}}
	data, _ = json.Marshal(final)
	_, _ = fmt.Fprintf(w, "data: %s\\n\\n", data)
	_, _ = io.WriteString(w, "data: [DONE]\\n\\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (b *Bridge) runAGY(ctx context.Context, profile string, req chatRequest) (string, map[string]int, error) {
	workspace, err := os.MkdirTemp("", "goclaw-agy-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(workspace)
	prompt, err := buildPrompt(req.Messages, workspace)
	if err != nil {
		return "", nil, err
	}
	cmd := exec.CommandContext(ctx, b.cfg.AGYPath, "-p", prompt, "--output-format", "json", "--print-timeout", "5m")
	if req.Model != "" && req.Model != defaultModel {
		cmd.Args = append(cmd.Args, "--model", req.Model)
	}
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "HOME="+b.profileHome(profile), "TERM=xterm-256color")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("AGY exited: %s", strings.TrimSpace(string(output)))
	}
	var result struct {
		Status, Response, Error string
		Usage                   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", nil, fmt.Errorf("invalid AGY response: %w", err)
	}
	if result.Status != "SUCCESS" || strings.TrimSpace(result.Response) == "" {
		return "", nil, errors.New(nonEmpty(result.Error, "AGY returned empty content"))
	}
	return result.Response, map[string]int{"prompt_tokens": result.Usage.InputTokens, "completion_tokens": result.Usage.OutputTokens, "total_tokens": result.Usage.TotalTokens}, nil
}

func buildPrompt(messages []message, workspace string) (string, error) {
	parts := []string{"Follow the system instructions and answer the latest user request."}
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			lastUser = i
			break
		}
	}
	images := 0
	for i, msg := range messages {
		var text string
		if json.Unmarshal(msg.Content, &text) == nil {
			parts = append(parts, "\n["+strings.ToUpper(msg.Role)+"]\n"+text)
			continue
		}
		var items []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(msg.Content, &items); err != nil {
			continue
		}
		for _, item := range items {
			if item.Type == "text" {
				parts = append(parts, "\n["+strings.ToUpper(msg.Role)+"]\n"+item.Text)
				continue
			}
			if item.Type != "image_url" || i != lastUser || !strings.HasPrefix(item.ImageURL.URL, "data:image/") {
				continue
			}
			comma := strings.IndexByte(item.ImageURL.URL, ',')
			if comma < 0 {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(item.ImageURL.URL[comma+1:])
			if err != nil {
				return "", fmt.Errorf("decode image: %w", err)
			}
			images++
			path := filepath.Join(workspace, fmt.Sprintf("latest-image-%d.png", images))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return "", err
			}
			parts = append(parts, fmt.Sprintf("\n[LATEST USER IMAGE %d]\nAttached image: %s", images, path))
		}
	}
	if images > 0 {
		parts = append(parts, "\nAnalyze only the attached image(s) from the latest user turn before answering.")
	}
	return strings.Join(parts, "\n"), nil
}

func (b *Bridge) handleTerminal(w http.ResponseWriter, r *http.Request) {
	profile, ok := b.profile(r)
	if !ok {
		http.Error(w, "invalid profile", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	if _, busy := b.active[profile]; busy {
		b.mu.Unlock()
		http.Error(w, "terminal already active", http.StatusConflict)
		return
	}
	b.active[profile] = struct{}{}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.active, profile); b.mu.Unlock() }()
	if err := os.MkdirAll(b.profileHome(profile), 0o700); err != nil {
		http.Error(w, "create profile", http.StatusInternalServerError)
		return
	}
	ws, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	cmd := exec.Command(b.cfg.AGYPath)
	cmd.Dir = b.profileHome(profile)
	cmd.Env = append(os.Environ(), "HOME="+b.profileHome(profile), "TERM=xterm-256color", "LANG=C.UTF-8")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 110})
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\nUnable to start AGY: "+err.Error()+"\r\n"))
		return
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, e := ptmx.Read(buffer)
			if n > 0 {
				if ws.WriteMessage(websocket.BinaryMessage, buffer[:n]) != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if typ == websocket.BinaryMessage || typ == websocket.TextMessage {
			if _, err := ptmx.Write(data); err != nil {
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
