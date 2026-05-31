package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteAgents_WriteTmpError drives the os.WriteFile(tmp) error branch
// in WriteAgents (claudefiles.go ~117-119): a directory at <name>.md.tmp
// makes the write fail with EISDIR.
func TestWriteAgents_WriteTmpError(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(filepath.Join(dir, "ag.md.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAgents(cwd, []ClaudeAgent{{Name: "ag", Description: "d", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected write-tmp error, got %v", err)
	}
}

// TestWriteAppendSystemPromptFile_WriteTmpError drives the
// os.WriteFile(tmp) error branch (claudefiles.go ~235-237): a directory
// at the append-system-prompt .tmp path makes the write fail.
func TestWriteAppendSystemPromptFile_WriteTmpError(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := AppendSystemPromptPath(cwd, "") + ".tmp"
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAppendSystemPromptFile(cwd, "", "content")
	if err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected write-tmp error, got %v", err)
	}
}

// Note: the os.Rename failure branches in WriteCommands / WriteAgents /
// WriteAppendSystemPromptFile are not deterministically reachable: each
// reads the target path first, and a directory at that path makes the
// preceding os.ReadFile fail (EISDIR, the read-existing-error branch)
// before rename is ever attempted. A genuine rename failure needs a
// cross-device or TOCTOU condition — listed in 'unreachable'.
