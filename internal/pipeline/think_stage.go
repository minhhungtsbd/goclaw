package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	maxTruncRetries            = 3
	responseSafetyRetryTimeout = 90 * time.Second
)

// ThinkStage runs per iteration. Calls LLM, handles truncation retries,
// accumulates usage, returns BreakLoop when response has no tool calls.
type ThinkStage struct {
	deps   *PipelineDeps
	result StageResult
}

// NewThinkStage creates a ThinkStage.
func NewThinkStage(deps *PipelineDeps) *ThinkStage {
	return &ThinkStage{deps: deps, result: Continue}
}

func (s *ThinkStage) Name() string        { return "think" }
func (s *ThinkStage) Result() StageResult { return s.result }

// Execute builds tools, calls LLM, handles truncation, sets flow control.
func (s *ThinkStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	// 1. Iteration budget nudges (70% / 90%)
	s.maybeInjectNudge(state)

	// 2. Build filtered tool definitions
	var toolDefs []providers.ToolDefinition
	if s.deps.BuildFilteredTools != nil {
		var err error
		toolDefs, err = s.deps.BuildFilteredTools(state)
		if err != nil {
			return fmt.Errorf("build tools: %w", err)
		}
		allowed := make(map[string]bool, len(toolDefs))
		for _, td := range toolDefs {
			if td.Function != nil {
				allowed[td.Function.Name] = true
			}
		}
		state.Tool.AllowedTools = allowed
	} else {
		state.Tool.AllowedTools = nil
	}

	// 3. Construct the final ChatRequest and enforce the request-level
	// context budget before any provider call is attempted.
	req, estimate, err := s.prepareFinalRequest(ctx, state, toolDefs)
	if err != nil {
		return err
	}
	// Configured exception emails bypass automated Cloudmini checks. Restrict
	// the very first model turn to a required Admin handoff so the request does
	// not spend an extra text/retry turn before creating the real ticket.
	if cloudminiNeedsConfiguredEmailAdminReview(state) && strings.TrimSpace(state.Tool.AdminHandoffTicket) == "" {
		if handoffTools := onlyToolDefinition(req.Tools, "escalate_to_admin"); len(handoffTools) > 0 {
			req.Tools = handoffTools
			req.Options = cloneRequestOptions(req.Options)
			req.Options[providers.OptToolChoice] = "required"
		} else {
			// Fail closed: without escalate_to_admin the mandatory handoff cannot
			// happen, so no LLM call and no other tool may answer the routed
			// request instead. End with a deterministic reply and surface the
			// agent configuration error.
			slog.Error("cloudmini configured email handoff blocked: escalate_to_admin unavailable",
				"run_id", state.RunID, "iteration", state.Iteration)
			state.Think.LastResponse = &providers.ChatResponse{
				Content:      cloudminiSafeGuardResponse(state),
				FinishReason: "stop",
			}
			s.result = BreakLoop
			return nil
		}
	}

	// 4. Call LLM (stream or sync — delegated to callback)
	if s.deps.CallLLM == nil {
		return fmt.Errorf("CallLLM callback not configured")
	}
	resp, err := s.deps.CallLLM(ctx, state, req)
	if err != nil {
		// The Admin handoff already succeeded, so a provider error/cancellation
		// must not turn the customer-visible result into an empty response. Finish
		// locally with the real ticket and let Observe/Finalize persist it.
		if adminHandoffCustomerReplyPending(state) {
			state.Think.LastResponse = &providers.ChatResponse{
				Content:      adminHandoffCustomerConfirmation(state.Tool.AdminHandoffTicket),
				FinishReason: "stop",
			}
			s.result = BreakLoop
			return nil
		}
		// Central hard-ceiling guard rejected the concrete request AFTER the
		// callback appended its final directive/retry/reasoning mutations (which
		// prepareFinalRequest could not see). Re-enter the full reduction chain
		// against the current messages, rebuild, and retry this iteration. Only
		// when reduction cannot bring the request under the ceiling do we abort —
		// the guard already guaranteed zero transport calls were made.
		if isRequestBudgetExceededErr(err) {
			if state.Think.OverflowRetries >= maxBudgetReductionRetries {
				return fmt.Errorf("request context budget exceeded after reduction: %w", err)
			}
			if s.reduceForBudgetExceeded(ctx, state) {
				// This LLM call produced no response, so LastResponse still holds
				// the PRIOR iteration's response. Clear it before returning Continue
				// so ToolStage/ObserveStage in this same iteration do not re-execute
				// the previous iteration's tool calls. The retry rebuilds and calls
				// the model again on the next iteration.
				state.Think.LastResponse = nil
				return nil // Retry this iteration (Continue result) with reduced context.
			}
			return fmt.Errorf("request context budget exceeded, reduction exhausted: %w", err)
		}
		// Issue 958: Check for context overflow — attempt emergency compaction + retry
		if isContextOverflowErr(err) {
			if state.Think.OverflowRetries > 0 {
				return fmt.Errorf("context overflow after compaction: %w", err)
			}
			if s.tryEmergencyCompaction(ctx, state, "context_overflow_error") {
				// Same stale-response hazard as the budget path: no response was
				// produced this iteration, so drop the prior one before retrying.
				state.Think.LastResponse = nil
				return nil // Retry this iteration (Continue result)
			}
		}
		return fmt.Errorf("llm call: %w", err)
	}

	// 5. Accumulate usage across turns and retain the usage snapshot for the
	// request that was just sent. AddUsage preserves provider telemetry fields.
	if resp.Usage != nil {
		// Log actual (post-call) usage vs the pre-call budget estimate (my guard
		// observability), then accumulate the run-cumulative total.
		s.logFinalRequestActual(ctx, state, estimate, *resp.Usage)
		AddUsage(&state.Think.TotalUsage, *resp.Usage)
		// Snapshot per-call usage: the LAST iteration's prompt size IS the
		// session's current context. Keep the previous snapshot when a response
		// carries no prompt tokens (e.g. providers that omit usage on some turns)
		// — matches upstream 503909d3 behaviour, which never wipes a usable
		// snapshot with an empty final response.
		if resp.Usage.PromptTokens > 0 {
			state.Think.LastUsage = *resp.Usage
		}
	}
	if adminHandoffCustomerReplyPending(state) && len(resp.ToolCalls) == 0 && strings.TrimSpace(resp.Content) == "" {
		resp = &providers.ChatResponse{
			Content:      adminHandoffCustomerConfirmation(state.Tool.AdminHandoffTicket),
			FinishReason: "stop",
		}
	}
	if len(resp.ToolCalls) == 0 && cloudminiNeedsEmailMismatchAdminReview(state) &&
		strings.TrimSpace(state.Tool.AdminHandoffTicket) != "" {
		resp.Content = cloudminiEmailMismatchReply(state, state.Tool.AdminHandoffTicket)
	}
	// A matched operational incident is structured, operator-authored data. If
	// the model omits or rewrites it unsafely, produce the grounded response
	// locally instead of spending another LLM call and risking a generic fallback
	// that contradicts service_info facts already verified in this run.
	if len(resp.ToolCalls) == 0 && cloudminiResponseViolatesGuard(state, resp.Content) {
		if deterministic, ok := cloudminiOperationalIncidentResponse(state); ok {
			resp.Content = deterministic
			resp.FinishReason = "stop"
		}
	}

	// A model may cite a historical ticket only after the read-only status tool
	// verifies it for this exact customer route. A pending historical ticket also
	// blocks creation of a duplicate handoff.
	mentionedTicket := adminHandoffTicketNeedingCheck(state, resp.Content)
	if mentionedTicket == "" {
		mentionedTicket = adminHandoffMentionedTicket(resp.Content)
	}
	requiresStatusCheck := len(resp.ToolCalls) == 0 && adminHandoffStatusNeedsCheck(state, resp.Content)
	unsupportedClaim := len(resp.ToolCalls) == 0 && unsupportedAdminHandoffClaim(resp.Content, state)
	cloudminiGuardViolation := len(resp.ToolCalls) == 0 && cloudminiResponseViolatesGuard(state, resp.Content)
	adminHandoffReplyViolation := len(resp.ToolCalls) == 0 && adminHandoffResponseViolatesGuard(state, resp.Content)
	if len(resp.ToolCalls) == 0 && (requiresStatusCheck || unsupportedClaim || cloudminiGuardViolation || adminHandoffReplyViolation) {
		state.Think.HandoffClaimRetries++
		retryReq := req
		var retryResp *providers.ChatResponse
		var retryErr error
		canRetry := true
		if requiresStatusCheck {
			retryReq.Tools = onlyToolDefinition(req.Tools, "admin_handoff_status")
			if len(retryReq.Tools) == 0 {
				canRetry = false
				retryErr = fmt.Errorf("required safety tool admin_handoff_status is unavailable")
			}
			retryReq.Options = cloneRequestOptions(req.Options)
			retryReq.Options[providers.OptToolChoice] = "required"
		}
		requiresCloudminiAdminReview := (cloudminiNeedsEmailMismatchAdminReview(state) ||
			cloudminiNeedsConfiguredEmailAdminReview(state)) &&
			strings.TrimSpace(state.Tool.AdminHandoffTicket) == ""
		if requiresCloudminiAdminReview {
			retryReq.Tools = onlyToolDefinition(req.Tools, "escalate_to_admin")
			if len(retryReq.Tools) == 0 {
				canRetry = false
				retryErr = fmt.Errorf("required safety tool escalate_to_admin is unavailable")
			}
			retryReq.Options = cloneRequestOptions(req.Options)
			retryReq.Options[providers.OptToolChoice] = "required"
		}
		retryReq.Messages = append(append([]providers.Message(nil), req.Messages...), providers.Message{
			Role: "system",
			Content: "[CLOUDMINI RESPONSE SAFETY CHECK] " + cloudminiResponseGuardInstruction(state) + "\n\n" +
				adminHandoffTruthInstruction(state, mentionedTicket),
		})
		if canRetry {
			retryCtx, cancel := context.WithTimeout(ctx, responseSafetyRetryTimeout)
			retryResp, retryErr = s.deps.CallLLM(retryCtx, state, retryReq)
			cancel()
		}
		if retryErr == nil && retryResp != nil {
			if retryResp.Usage != nil {
				AddUsage(&state.Think.TotalUsage, *retryResp.Usage)
				if retryResp.Usage.PromptTokens > 0 {
					state.Think.LastUsage = *retryResp.Usage
				}
			}
			resp = retryResp
		}
		if retryErr != nil || resp == nil || len(resp.ToolCalls) == 0 &&
			(requiresStatusCheck || unsupportedAdminHandoffClaim(resp.Content, state) || cloudminiResponseViolatesGuard(state, resp.Content) || adminHandoffResponseViolatesGuard(state, resp.Content)) {
			fallback := adminHandoffStatusFallback(state, mentionedTicket)
			if mentionedTicket == "" && (cloudminiGuardViolation || adminHandoffReplyViolation || (resp != nil && cloudminiResponseViolatesGuard(state, resp.Content)) || (resp != nil && adminHandoffResponseViolatesGuard(state, resp.Content))) {
				fallback = cloudminiSafeGuardResponse(state)
			}
			resp = &providers.ChatResponse{
				Content:      fallback,
				FinishReason: "stop",
			}
		}
	}

	if isEmptyLengthResponse(resp) {
		if state.Think.OverflowRetries > 0 {
			return fmt.Errorf("llm response truncated before content after compaction")
		}
		if s.tryEmergencyCompaction(ctx, state, "empty_length_response") {
			// LastResponse has NOT yet been updated to this empty response, so it
			// still holds the prior iteration's result. Clear it so downstream
			// stages this iteration don't act on the stale response during retry.
			state.Think.LastResponse = nil
			return nil // Retry next iteration with compacted history.
		}
		return fmt.Errorf("llm response truncated before content")
	}

	state.Think.LastResponse = resp

	// 6. Handle truncation: retry when tool call args are truncated or malformed.
	// Gemini returns finish_reason="tool_calls" (not "length") even when the thinking
	// budget exhausted max_tokens before args could be emitted — detect via empty
	// args on allowlisted mutating tools. Nullary tools (datetime, heartbeat) skip
	// the heuristic so their legitimate empty-args calls pass through.
	// Text-only truncation (no tool calls) is a valid long answer — deliver it.
	truncated := len(resp.ToolCalls) > 0 && (resp.FinishReason == "length" ||
		(resp.FinishReason == "tool_calls" && toolCallsHaveMissingRequiredArgs(resp.ToolCalls)))
	parseErr := !truncated && toolCallsHaveParseErrors(resp.ToolCalls)
	if truncated || parseErr {
		state.Think.TruncRetries++
		if state.Think.TruncRetries >= maxTruncRetries {
			s.result = AbortRun
			return nil
		}
		hint := "[System] Your output was truncated because it exceeded max_tokens. Your tool call arguments were incomplete. Please retry with shorter content — split large writes into multiple smaller calls."
		if parseErr {
			hint = "[System] One or more tool call arguments were malformed (truncated JSON). Please retry with shorter content."
		}
		state.Messages.AppendPending(providers.Message{Role: "assistant", Content: resp.Content})
		state.Messages.AppendPending(providers.Message{Role: "user", Content: hint})
		return nil // Continue to next iteration for retry
	}
	state.Think.TruncRetries = 0    // reset on success
	state.Think.OverflowRetries = 0 // reset on success

	// 7. Uniquify tool call IDs (OpenAI returns 400 on duplicates across iterations).
	// Skip if raw content present (Anthropic thinking passback) to avoid desync.
	if len(resp.ToolCalls) > 0 && resp.RawAssistantContent == nil && s.deps.UniqueToolCallIDs != nil {
		resp.ToolCalls = s.deps.UniqueToolCallIDs(resp.ToolCalls, state.RunID, state.Iteration)
	}

	// 8. Flow control + message append.
	// Final answer (no tool calls): FinalizeStage builds the definitive assistant
	// message with sanitization + MediaRefs, so skip AppendPending here to avoid
	// a duplicate. Matches v2 behavior where loop breaks before appending.
	if len(resp.ToolCalls) == 0 {
		s.result = BreakLoop
		return nil
	}

	// Tool iteration: append assistant message for LLM context continuity.
	assistantMsg := providers.Message{
		Role:                "assistant",
		Content:             resp.Content,
		Thinking:            resp.Thinking,
		ToolCalls:           resp.ToolCalls,
		Phase:               resp.Phase,               // Codex phase metadata
		RawAssistantContent: resp.RawAssistantContent, // Anthropic thinking blocks passback
	}
	state.Messages.AppendPending(assistantMsg)

	s.emitToolIterationBlockReply(state, resp)

	return nil
}

