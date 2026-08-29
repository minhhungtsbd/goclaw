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
