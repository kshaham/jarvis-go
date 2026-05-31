package office

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// These tests drive the rename-failure branch of the three claudefiles
// writers (the `os.Remove(tmp); return ...rename...` arm) via the
// renameFile seam. The branch is otherwise unreachable in a unit test:
// reaching the rename requires the tmp write to have succeeded, and the
// only way the real os.Rename fails at that point is a mid-call FS race.

func TestWriteCommands_RenameError(t *testing.T) {
	cwd := t.TempDir()
	prev := renameFile
	sentinel := errors.New("simulated rename failure")
	renameFile = func(string, string) error { return sentinel }
	t.Cleanup(func() { renameFile = prev })

	wrote, err := WriteCommands(cwd, []ClaudeCommand{{Name: "standup", Body: "hi"}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rename sentinel error, got %v", err)
	}
	if wrote != 0 {
		t.Errorf("expected 0 written on rename failure, got %d", wrote)
	}
	// The tmp file must have been cleaned up.
	tmp := filepath.Join(cwd, ".claude", "commands", "standup.md.tmp")
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Errorf("tmp file should be removed after rename failure, stat err=%v", statErr)
	}
}

func TestWriteAgents_RenameError(t *testing.T) {
	cwd := t.TempDir()
	prev := renameFile
	sentinel := errors.New("simulated rename failure")
	renameFile = func(string, string) error { return sentinel }
	t.Cleanup(func() { renameFile = prev })

	wrote, err := WriteAgents(cwd, []ClaudeAgent{{Name: "code-architect", Body: "hi"}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rename sentinel error, got %v", err)
	}
	if wrote != 0 {
		t.Errorf("expected 0 written on rename failure, got %d", wrote)
	}
	tmp := filepath.Join(cwd, ".claude", "agents", "code-architect.md.tmp")
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Errorf("tmp file should be removed after rename failure, stat err=%v", statErr)
	}
}

func TestWriteAppendSystemPromptFile_RenameError(t *testing.T) {
	cwd := t.TempDir()
	prev := renameFile
	sentinel := errors.New("simulated rename failure")
	renameFile = func(string, string) error { return sentinel }
	t.Cleanup(func() { renameFile = prev })

	_, err := WriteAppendSystemPromptFile(cwd, "", "stable prefix content")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rename sentinel error, got %v", err)
	}
	tmp := AppendSystemPromptPath(cwd, "") + ".tmp"
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Errorf("tmp file should be removed after rename failure, stat err=%v", statErr)
	}
}