func onlyToolDefinition(definitions []providers.ToolDefinition, name string) []providers.ToolDefinition {
	for _, definition := range definitions {
		if definition.Function != nil && definition.Function.Name == name {
			return []providers.ToolDefinition{definition}
		}
	}
	return nil
}

func cloneRequestOptions(options map[string]any) map[string]any {
	cloned := make(map[string]any, len(options)+1)
	for key, value := range options {
		cloned[key] = value
	}
	return cloned
}

func (s *ThinkStage) logFinalRequestActual(ctx context.Context, state *RunState, estimate FinalRequestEstimate, usage providers.Usage) {
	actualInput := InputContextTokens(usage)
	if actualInput <= 0 || estimate.InputTokens <= 0 {
		return
	}

	providerName := ""
	if state.Provider != nil {
		providerName = state.Provider.Name()
	}
	ratio := float64(actualInput) / float64(estimate.InputTokens)
	level, reason := finalContextActualLevel(estimate, actualInput)
	args := []any{
		"run_id", state.RunID,
		"iteration", state.Iteration,
		"provider", providerName,
		"model", state.Model,
		"estimated_input", estimate.InputTokens,
		"actual_input", actualInput,
		"estimate_ratio", ratio,
		"effective_window", estimate.ContextWindow,
		"hard_input_cap", estimate.HardInputCapTokens,
		"compact_target", estimate.CompactTargetTokens,
	}
	if reason != "" {
		args = append(args, "reason", reason)
	}
	slog.Log(ctx, level, "final_context.actual", args...)
}

