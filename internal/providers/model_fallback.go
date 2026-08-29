package providers

import (
	"context"
	"fmt"
	"strings"
)

// FallbackCandidate is one runtime provider/model fallback option.
type FallbackCandidate struct {
	ProviderName string
	Model        string
	Provider     Provider
}

// ModelFallbackProvider wraps a primary provider with ordered fallback
// provider/model candidates. The primary candidate is always tried first.
type ModelFallbackProvider struct {
	primary     FallbackCandidate
	fallbacks   []FallbackCandidate
	classifier  ErrorClassifier
	tracker     *CooldownTracker
	maxAttempts int
}

type FallbackCallInfo struct {
	Streamed bool
}

type FallbackAfterCall func(*ChatResponse, error, FallbackCallInfo)
type FallbackBeforeCall func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (after FallbackAfterCall, err error)

func NewModelFallbackProvider(primary FallbackCandidate, fallbacks []FallbackCandidate, maxAttempts int, cooldownEnabled bool) *ModelFallbackProvider {
	var tracker *CooldownTracker
	if cooldownEnabled {
		tracker = NewCooldownTracker(0)
	}
	return &ModelFallbackProvider{
		primary:     primary,
		fallbacks:   fallbacks,
		classifier:  NewDefaultClassifier(),
		tracker:     tracker,
		maxAttempts: maxAttempts,
	}
}

func (p *ModelFallbackProvider) PrimaryProvider() Provider {
	return p.primary.Provider
}

func (p *ModelFallbackProvider) Name() string {
	if p.primary.Provider != nil {
		return p.primary.Provider.Name()
	}
	return p.primary.ProviderName
}

func (p *ModelFallbackProvider) DefaultModel() string {
	if p.primary.Model != "" {
		return p.primary.Model
	}
	if p.primary.Provider != nil {
		return p.primary.Provider.DefaultModel()
	}
	return ""
}

// Capabilities reports the union of all usable candidates. Per-candidate
// compatibility is still enforced immediately before each transport call.
func (p *ModelFallbackProvider) Capabilities() ProviderCapabilities {
	var out ProviderCapabilities
	for _, entry := range append([]FallbackCandidate{p.primary}, p.fallbacks...) {
		aware, ok := entry.Provider.(CapabilitiesAware)
		if !ok || entry.Provider == nil {
			continue
		}
		caps := aware.Capabilities()
		out.Streaming = out.Streaming || caps.Streaming
		out.ToolCalling = out.ToolCalling || caps.ToolCalling
		out.StreamWithTools = out.StreamWithTools || caps.StreamWithTools
		out.Thinking = out.Thinking || caps.Thinking
		out.Vision = out.Vision || caps.Vision
		out.CacheControl = out.CacheControl || caps.CacheControl
		out.ImageGeneration = out.ImageGeneration || caps.ImageGeneration
		if out.MaxContextWindow == 0 || (caps.MaxContextWindow > 0 && caps.MaxContextWindow < out.MaxContextWindow) {
			out.MaxContextWindow = caps.MaxContextWindow
		}
		if out.TokenizerID == "" {
			out.TokenizerID = caps.TokenizerID
		}
	}
	return out
}

func (p *ModelFallbackProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		return callFallbackCandidate(ctx, entry, nextReq, false, nil, func() (*ChatResponse, error) {
			return entry.Provider.Chat(ctx, nextReq)
		})
	})
}

func (p *ModelFallbackProvider) ChatWithHook(ctx context.Context, req ChatRequest, before FallbackBeforeCall) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		after, err := before(ctx, entry, nextReq)
		if err != nil {
			return nil, err
		}
		return callFallbackCandidate(ctx, entry, nextReq, false, after, func() (*ChatResponse, error) {
			return entry.Provider.Chat(ctx, nextReq)
		})
	})
}

func (p *ModelFallbackProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		streamed := false
		resp, err := callFallbackCandidate(ctx, entry, nextReq, true, nil, func() (*ChatResponse, error) {
			return entry.Provider.ChatStream(ctx, nextReq, func(chunk StreamChunk) {
				if chunk.Content != "" || chunk.Thinking != "" || len(chunk.Images) > 0 {
					streamed = true
				}
				onChunk(chunk)
			})
		})
		if streamed && err != nil {
			return nil, noFallbackAfterStreamError{err: err}
		}
		return resp, err
	})
}

