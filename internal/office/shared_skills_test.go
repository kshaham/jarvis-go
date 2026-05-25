package office

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// withTempHome redirects HOME so sharedSkillsRoot points into a
// temp directory. Returns the SKILL_ROOT path for assertions.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".claude", "skills")
}

func TestWriteSharedSkill_CreatesNewFile(t *testing.T) {
	root := withTempHome(t)
	path, updated, err := WriteSharedSkill("test-skill", "# Test\nhello")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if updated {
		t.Error("expected updated=false for new file")
	}
	if path != filepath.Join(root, "test-skill", "SKILL.md") {
		t.Errorf("path=%q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "# Test\nhello" {
		t.Errorf("body=%q", string(body))
	}
}

func TestWriteSharedSkill_OverwritesExistingFile(t *testing.T) {
	_ = withTempHome(t)
	// First write.
	path, updated, err := WriteSharedSkill("dup", "first")
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("first write should have updated=false")
	}
	// Second write same name.
	_, updated2, err := WriteSharedSkill("dup", "second")
	if err != nil {
		t.Fatal(err)
	}
	if !updated2 {
		t.Error("second write should have updated=true")
	}
	body, _ := os.ReadFile(path)
	if string(body) != "second" {
		t.Errorf("body=%q want second", body)
	}
}

func TestWriteSharedSkill_PathTraversalRejected(t *testing.T) {
	_ = withTempHome(t)
	_, _, err := WriteSharedSkill("../../etc/passwd", "evil")
	if !errors.Is(err, ErrSkillPathTraversal) {
		t.Errorf("expected ErrSkillPathTraversal, got %v", err)
	}
}

func TestDeleteSharedSkill_HappyPath(t *testing.T) {
	_ = withTempHome(t)
	if _, _, err := WriteSharedSkill("toremove", "x"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSharedSkill("toremove"); err != nil {
		t.Errorf("delete: %v", err)
	}
	// Removal should be recursive — the dir itself should be gone.
	if _, err := os.Stat(filepath.Join(sharedSkillsRoot(), "toremove")); !os.IsNotExist(err) {
		t.Errorf("skill dir still exists: %v", err)
	}
}

func TestDeleteSharedSkill_PathTraversalRejected(t *testing.T) {
	_ = withTempHome(t)
	if err := DeleteSharedSkill("../../etc"); !errors.Is(err, ErrSkillPathTraversal) {
		t.Errorf("expected ErrSkillPathTraversal, got %v", err)
	}
}

func TestDeleteSharedSkill_NonExistentNotAnError(t *testing.T) {
	// os.RemoveAll returns nil for non-existent paths, so the helper
	// should too.
	_ = withTempHome(t)
	if err := DeleteSharedSkill("never-existed"); err != nil {
		t.Errorf("delete of non-existent skill returned error: %v", err)
	}
}
