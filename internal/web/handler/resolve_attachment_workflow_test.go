package handler

// Branch fill for resolveAttachment's "workflow" kind: read a workflow markdown
// file from the library and inline it as an attachment (the experience and
// missionExcerpt kinds are covered elsewhere).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAttachment_WorkflowFromLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wfDir := filepath.Join(home, ".jarvis", "office-memory", "library", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "auth.md"), []byte("# Auth Flow\nstep one"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	att, err := resolveAttachment("workflow", "auth")
	if err != nil {
		t.Fatalf("resolve workflow: %v", err)
	}
	if att.Kind != "workflow" {
		t.Errorf("kind=%q, want workflow", att.Kind)
	}
	if att.Title != "Auth Flow" {
		t.Errorf("title=%q, want 'Auth Flow'", att.Title)
	}
	if !strings.Contains(att.Body, "step one") {
		t.Errorf("body not loaded: %q", att.Body)
	}

	// Missing workflow file -> error.
	if _, err := resolveAttachment("workflow", "missing"); err == nil {
		t.Error("expected error for missing workflow file")
	}
}