func (p *ModelFallbackProvider) ChatStreamWithHook(ctx context.Context, req ChatRequest, onChunk func(StreamChunk), before FallbackBeforeCall) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		after, err := before(ctx, entry, nextReq)
		if err != nil {
			return nil, err
		}
		streamed := false
		resp, err := callFallbackCandidate(ctx, entry, nextReq, true, func(resp *ChatResponse, err error, _ FallbackCallInfo) {
			if after != nil {
				after(resp, err, FallbackCallInfo{Streamed: streamed})
			}
		}, func() (*ChatResponse, error) {
			return entry.Provider.ChatStream(ctx, nextReq, func(chunk StreamChunk) {
				if chunk.Content != "" || chunk.Thinking != "" || len(chunk.Images) > 0 {
					streamed = true
				}
				onChunk(chunk)
			})
		})
		if streamed && err != nil {
			return nil, noFallbackAfterStreamError{err: err}
		}
		return resp, err
	})
}

// FallbackContractError identifies a provider/model that cannot safely satisfy
// the current request or returned a structurally invalid success response.
type FallbackContractError struct {
	Reason   FailoverReason
	Provider string
	Model    string
	Detail   string
}

func (e *FallbackContractError) Error() string {
	return fmt.Sprintf("fallback candidate %s/%s: %s", e.Provider, e.Model, e.Detail)
}

func callFallbackCandidate(
	ctx context.Context,
	entry FallbackCandidate,
	req ChatRequest,
	stream bool,
	after FallbackAfterCall,
	call func() (*ChatResponse, error),
) (*ChatResponse, error) {
	if err := validateFallbackCandidate(entry, req, stream); err != nil {
		if after != nil {
			after(nil, err, FallbackCallInfo{})
		}
		return nil, err
	}
	resp, err := call()
	if err == nil {
		err = validateFallbackResponse(entry, req, resp)
	}
	if after != nil {
		after(resp, err, FallbackCallInfo{})
	}
	return resp, err
}

func validateFallbackCandidate(entry FallbackCandidate, req ChatRequest, stream bool) error {
	contractErr := func(detail string) error {
		return &FallbackContractError{
			Reason: FailoverCapability, Provider: entry.ProviderName, Model: entry.Model, Detail: detail,
		}
	}
	if entry.Provider == nil {
		return contractErr("provider is unavailable")
	}
	needsTools := len(req.Tools) > 0
	needsVision := requestContainsVisualMedia(req)
	if !needsTools && !needsVision && !stream {
		return nil
	}
	aware, ok := entry.Provider.(CapabilitiesAware)
	if !ok {
		return contractErr("provider does not declare capabilities required by this request")
	}
	caps := aware.Capabilities()
	if needsTools && !caps.ToolCalling {
		return contractErr("tool calling is not supported")
	}
	if needsVision && !caps.Vision {
		return contractErr("vision input is not supported")
	}
	if stream && !caps.Streaming {
		return contractErr("streaming is not supported")
	}
	if stream && needsTools && !caps.StreamWithTools {
		return contractErr("streaming with tool calls is not supported")
	}
	return nil
}

func validateFallbackResponse(entry FallbackCandidate, req ChatRequest, resp *ChatResponse) error {
	invalid := func(detail string) error {
		return &FallbackContractError{
			Reason: FailoverInvalidOutput, Provider: entry.ProviderName, Model: entry.Model, Detail: detail,
		}
	}
	if resp == nil {
		return invalid("provider returned a nil response without an error")
	}
	if strings.TrimSpace(resp.Content) == "" && strings.TrimSpace(resp.Thinking) == "" && len(resp.ToolCalls) == 0 && len(resp.Images) == 0 {
		return invalid("provider returned an empty response")
	}
	if isOrchestrationPlaceholder(resp.Content) && len(resp.ToolCalls) == 0 && len(resp.Images) == 0 {
		return invalid("provider returned an orchestration placeholder instead of an answer")
	}

	allowedTools := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		if tool.Function != nil && tool.Function.Name != "" {
			allowedTools[tool.Function.Name] = struct{}{}
		}
	}
	seenIDs := make(map[string]struct{}, len(resp.ToolCalls))
	for _, toolCall := range resp.ToolCalls {
		if strings.TrimSpace(toolCall.ID) == "" {
			return invalid("provider returned a tool call without an id")
		}
		if _, exists := seenIDs[toolCall.ID]; exists {
			return invalid("provider returned duplicate tool call ids")
		}
		seenIDs[toolCall.ID] = struct{}{}
		if _, ok := allowedTools[toolCall.Name]; !ok {
			return invalid("provider called an unavailable tool: " + toolCall.Name)
		}
		if toolCall.ParseError != "" {
			return invalid("provider returned malformed arguments for tool: " + toolCall.Name)
		}
	}

	if requiredToolName, required := requiredToolChoice(req.Options); required {
		if len(resp.ToolCalls) == 0 {
			return invalid("tool_choice requires a tool call, but the provider returned none")
		}
		if requiredToolName != "" {
			for _, toolCall := range resp.ToolCalls {
				if toolCall.Name == requiredToolName {
					return nil
				}
			}
			return invalid("provider did not call the explicitly required tool: " + requiredToolName)
		}
	}
	return nil
}

