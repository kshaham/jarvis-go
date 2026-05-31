package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildCodeGraphSnapshot_EmptyDirYieldsEmptyEntries drives the
// entries==nil → empty-slice branch: a dir with nothing but the root.
func TestBuildCodeGraphSnapshot_EmptyDirYieldsEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	snap := BuildCodeGraphSnapshot(dir)
	m, ok := snap.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map snapshot, got %T", snap)
	}
	entries, ok := m["entries"].([]interface{})
	if !ok {
		t.Fatalf("expected entries slice, got %T", m["entries"])
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("expected empty (non-nil) entries for empty dir, got %v", entries)
	}
}

// TestBuildCodeGraphSnapshot_DepthCapSkipsDeepDirs drives the max-depth
// skip: a directory nested deeper than codeGraphMaxDepth is pruned.
func TestBuildCodeGraphSnapshot_DepthCapSkipsDeepDirs(t *testing.T) {
	root := t.TempDir()
	// Build root/a/b/c/d/deep.txt — d is at depth 4 (> codeGraphMaxDepth=3).
	deep := filepath.Join(root, "a", "b", "c", "d")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "deep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A shallow file that must be included.
	if err := os.WriteFile(filepath.Join(root, "shallow.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root)
	m := snap.(map[string]interface{})
	entries := m["entries"].([]interface{})
	sawDeep := false
	sawShallow := false
	for _, e := range entries {
		em := e.(map[string]interface{})
		switch em["name"] {
		case "deep.txt":
			sawDeep = true
		case "shallow.txt":
			sawShallow = true
		}
	}
	if !sawShallow {
		t.Error("expected shallow.txt to be included")
	}
	if sawDeep {
		t.Error("expected deep.txt (depth 5) to be pruned by the depth cap")
	}
}
