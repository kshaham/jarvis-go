package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteStatusLine_OutputStyleWriteFails drives the writeFileAtomic0644
// error branch for the output-style file: the script write succeeds, but the
// output-style tmpfile path is pre-created as a directory so its WriteFile
// fails.
func TestWriteStatusLine_OutputStyleWriteFails(t *testing.T) {
	cwd := t.TempDir()
	claudeDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create output-style.json.tmp AS A DIRECTORY so writeFileAtomic0644's
	// WriteFile(tmp, ...) fails while the earlier script write still succeeds.
	if err := os.MkdirAll(filepath.Join(claudeDir, "output-style.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteStatusLine(cwd, StatusLineParams{
		SessionID: "sess-1",
		DaemonURL: "http://jarvis:8080",
	})
	if err == nil {
		t.Fatal("expected an error writing the output-style file")
	}
}

// TestListSnapshots_UnreadableSnapshotSkipped drives the readErr skip in
// ListSnapshots: a name-matching .json snapshot that cannot be read is skipped.
func TestListSnapshots_UnreadableSnapshotSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	t.Setenv("HOME", t.TempDir())
	dir := snapshotDir()
	if dir == "" {
		t.Skip("snapshot dir unavailable in this environment")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A name-matching but unreadable snapshot (chmod 000) is skipped; a good
	// one is listed.
	bad := filepath.Join(dir, "locked.json")
	if err := os.WriteFile(bad, []byte(`{"name":"locked"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"name":"good"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	// The unreadable one must be absent; the good one present.
	for _, s := range out {
		if s.Name == "locked" {
			t.Error("expected unreadable snapshot to be skipped")
		}
	}
}

// TestCleanupClaudeFiles_UnreadableMarkdownSkipped drives the os.ReadFile error
// skip inside CleanupClaudeFiles: an unreadable .md file in the commands dir is
// skipped rather than aborting the cleanup.
func TestCleanupClaudeFiles_UnreadableMarkdownSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	cwd := t.TempDir()
	cmdsDir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An unreadable .md file that the cleanup scan must skip.
	bad := filepath.Join(cmdsDir, "locked.md")
	if err := os.WriteFile(bad, []byte("locked content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	// Must not panic or error out on the unreadable file.
	CleanupClaudeFiles(cwd)
}
