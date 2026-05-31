package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScanners_NoHome drives the os.UserHomeDir error branch in every
// scanner that resolves HOME directly (memory, standup, whiteboard).
func TestScanners_NoHome(t *testing.T) {
	withNoHome(t)
	opts := KnowledgeSearchOptions{}
	if got := scanMemoryHits("x", opts); got != nil {
		t.Errorf("scanMemoryHits should be nil with no HOME, got %v", got)
	}
	if got := scanStandupHits("x", opts); got != nil {
		t.Errorf("scanStandupHits should be nil with no HOME, got %v", got)
	}
	if got := scanWhiteboardHits("x"); got != nil {
		t.Errorf("scanWhiteboardHits should be nil with no HOME, got %v", got)
	}
}

// TestScanMemoryHits_RootReadDirError covers the os.ReadDir(root) error
// path: the agent-memory root does not exist at all.
func TestScanMemoryHits_RootMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No ~/.jarvis/agent-memory created → ReadDir(root) errors → nil.
	if got := scanMemoryHits("", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil when agent-memory root missing, got %v", got)
	}
}

// TestScanMemoryHits_StatErrorSkipsFile makes a memory entry's path a
// symlink to a nonexistent target so os.Stat fails and the file is
// skipped without aborting the scan.
func TestScanMemoryHits_SkipsNonMarkdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := "/proj/x"
	dir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash(wd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-.md file (skipped by suffix check) and a subdir (skipped by IsDir).
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("## section: a\nhello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := scanMemoryHits("hello", KnowledgeSearchOptions{WorkingDir: wd})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit from real.md, got %d (%v)", len(hits), hits)
	}
}

// TestScanMemoryHits_ProjectDirMissing covers the inner ReadDir(dir)
// continue branch: WorkingDir resolves to a project hash dir that doesn't
// exist on disk.
func TestScanMemoryHits_ProjectDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create root so the outer scan proceeds, but not the project subdir.
	root := filepath.Join(home, ".jarvis", "agent-memory")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	hits := scanMemoryHits("x", KnowledgeSearchOptions{WorkingDir: "/nope/missing"})
	if hits != nil {
		t.Errorf("expected nil hits for missing project dir, got %v", hits)
	}
}

// TestScanMemoryHits_AllowedAgentsExcludes covers the AllowedAgents filter
// skip in the memory scanner.
func TestScanMemoryHits_AllowedAgentsExcludes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := "/proj/y"
	dir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash(wd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("secret data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := scanMemoryHits("secret", KnowledgeSearchOptions{
		WorkingDir:    wd,
		AllowedAgents: map[string]bool{"beta": true}, // alpha not allowed
	})
	if len(hits) != 0 {
		t.Errorf("expected alpha to be filtered out, got %v", hits)
	}
}

// TestScanLearningHits_ReadDirMissing covers the learnings ReadDir error.
func TestScanLearningHits_DirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := scanLearningHits("x", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil when learnings dir missing, got %v", got)
	}
}

// TestScanLearningHits_BadJSONSkipped covers the unmarshal-error continue
// and the AllowedAgents filter.
func TestScanLearningHits_BadJSONAndFilter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := learningsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Corrupt JSON → skipped.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-json file → skipped by suffix.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Subdir → skipped by IsDir.
	if err := os.MkdirAll(filepath.Join(dir, "nested.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	hits := scanLearningHits("anything", KnowledgeSearchOptions{AllowedAgents: map[string]bool{"corrupt": true}})
	if len(hits) != 0 {
		t.Errorf("expected no hits from corrupt/skipped files, got %v", hits)
	}
}

// TestScanStandupHits_DirMissing covers the standups ReadDir error.
func TestScanStandupHits_DirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := scanStandupHits("x", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil when standups dir missing, got %v", got)
	}
}

// TestScanStandupHits_BadJSONSkipped covers the unmarshal-error and
// non-json skips.
func TestScanStandupHits_BadJSONSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dir.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := scanStandupHits("x", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil hits from bad standups, got %v", got)
	}
}

// TestScanWhiteboardHits_BadJSON covers the whiteboard unmarshal error.
func TestScanWhiteboardHits_BadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whiteboard.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := scanWhiteboardHits("x"); got != nil {
		t.Errorf("expected nil hits from corrupt whiteboard, got %v", got)
	}
}

// TestScanWhiteboardHits_EmptySectionSkipped covers the empty-content
// continue branch.
func TestScanWhiteboardHits_EmptySectionSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	board := TeamWhiteboard{Entries: map[string]TeamWhiteboardEntry{
		"A": {Sections: map[string]TeamWhiteboardSection{
			"empty": {Content: ""},
			"full":  {Content: "real content here"},
		}},
	}}
	bdata, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whiteboard.json"), bdata, 0o644); err != nil {
		t.Fatal(err)
	}
	hits := scanWhiteboardHits("")
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit (empty section skipped), got %d", len(hits))
	}
}
