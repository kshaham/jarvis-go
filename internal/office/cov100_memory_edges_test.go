package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadMemorySections_PreludeNoExistingNotes drives the else branch of the
// prelude->notes merge: a memory file with leading prose but NO notes section,
// so the prelude becomes the notes section verbatim.
func TestLoadMemorySections_PreludeNoExistingNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-prelude-only"
	path := GetAgentMemoryPath("cfg", wd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Leading prose only; a non-notes section ensures "notes" is absent.
	body := "# Memory: cfg\n\nstandalone prelude line\n\n## section: decisions\nchose option A\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := MemoryReadSection("cfg", wd, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "standalone prelude line") {
		t.Errorf("expected prelude promoted into notes, got %q", got)
	}
	if strings.Contains(got, "chose option A") {
		t.Error("decisions content should not leak into notes")
	}
}

// TestMemoryQuery_UnreadableFileReturnsError drives the os.Open
// non-IsNotExist error branch in MemoryQuery: an existing but unreadable
// memory file makes os.Open fail with EACCES (not "does not exist").
func TestMemoryQuery_UnreadableFileReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file perms are bypassed")
	}
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-query-unreadable"
	path := GetAgentMemoryPath("cfg", wd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("## section: notes\nneedle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if _, err := MemoryQuery("cfg", wd, "needle"); err == nil {
		t.Fatal("expected an open error for an unreadable memory file")
	}
}

// TestWriteMemorySections_MkdirFails drives the MkdirAll error branch in
// writeMemorySections: the agent-memory root is read-only so creating the
// per-project subdir fails, while the (missing) file's load returns empty
// sections without error first.
func TestWriteMemorySections_MkdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir perms are bypassed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := "/proj/mem-mkdir-fail"
	path := GetAgentMemoryPath("cfg", wd)
	hashDir := filepath.Dir(path) // .../agent-memory/<hash>
	agentMemRoot := filepath.Dir(hashDir)

	// Create the agent-memory root but NOT the per-project hash subdir, then
	// make the root read-only so MkdirAll(hashDir) fails. The load step sees
	// the file as not-existing (IsNotExist) -> empty sections, nil error.
	if err := os.MkdirAll(agentMemRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agentMemRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentMemRoot, 0o755) })

	if err := MemoryWriteSection("cfg", wd, "notes", "content"); err == nil {
		t.Fatal("expected MkdirAll error when agent-memory root is read-only")
	}
}