func finalContextActualLevel(estimate FinalRequestEstimate, actualInput int) (slog.Level, string) {
	if actualInput > estimate.HardInputCapTokens {
		return slog.LevelWarn, "hard_cap_exceeded"
	}
	if actualInput*100 > estimate.InputTokens*105 {
		return slog.LevelWarn, "estimate_undercount"
	}
	return slog.LevelDebug, ""
}

func (s *ThinkStage) tryEmergencyCompaction(ctx context.Context, state *RunState, reason string) bool {
	state.Think.OverflowRetries++
	if s.deps.CompactMessages == nil {
		return false
	}

	originalLen := len(state.Messages.History())
	savedPending := state.Messages.Pending()
	compacted, compactErr := s.deps.CompactMessages(ctx, state.Messages.History(), state.Model)
	if compactErr != nil {
		slog.Warn("emergency_compaction_failed", "reason", reason, "error", compactErr)
		return false
	}
	state.Messages.ReplaceHistory(compacted)
	for _, msg := range savedPending {
		state.Messages.AppendPending(msg)
	}
	slog.Info("emergency_compaction_triggered",
		"run_id", state.RunID,
		"reason", reason,
		"original_msgs", originalLen,
		"compacted_msgs", len(compacted),
	)
	return true
}

func isEmptyLengthResponse(resp *providers.ChatResponse) bool {
	return resp != nil &&
		resp.FinishReason == "length" &&
		strings.TrimSpace(resp.Content) == "" &&
		len(resp.ToolCalls) == 0 &&
		len(resp.Images) == 0
}

