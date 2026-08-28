package tools

import (
	"context"
	"strings"
	"testing"
)

func TestUseSkillRejectsHistoricalSkillPaths(t *testing.T) {
	result := NewUseSkillTool().Execute(context.Background(), map[string]any{"name": "cloudmini-support"})
	if result.IsError {
		t.Fatalf("use_skill error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Ignore every SKILL.md path from conversation history") {
		t.Fatalf("use_skill result = %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "next action MUST be read_file") {
		t.Fatalf("use_skill did not require loading instructions: %q", result.ForLLM)
	}
}
