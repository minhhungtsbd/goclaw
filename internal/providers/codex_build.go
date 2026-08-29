package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// invalidFcIDChars matches characters not allowed in Responses API tool call IDs.
var invalidFcIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// buildRequestBody converts internal ChatRequest to Responses API format.
func (p *CodexProvider) buildRequestBody(req ChatRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var instructions string
	var input []any
	seenCallIDs := make(map[string]bool)
	callIDQueues := make(map[string][]string)

	for messageIndex, m := range req.Messages {
		switch m.Role {
		case "system":
			if instructions == "" {
				instructions = m.Content
			} else {
				instructions += "\n\n" + m.Content
			}

		case "user":
			if len(m.Images) > 0 || len(m.Videos) > 0 {
				var parts []map[string]any
				for _, img := range m.Images {
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data),
					})
				}
				// Videos are not supported by Codex, they are omitted here.
				if m.Content != "" {
					parts = append(parts, map[string]any{
						"type": "input_text",
						"text": m.Content,
					})
				}
				input = append(input, map[string]any{
					"role":    "user",
					"content": parts,
				})
			} else {
				input = append(input, map[string]any{
					"role":    "user",
					"content": m.Content,
				})
			}

		case "assistant":
			if len(m.ToolCalls) > 0 {
				for callIndex, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					callID := toFcID(tc.ID)
					if seenCallIDs[callID] {
						callID = uniqueFcID(tc.ID, messageIndex, callIndex, seenCallIDs)
					}
					seenCallIDs[callID] = true
					callIDQueues[tc.ID] = append(callIDQueues[tc.ID], callID)
					input = append(input, map[string]any{
						"type":      "function_call",
						"id":        callID,
						"call_id":   callID,
						"name":      tc.Name,
						"arguments": string(argsJSON),
					})
				}
			}
			if m.Content != "" {
				item := map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": m.Content},
					},
				}
				if m.Phase != "" {
					item["phase"] = m.Phase
				}
				input = append(input, item)
			}

		case "tool":
			callID := toFcID(m.ToolCallID)
			if queue := callIDQueues[m.ToolCallID]; len(queue) > 0 {
				callID = queue[0]
				callIDQueues[m.ToolCallID] = queue[1:]
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  m.Content,
			})
		}
	}

	body := map[string]any{
		"model":  model,
		"input":  input,
		"stream": stream,
		"store":  false,
	}

	if instructions == "" {
		instructions = "You are a helpful assistant."
	}
	body["instructions"] = instructions

	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			if t.Type == "image_generation" {
				// Pass native image_generation tool object as-is — Responses API first-class tool.
				// Defaults chosen for Phase 1b; per-agent overrides are Phase 4.
				tools = append(tools, map[string]any{
					"type":           "image_generation",
					"action":         "generate",
					"model":          "gpt-image-2",
					"output_format":  "png",
					"partial_images": 1,
				})
			} else if t.Function != nil {
				// Function tool path (default). Works with both value-type and pointer Function
				// fields — we only access t.Function when type is not "image_generation".
				tools = append(tools, map[string]any{
					"type":        "function",
					"name":        t.Function.Name,
					"description": t.Function.Description,
					"parameters":  NormalizeSchema("codex", t.Function.Parameters),
				})
			}
		}
		body["tools"] = tools
	}
	if choice, ok := req.Options[OptToolChoice]; ok && choice != nil {
		body["tool_choice"] = choice
	}

	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		body["reasoning"] = map[string]any{"effort": level}
	}

	// Prompt caching params (prompt_cache_key, prompt_cache_retention) are accepted
	// only by native OpenAI endpoints. The ChatGPT subscription OAuth backend
	// (chatgpt.com/backend-api) rejects them with HTTP 400, so gate on the endpoint —
	// the same native-only policy CacheMiddleware already applies. Server-side prefix
	// caching still works on the OAuth backend without these params.
	if isOpenAINativeEndpoint(p.apiBase) {
		if cacheKey, ok := req.Options[OptPromptCacheKey]; ok {
			body["prompt_cache_key"] = cacheKey
		}
		if retention, ok := req.Options[OptPromptCacheRetention]; ok {
			body["prompt_cache_retention"] = retention
		}
	}

	return body
}

func (p *CodexProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	endpoint := p.apiBase + "/codex/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: get auth token: %w", p.name, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("OpenAI-Beta", "responses=v1")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{
			Status:     resp.StatusCode,
			Body:       fmt.Sprintf("%s: %s", p.name, string(respBody)),
			RetryAfter: retryAfter,
		}
	}

	return resp.Body, nil
}

func uniqueFcID(original string, messageIndex, callIndex int, seen map[string]bool) string {
	for salt := 0; ; salt++ {
		raw := fmt.Sprintf("%s:%d:%d:%d", original, messageIndex, callIndex, salt)
		hash := sha256.Sum256([]byte(raw))
		candidate := "fc_" + hex.EncodeToString(hash[:])[:37]
		if !seen[candidate] {
			return candidate
		}
	}
}

// toFcID ensures a tool call ID starts with "fc_" and contains only
// letters, numbers, underscores, or dashes as required by the Responses API.
func toFcID(id string) string {
	original := id
	if strings.HasPrefix(id, "tool_") {
		id = id[len("tool_"):]
	} else if strings.HasPrefix(id, "call_") {
		id = id[len("call_"):]
	} else if strings.HasPrefix(id, "fc_") {
		id = id[len("fc_"):]
	}
	// Replace invalid characters (e.g. colons from session keys) with underscores.
	id = invalidFcIDChars.ReplaceAllString(id, "_")
	// ChatGPT's Responses endpoint rejects input item IDs over 64 characters.
	// Keep IDs at 40 characters to match the strict transcript invariant and
	// hash instead of truncating so distinct calls cannot collapse together.
	if id == "" || len(id) > 37 {
		hash := sha256.Sum256([]byte(original))
		id = hex.EncodeToString(hash[:])[:37]
	}
	return "fc_" + id
}
