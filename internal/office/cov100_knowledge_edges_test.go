package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanMemoryHits_UnreadableMarkdownSkipped drives the os.Open error skip
// (openErr): an unreadable .md file (chmod 000) cannot be opened, so the
// scanner moves on to the next file.
func TestScanMemoryHits_UnreadableMarkdownSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := "/proj/openerr"
	dir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash(wd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An unreadable .md (Open fails) plus a good one (hit).
	bad := filepath.Join(dir, "locked.md")
	if err := os.WriteFile(bad, []byte("## section: a\nlocked secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	if err := os.WriteFile(filepath.Join(dir, "good.md"), []byte("## section: a\nfindme please\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits := scanMemoryHits("findme", KnowledgeSearchOptions{WorkingDir: wd})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit from the readable file (unreadable one skipped), got %d (%v)", len(hits), hits)
	}
}

// TestScanLearningHits_UnreadableJSONSkipped drives the readErr skip in
// scanLearningHits: an unreadable .json file cannot be read so it's skipped.
func TestScanLearningHits_UnreadableJSONSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := learningsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "locked.json")
	if err := os.WriteFile(bad, []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	// Must not panic; the unreadable file is skipped and no hits found.
	hits := scanLearningHits("anything", KnowledgeSearchOptions{})
	if hits != nil && len(hits) != 0 {
		t.Errorf("expected no hits from an unreadable learnings file, got %v", hits)
	}
}

// TestScanStandupHits_UnreadableJSONSkipped drives the readErr skip in
// scanStandupHits via an unreadable standup .json file.
func TestScanStandupHits_UnreadableJSONSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "locked.json")
	if err := os.WriteFile(bad, []byte(`{"id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	hits := scanStandupHits("anything", KnowledgeSearchOptions{})
	if hits != nil && len(hits) != 0 {
		t.Errorf("expected no hits from an unreadable standup file, got %v", hits)
	}
}