func isOrchestrationPlaceholder(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	normalized = strings.TrimSuffix(normalized, ".")
	switch normalized {
	case "got it, i'll incorporate that into what i'm working on":
		return true
	default:
		return false
	}
}

func requestContainsVisualMedia(req ChatRequest) bool {
	for _, msg := range req.Messages {
		if len(msg.Images) > 0 || len(msg.Videos) > 0 {
			return true
		}
	}
	return false
}

func requiredToolChoice(options map[string]any) (string, bool) {
	choice, ok := options[OptToolChoice]
	if !ok || choice == nil {
		return "", false
	}
	switch value := choice.(type) {
	case string:
		return "", strings.EqualFold(strings.TrimSpace(value), "required")
	case map[string]any:
		name, _ := value["name"].(string)
		if function, ok := value["function"].(map[string]any); ok {
			if nestedName, ok := function["name"].(string); ok {
				name = nestedName
			}
		}
		kind, _ := value["type"].(string)
		return strings.TrimSpace(name), strings.TrimSpace(name) != "" || strings.EqualFold(kind, "required")
	default:
		return "", false
	}
}

func (p *ModelFallbackProvider) runOrdered(
	ctx context.Context,
	req ChatRequest,
	call func(context.Context, FallbackCandidate, ChatRequest) (*ChatResponse, error),
) (*ChatResponse, error) {
	candidates := p.orderedCandidates(req.Model)
	var attempts []FailoverAttempt
	transportAttempts := 0
	for _, entry := range candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if p.maxAttempts > 0 && transportAttempts >= p.maxAttempts {
			break
		}
		key := CooldownKey(entry.ProviderName, entry.Model)
		if p.tracker != nil && !p.tracker.IsAvailable(key) && !p.tracker.ShouldProbe(key) {
			continue
		}
		resp, err := call(ctx, entry, req)
		if err == nil {
			if p.tracker != nil {
				p.tracker.RecordSuccess(key)
			}
			return resp, nil
		}
		if streamErr, ok := err.(noFallbackAfterStreamError); ok {
			return nil, streamErr.err
		}
		classification := ClassifyHTTPError(p.classifier, err)
		if classification.Reason != FailoverCapability {
			transportAttempts++
		}
		attempts = append(attempts, FailoverAttempt{
			Candidate:      ModelCandidate{Provider: entry.ProviderName, Model: entry.Model, ProfileID: entry.ProviderName + "/" + entry.Model},
			Classification: classification,
			Err:            err,
		})
		// Capability mismatches are request-specific. Do not globally cool down a
		// text-capable route just because one request required tools or vision.
		if p.tracker != nil && classification.Kind == "reason" && classification.Reason != FailoverCapability {
			p.tracker.RecordFailure(key, classification.Reason)
		}
		if classification.Kind == "context_overflow" || classification.Reason == FailoverUnknown {
			return nil, err
		}
	}
	return nil, &FailoverSummaryError{Attempts: attempts}
}

func (p *ModelFallbackProvider) orderedCandidates(requestModel string) []FallbackCandidate {
	primary := p.primary
	if requestModel != "" {
		primary.Model = requestModel
	}
	out := []FallbackCandidate{primary}
	for _, fallback := range p.fallbacks {
		if fallback.Provider == nil || fallback.ProviderName == "" || fallback.Model == "" {
			continue
		}
		if fallback.ProviderName == primary.ProviderName && fallback.Model == primary.Model {
			continue
		}
		out = append(out, fallback)
	}
	return out
}

type noFallbackAfterStreamError struct {
	err error
}

func (e noFallbackAfterStreamError) Error() string {
	return e.err.Error()
}
