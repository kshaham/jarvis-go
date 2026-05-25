package cases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCaseManager_CreatesBaseDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "nested", "cases")
	cm := NewCaseManager(base)
	if cm == nil {
		t.Fatal("nil manager")
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("base dir not created: %v", err)
	}
}

func TestCreate_HappyPath(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	info, err := cm.Create("alpha-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.Name != "alpha-1" {
		t.Errorf("name=%q", info.Name)
	}
	if _, err := os.Stat(info.Path); err != nil {
		t.Errorf("path not on disk: %v", err)
	}
}

func TestCreate_RejectsInvalidNames(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	cases := []string{
		"",
		"../escape",
		"foo/bar",
		"with space",
		"weird@char",
		"a..b", // contains ".."
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := cm.Create(name); err == nil {
				t.Errorf("Create(%q) should have errored", name)
			}
		})
	}
}

func TestCreate_AcceptsValidNames(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	cases := []string{"abc", "ABC123", "with-dash", "with_underscore", "with.dot", "AlphaNumeric_42"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := cm.Create(name); err != nil {
				t.Errorf("Create(%q) failed: %v", name, err)
			}
		})
	}
}

func TestList_SortedByName(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		if _, err := cm.Create(n); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}
	got := cm.List()
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("[%d] %q want %q", i, got[i].Name, w)
		}
	}
}

func TestList_SkipsHiddenAndFiles(t *testing.T) {
	base := t.TempDir()
	cm := NewCaseManager(base)
	if _, err := cm.Create("visible"); err != nil {
		t.Fatal(err)
	}
	// Add a dotfile-style entry and a regular file alongside the case dirs.
	if err := os.Mkdir(filepath.Join(base, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := cm.List()
	if len(got) != 1 || got[0].Name != "visible" {
		t.Errorf("unexpected list: %+v", got)
	}
}

func TestList_HasInitPromptAndFixPlanFlags(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	info, err := cm.Create("case-1")
	if err != nil {
		t.Fatal(err)
	}
	// Drop the two sentinel files.
	if err := os.WriteFile(filepath.Join(info.Path, "initprompt.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "@fix_plan.md"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := cm.List()
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if !got[0].HasInitPrompt || !got[0].HasFixPlan {
		t.Errorf("flags missed: %+v", got[0])
	}
}

func TestGet_NonExistent(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if _, err := cm.Get("nope"); err == nil {
		t.Error("expected error for missing case")
	}
}

func TestDelete_HappyPath(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	info, err := cm.Create("toremove")
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.Delete("toremove"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Errorf("dir still exists: %v", err)
	}
}

func TestDelete_RejectsTraversal(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if err := cm.Delete("../escape"); err == nil {
		t.Error("expected error for traversal name")
	}
}

func TestLink_HappyPath(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	target := t.TempDir() // exists, is a dir
	info, err := cm.Link("linkedcase", target)
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if info.Path != target {
		t.Errorf("Path=%q want %q", info.Path, target)
	}
	// Verify the symlink exists in the cases dir.
	resolved, err := os.Readlink(filepath.Join(cm.baseDir, "linkedcase"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if resolved != target {
		t.Errorf("symlink target=%q want %q", resolved, target)
	}
}

func TestLink_RejectsDuplicate(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	target := t.TempDir()
	if _, err := cm.Link("dup", target); err != nil {
		t.Fatal(err)
	}
	if _, err := cm.Link("dup", target); err == nil {
		t.Error("expected error on duplicate link name")
	}
}

func TestLink_RejectsMissingTarget(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	if _, err := cm.Link("c1", "/path/that/definitely/does/not/exist/xyz"); err == nil {
		t.Error("expected error for missing target")
	}
}

func TestLink_RejectsFileTarget(t *testing.T) {
	cm := NewCaseManager(t.TempDir())
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cm.Link("c1", f); err == nil {
		t.Error("expected error when target is a file, not a directory")
	}
}

func TestIsValidCaseName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"happy alphanumeric", "alpha-1", true},
		{"with dot", "case.v2", true},
		{"with underscore", "case_v2", true},
		{"mixed case", "Alpha", true},
		{"all digits", "12345", true},

		{"empty", "", false},
		{"slash", "foo/bar", false},
		{"backslash", "foo\\bar", false},
		{"space", "with space", false},
		{"tilde", "~tilde", false},
		{"dot-dot literal", "..", false},
		{"dot-dot in middle", "a..b", false},
		{"leading dotdot", "../escape", false},
		{"too long", strings.Repeat("a", 129), false},
		{"control char", "alpha\x00", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidCaseName(tc.in); got != tc.want {
				t.Errorf("IsValidCaseName(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}
