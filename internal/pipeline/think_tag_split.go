package pipeline

import (
	"regexp"
	"strings"
)

var taggedThinkingOpenRe = regexp.MustCompile(`(?is)<\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\b[^>]*>`)
var taggedThinkingCloseRe = regexp.MustCompile(`(?is)</\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\s*>`)

func splitTaggedThinkingContent(content, existingThinking string) (string, string) {
	openLoc := taggedThinkingOpenRe.FindStringIndex(content)
	if openLoc == nil {
		// Some OpenAI-compatible reasoning models omit the opening tag and emit
		// only: "private reasoning</think>visible answer". Treat the prefix as
		// thinking instead of leaking it to the customer.
		closeLoc := taggedThinkingCloseRe.FindStringIndex(content)
		if closeLoc == nil {
			return content, existingThinking
		}
		thinking := content[:closeLoc[0]]
		answer := taggedThinkingCloseRe.ReplaceAllString(content[closeLoc[1]:], "")
		return answer, appendTaggedThinking(existingThinking, thinking)
	}

	var answer strings.Builder
	var taggedThinking strings.Builder
	remaining := content

	for remaining != "" {
		openLoc := taggedThinkingOpenRe.FindStringIndex(remaining)
		if openLoc == nil {
			answer.WriteString(remaining)
			break
		}

		answer.WriteString(remaining[:openLoc[0]])
		afterOpen := remaining[openLoc[1]:]
		closeLoc := taggedThinkingCloseRe.FindStringIndex(afterOpen)
		if closeLoc == nil {
			taggedThinking.WriteString(afterOpen)
			break
		}

		taggedThinking.WriteString(afterOpen[:closeLoc[0]])
		remaining = afterOpen[closeLoc[1]:]
	}

	return answer.String(), appendTaggedThinking(existingThinking, taggedThinking.String())
}

func appendTaggedThinking(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if existing == "" {
		return addition
	}
	if addition == "" {
		return existing
	}
	return existing + "\n" + addition
}
