package office

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeCodeGraphImpact_ExistingAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := AnalyzeCodeGraphImpact(root, []string{"real.txt", "ghost.txt"})
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if !got[0].Exists || got[0].Size != 5 {
		t.Errorf("real.txt: %+v", got[0])
	}
	if got[1].Exists || got[1].Size != 0 {
		t.Errorf("ghost.txt should be Exists=false Size=0: %+v", got[1])
	}
}

func TestAnalyzeCodeGraphImpact_AbsolutePathBypassesRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	abs := filepath.Join(other, "elsewhere.txt")
	if err := os.WriteFile(abs, []byte("xyz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := AnalyzeCodeGraphImpact(root, []string{abs})
	if !got[0].Exists || got[0].Size != 3 {
		t.Errorf("absolute path: %+v", got[0])
	}
}

func TestAnalyzeCodeGraphImpact_EmptyInputReturnsEmptySlice(t *testing.T) {
	got := AnalyzeCodeGraphImpact("/tmp", nil)
	if got == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestBuildCodeGraphSnapshot_EmptyRootReturnsNil(t *testing.T) {
	if got := BuildCodeGraphSnapshot(""); got != nil {
		t.Errorf("expected nil for empty root, got %+v", got)
	}
}

func TestBuildCodeGraphSnapshot_MissingDirReturnsNil(t *testing.T) {
	if got := BuildCodeGraphSnapshot("/nonexistent/path/abc/xyz/123"); got != nil {
		t.Errorf("expected nil for missing dir, got %+v", got)
	}
}

func TestBuildCodeGraphSnapshot_HappyPath(t *testing.T) {
	root := t.TempDir()
	// Build a tiny tree:
	//   root/
	//     a.txt
	//     subdir/
	//       b.txt
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "subdir", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root).(map[string]interface{})
	if snap["root"] != root {
		t.Errorf("root=%v want %s", snap["root"], root)
	}
	if snap["total"].(int) < 3 {
		t.Errorf("total=%d want >=3 (a.txt, subdir, b.txt)", snap["total"])
	}
	entries := snap["entries"].([]interface{})
	if len(entries) < 3 {
		t.Errorf("expected ≥3 entries, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		m := e.(map[string]interface{})
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"a.txt", "subdir", "b.txt"} {
		if !names[want] {
			t.Errorf("missing entry %q in snapshot: %+v", want, names)
		}
	}
}

func TestBuildCodeGraphSnapshot_SkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root).(map[string]interface{})
	for _, e := range snap["entries"].([]interface{}) {
		m := e.(map[string]interface{})
		name := m["name"].(string)
		if name == ".git" || name == "config" {
			t.Errorf("expected .git contents to be skipped, got %q", name)
		}
	}
}

func TestBuildCodeGraphSnapshot_SkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "lodash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "lodash", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root).(map[string]interface{})
	for _, e := range snap["entries"].([]interface{}) {
		m := e.(map[string]interface{})
		name := m["name"].(string)
		if name == "lodash" || name == "index.js" {
			t.Errorf("expected node_modules/ contents to be skipped, got %q (path=%v)",
				name, m["path"])
		}
	}
}

func TestBuildCodeGraphSnapshot_RespectsMaxDepth(t *testing.T) {
	// Build a tree deeper than codeGraphMaxDepth.
	root := t.TempDir()
	deep := root
	for i := 0; i < codeGraphMaxDepth+5; i++ {
		deep = filepath.Join(deep, "d"+string(rune('0'+i)))
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Place a marker file at the deepest level.
	if err := os.WriteFile(filepath.Join(deep, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root).(map[string]interface{})
	for _, e := range snap["entries"].([]interface{}) {
		m := e.(map[string]interface{})
		if m["name"] == "marker.txt" {
			t.Errorf("marker beyond max depth leaked in: %+v", m)
		}
	}
}
