package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestLoadSkillFrontmatterCache_NoHome covers the UserHomeDir error
// branch via the reset seam.
func TestLoadSkillFrontmatterCache_NoHome(t *testing.T) {
	resetSkillFrontmatterCacheForTest()
	t.Cleanup(resetSkillFrontmatterCacheForTest)
	withNoHome(t)
	got := loadSkillFrontmatterCache()
	if len(got) != 0 {
		t.Errorf("expected empty cache with no HOME, got %v", got)
	}
}

// TestLoadSkillFrontmatterCache_MissingDir covers the ReadDir error
// branch (no ~/.claude/skills present).
func TestLoadSkillFrontmatterCache_MissingDir(t *testing.T) {
	resetSkillFrontmatterCacheForTest()
	t.Cleanup(resetSkillFrontmatterCacheForTest)
	t.Setenv("HOME", t.TempDir())
	got := loadSkillFrontmatterCache()
	if len(got) != 0 {
		t.Errorf("expected empty cache when skills dir missing, got %v", got)
	}
}

// TestLoadSkillFrontmatterCache_ParsesAndSkipsNonDir seeds a skills dir
// with one real skill, a non-dir entry (skipped), and a skill missing
// SKILL.md (empty description).
func TestLoadSkillFrontmatterCache_ParsesAndSkips(t *testing.T) {
	resetSkillFrontmatterCacheForTest()
	t.Cleanup(resetSkillFrontmatterCacheForTest)
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := "---\nname: alpha\ndescription: \"does alpha things\"\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(root, "alpha", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-directory entry inside skills root → skipped by !e.IsDir().
	if err := os.WriteFile(filepath.Join(root, "loose-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A skill dir without SKILL.md → empty description.
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}

	cache := loadSkillFrontmatterCache()
	if cache["alpha"] != "does alpha things" {
		t.Errorf("expected alpha description parsed, got %q", cache["alpha"])
	}
	if _, ok := cache["beta"]; !ok {
		t.Error("expected beta present with empty description")
	}
	if cache["beta"] != "" {
		t.Errorf("expected empty description for beta, got %q", cache["beta"])
	}
	if _, ok := cache["loose-file"]; ok {
		t.Error("non-directory entry should not be cached")
	}
}

// TestReadSkillDescription_Errors covers the open-error and no-frontmatter
// branches.
func TestReadSkillDescription_OpenError(t *testing.T) {
	if _, err := readSkillDescription(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("expected open error for missing SKILL.md")
	}
}

func TestReadSkillDescription_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	// First three lines are not `---` → returns "" after lineNo>3.
	if err := os.WriteFile(p, []byte("line1\nline2\nline3\nline4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desc, err := readSkillDescription(p)
	if err != nil || desc != "" {
		t.Errorf("expected empty desc with no frontmatter, got %q,%v", desc, err)
	}
}

func TestReadSkillDescription_FrontmatterNoDescription(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	// Frontmatter present but closes without a description line.
	if err := os.WriteFile(p, []byte("---\nname: x\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desc, err := readSkillDescription(p)
	if err != nil || desc != "" {
		t.Errorf("expected empty desc when no description field, got %q,%v", desc, err)
	}
}

// TestOneLine_MidRuneBackup forces the rune-boundary backup loop by
// placing a 4-byte emoji so the cut index lands on a continuation byte.
func TestOneLine_MidRuneBackup(t *testing.T) {
	// max=110, limit starts at 107. Put a 4-byte rune at bytes 105..108 so
	// s[107] is a continuation byte and the loop backs up to 105.
	long := strings.Repeat("a", 105) + "🚀" + strings.Repeat("b", 20)
	out := oneLine(long)
	if !utf8.ValidString(out) {
		t.Errorf("oneLine produced invalid UTF-8: %q", out)
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("expected ellipsis, got %q", out)
	}
}

// TestResetECCMemoForTest exercises the test-only reset helper so it is
// covered (it is itself a behaviour-preserving seam).
func TestResetECCMemoForTest(t *testing.T) {
	// Calling it must not panic and must allow a fresh parse afterward.
	resetECCMemoForTest()
	agents := BuildECCAgents()
	if agents == nil {
		t.Error("expected embedded agents after reset")
	}
	resetECCMemoForTest()
	cmds := BuildECCCommands()
	if cmds == nil {
		t.Error("expected embedded commands after reset")
	}
}
