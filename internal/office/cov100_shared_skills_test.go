package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteSharedSkill_MkdirFails makes ~/.claude/skills a file so the
// per-skill MkdirAll fails.
func TestWriteSharedSkill_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	// skills is a FILE → MkdirAll(skills/<name>) fails.
	if err := os.WriteFile(filepath.Join(claude, "skills"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteSharedSkill("myskill", "content"); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestWriteSharedSkill_WriteFails makes the SKILL.md path a directory so
// os.WriteFile fails.
func TestWriteSharedSkill_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillDir := filepath.Join(sharedSkillsRoot(), "myskill")
	if err := os.MkdirAll(filepath.Join(skillDir, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteSharedSkill("myskill", "content"); err == nil {
		t.Fatal("expected write error when SKILL.md is a directory")
	}
}

// TestDeleteSharedSkill_RemoveError makes the skill a read-only dir
// containing a file so RemoveAll fails on the contained entry.
func TestDeleteSharedSkill_RemoveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir would not block RemoveAll")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillDir := filepath.Join(sharedSkillsRoot(), "locked")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the skill dir read-only AND non-executable so RemoveAll can't
	// traverse/unlink its contents.
	if err := os.Chmod(skillDir, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillDir, 0o755) })

	if err := DeleteSharedSkill("locked"); err == nil {
		t.Fatal("expected RemoveAll error on a read-only skill dir")
	}
}