func (s *ThinkStage) emitToolIterationBlockReply(state *RunState, resp *providers.ChatResponse) {
	content := resp.Content
	source := protocol.BlockReplySourceLLMProgress
	if len(resp.ToolCalls) > 0 {
		source = protocol.BlockReplySourceToolAnnouncement
	}
	if strings.TrimSpace(content) == "" {
		return
	}
	// Tool-iteration text is delivered immediately on non-streaming channels.
	// Never expose a ticket or claim an Admin handoff before the handoff tool has
	// completed; FinalizeStage owns the single, truthful customer confirmation.
	if responseCallsTool(resp, "escalate_to_admin") || adminHandoffTicketPattern.MatchString(content) || unsupportedAdminHandoffClaim(content, state) {
		return
	}
	if s.deps.EmitBlockReplyWithSource != nil {
		s.deps.EmitBlockReplyWithSource(content, source)
		return
	}
	if s.deps.EmitBlockReply != nil {
		s.deps.EmitBlockReply(content)
	}
}

func responseCallsTool(resp *providers.ChatResponse, name string) bool {
	if resp == nil {
		return false
	}
	for _, call := range resp.ToolCalls {
		if call.Name == name {
			return true
		}
	}
	return false
}

// maybeInjectNudge injects iteration budget warnings at 70% and 90%.
func (s *ThinkStage) maybeInjectNudge(state *RunState) {
	maxIter := s.deps.Config.MaxIterations
	if maxIter <= 0 {
		return
	}
	pct := float64(state.Iteration) / float64(maxIter)

	if pct >= 0.9 && !state.Evolution.Nudge90Sent {
		state.Evolution.Nudge90Sent = true
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: "[System] URGENT: You are at 90% of your iteration budget. Wrap up immediately — deliver final results now.",
		})
	} else if pct >= 0.7 && !state.Evolution.Nudge70Sent {
		state.Evolution.Nudge70Sent = true
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: "[System] You have used 70% of your iteration budget. Start wrapping up your work.",
		})
	}
}

