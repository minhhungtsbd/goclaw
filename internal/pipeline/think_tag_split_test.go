package pipeline

import "testing"

func TestSplitTaggedThinkingContent_EscapedOrphanClosingTag(t *testing.T) {
	answer, thinking := splitTaggedThinkingContent("Internal reasoning.\\</think>Visible answer", "")
	if thinking != "Internal reasoning." {
		t.Fatalf("thinking = %q, want escaped prefix as reasoning", thinking)
	}
	if answer != "Visible answer" {
		t.Fatalf("answer = %q, want visible answer", answer)
	}
}
