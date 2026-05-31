package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanupClaudeFiles_SkipsUnreadableEntry drives the os.ReadFile
// error continue in CleanupClaudeFiles (claudefiles.go ~153-154): a
// `.md` entry that is actually a directory makes ReadFile return EISDIR,
// so the entry is skipped while a jarvis-authored sibling is removed.
func TestCleanupClaudeFiles_SkipsUnreadableEntry(t *testing.T) {
	cwd := t.TempDir()
	cmds := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(cmds, 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory ending in .md => ReadFile(path) errors => continue.
	if err := os.MkdirAll(filepath.Join(cmds, "weird.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A jarvis-authored command that SHOULD be removed.
	jarvisFile := filepath.Join(cmds, "real.md")
	if err := os.WriteFile(jarvisFile, []byte(jarvisFileMarker+"\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CleanupClaudeFiles(cwd); err != nil {
		t.Fatalf("CleanupClaudeFiles: %v", err)
	}
	// The jarvis file is removed; the weird.md directory remains.
	if _, err := os.Stat(jarvisFile); !os.IsNotExist(err) {
		t.Errorf("jarvis-authored command should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cmds, "weird.md")); err != nil {
		t.Errorf("unreadable .md entry should be left alone: %v", err)
	}
}

// TestWriteAppendSystemPromptFile_LeavesOperatorFile drives the
// operator-authored leave-alone branch in WriteAppendSystemPromptFile:
// an existing file WITHOUT the jarvis marker is returned untouched.
func TestWriteAppendSystemPromptFile_LeavesOperatorFile(t *testing.T) {
	cwd := t.TempDir()
	path := AppendSystemPromptPath(cwd, "")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	operator := []byte("operator wrote this, no marker\n")
	if err := os.WriteFile(path, operator, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := WriteAppendSystemPromptFile(cwd, "", "jarvis content")
	if err != nil {
		t.Fatalf("WriteAppendSystemPromptFile: %v", err)
	}
	if got != path {
		t.Errorf("expected the operator path returned, got %q", got)
	}
	// File content must be unchanged.
	after, _ := os.ReadFile(path)
	if string(after) != string(operator) {
		t.Errorf("operator file should be untouched, got %q", after)
	}
	if strings.Contains(string(after), "jarvis content") {
		t.Error("jarvis content should not have overwritten the operator file")
	}
}