// toolCallsHaveParseErrors returns true if any tool call has a non-empty ParseError,
// indicating the arguments JSON was malformed or truncated by the provider.
func toolCallsHaveParseErrors(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if tc.ParseError != "" {
			return true
		}
	}
	return false
}

// mutatingToolsRequireArgs is the static allowlist of tools where empty
// arguments are virtually never legitimate. Production telemetry (30d) shows
// 1/211 tool_call spans had empty args (the Gemini-3 budget-exhaustion trace);
// datetime/heartbeat/web_search always carry args. Conservative scope — expand
// only with telemetry justification.
var mutatingToolsRequireArgs = map[string]struct{}{
	"write_file":   {},
	"edit":         {},
	"exec":         {},
	"create_image": {},
	"read_file":    {},
}

// toolCallsHaveMissingRequiredArgs returns true when any call in the batch
// targets a mutating tool from the allowlist but carries empty Arguments.
// This is the Gemini-3 truncation signal: finish_reason="tool_calls" with
// len(args)==0 on a tool we know requires params means the budget ran out
// before args could be emitted.
func toolCallsHaveMissingRequiredArgs(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if _, requires := mutatingToolsRequireArgs[tc.Name]; !requires {
			continue
		}
		if len(tc.Arguments) == 0 {
			return true
		}
	}
	return false
}

