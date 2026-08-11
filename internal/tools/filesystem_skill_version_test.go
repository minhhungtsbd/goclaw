package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileUsesLatestManagedSkillVersion(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills-store", "cloudmini-support")
	oldFile := filepath.Join(skillsDir, "1", "knowledge", "proxy-troubleshooting.md")
	latestFile := filepath.Join(skillsDir, "2", "knowledge", "proxy-troubleshooting.md")
	for path, content := range map[string]string{oldFile: "old policy", latestFile: "latest policy"} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("create skill directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write skill file: %v", err)
		}
	}

	reader := NewReadFileTool(root, true)
	reader.AllowPaths(root)
	result := reader.Execute(context.Background(), map[string]any{"path": oldFile})
	if result.IsError {
		t.Fatalf("read_file error: %s", result.ForLLM)
	}
	if result.ForLLM != "latest policy" {
		t.Fatalf("read_file = %q, want latest skill content", result.ForLLM)
	}
}
