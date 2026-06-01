package cases

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGet_HappyPath covers the success branch of Get (the existing suite only
// tested the non-existent case).
func TestGet_HappyPath(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if _, err := cm.Create("bug-123"); err != nil {
		t.Fatal(err)
	}
	got, err := cm.Get("bug-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name != "bug-123" {
		t.Fatalf("unexpected case info: %#v", got)
	}
}

// TestGet_InvalidName covers the validation guard in Get.
func TestGet_InvalidName(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if _, err := cm.Get("../escape"); err == nil {
		t.Fatal("expected error for invalid case name")
	}
}

// TestList_ReadsDescriptionFromConfig covers the config.json description branch.
func TestList_ReadsDescriptionFromConfig(t *testing.T) {
	base := t.TempDir()
	cm := NewCaseManager(base)
	caseDir := filepath.Join(base, "with-config")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"description":"a documented case"}`
	if err := os.WriteFile(filepath.Join(caseDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cm.List() {
		if c.Name == "with-config" {
			if c.Description != "a documented case" {
				t.Fatalf("expected description from config.json, got %q", c.Description)
			}
			return
		}
	}
	t.Fatal("case with-config not listed")
}

// TestList_ReadDirErrorReturnsNil covers the ReadDir failure branch by pointing
// the manager at a path that is a regular file, not a directory.
func TestList_ReadDirErrorReturnsNil(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cm := &CaseManager{baseDir: f}
	if got := cm.List(); got != nil {
		t.Fatalf("expected nil on ReadDir error, got %#v", got)
	}
}

// TestDelete_RejectsDotResolvingToBase covers the post-validation prefix guard:
// "." passes IsValidCaseName but resolves to baseDir itself, which must be
// rejected rather than wiping the whole cases root.
func TestDelete_RejectsDotResolvingToBase(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if err := cm.Delete("."); err == nil {
		t.Fatal("expected Delete(\".\") to be rejected")
	}
}

// TestListRalphWizardFiles_SkipsNonDirEntries covers the non-directory skip in
// the wizard listing walk.
func TestListRalphWizardFiles_SkipsNonDirEntries(t *testing.T) {
	caseDir := t.TempDir()
	wizardDir := filepath.Join(caseDir, "ralph-wizard")
	if err := os.MkdirAll(wizardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray file directly under ralph-wizard/ must be skipped.
	if err := os.WriteFile(filepath.Join(wizardDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// One real agent dir with a prompt so the slice isn't empty.
	agentDir := filepath.Join(wizardDir, "planner")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "prompt.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ListRalphWizardFiles(&CaseInfo{Path: caseDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].AgentType != "planner" {
		t.Fatalf("expected only the planner agent, got %#v", files)
	}
}