// isContextOverflowErr checks if an error indicates context window overflow.
// Uses the exported helper from providers package for pattern matching.
func isContextOverflowErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return providers.IsContextOverflowMessage(lower)
}

// maxBudgetReductionRetries bounds how many times a single iteration re-enters
// reduction after the central request-budget guard rejects the built request.
// Each retry runs one reduction step; three covers prune -> compact -> shrink.
const maxBudgetReductionRetries = 3

// contextBudgetExceededError is satisfied by the agent loop's
// RequestBudgetExceededError. Detected via interface so this package does not
// import the agent package (which would be a cycle).
type contextBudgetExceededError interface {
	ContextBudgetExceeded() bool
}

// isRequestBudgetExceededErr reports whether err (or anything it wraps) is a
// request-level context-budget overflow raised by the central pre-transport
// guard. Such an error guarantees no provider transport call was made.
func isRequestBudgetExceededErr(err error) bool {
	var budgetErr contextBudgetExceededError
	if errors.As(err, &budgetErr) {
		return budgetErr.ContextBudgetExceeded()
	}
	return false
}

// reduceForBudgetExceeded runs one pass of the reduction chain
// (prune_history -> compact_history -> shrink_memory) against the current
// state, stopping at the first step that changes anything. It increments
// OverflowRetries so a stuck request eventually aborts. Returns true when some
// reduction was applied and the iteration should retry.
func (s *ThinkStage) reduceForBudgetExceeded(ctx context.Context, state *RunState) bool {
	state.Think.OverflowRetries++

	// Rebuild the estimate against current messages so pruning targets the
	// right budget. Tools are rebuilt lazily by the retried iteration.
	req := s.buildChatRequest(state, nil)
	estimate, err := s.finalRequestEstimate(state, req)
	if err != nil {
		slog.Warn("request_budget.count_failed", "run_id", state.RunID, "error", err)
		return false
	}

	steps := []string{"prune_history", "compact_history", "shrink_memory"}
	for _, step := range steps {
		changed, err := s.reduceFinalRequestContext(ctx, state, estimate, step)
		if err != nil {
			slog.Warn("request_budget.reduce_failed",
				"run_id", state.RunID,
				"step", step,
				"error", err)
			continue
		}
		if changed {
			slog.Info("request_budget.reduced",
				"run_id", state.RunID,
				"step", step,
				"overflow_retries", state.Think.OverflowRetries)
			return true
		}
	}
	return false
}
