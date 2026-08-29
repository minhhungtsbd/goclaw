package channels

import "testing"

func TestSplitThinkTags_OrphanClosingTag(t *testing.T) {
	got := SplitThinkTags("private reasoning</think>Visible answer")

	if !got.Found {
		t.Fatal("expected orphan closing tag to be recognized")
	}
	if got.Thinking != "private reasoning" {
		t.Fatalf("Thinking = %q, want %q", got.Thinking, "private reasoning")
	}
	if got.Answer != "Visible answer" {
		t.Fatalf("Answer = %q, want %q", got.Answer, "Visible answer")
	}
}

func TestSplitThinkTags_OrphanClosingTagRemovesAdditionalClosers(t *testing.T) {
	got := SplitThinkTags("private reasoning</thinking>Visible</think> answer")

	if got.Thinking != "private reasoning" {
		t.Fatalf("Thinking = %q, want %q", got.Thinking, "private reasoning")
	}
	if got.Answer != "Visible answer" {
		t.Fatalf("Answer = %q, want %q", got.Answer, "Visible answer")
	}
}

func TestSplitThinkTags_EscapedOrphanClosingTag(t *testing.T) {
	got := SplitThinkTags("Chủ nhân chào, cần đáp lại thân thiện.\\</think>\nChào anh!")

	if got.Thinking != "Chủ nhân chào, cần đáp lại thân thiện." {
		t.Fatalf("Thinking = %q, want escaped prefix as reasoning", got.Thinking)
	}
	if got.Answer != "\nChào anh!" {
		t.Fatalf("Answer = %q, want visible answer with preserved whitespace", got.Answer)
	}
}
