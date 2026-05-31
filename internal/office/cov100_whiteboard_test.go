package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWhiteboardStore_SaveLocked_MkdirFails makes the parent of the store
// path a file so saveLocked's MkdirAll fails (swallowed; no panic, no file).
func TestWhiteboardStore_SaveLocked_MkdirFails(t *testing.T) {
	dir := t.TempDir()
	// parentfile is a FILE; the store path is under it → MkdirAll(parent) fails.
	parentFile := filepath.Join(dir, "parentfile")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &WhiteboardStore{path: filepath.Join(parentFile, "sub", "whiteboard.json")}
	s.load()                           // init the empty entries map (file absent)
	s.Set("agent", "notes", "content") // triggers saveLocked → MkdirAll fails (swallowed)
	// The in-memory state is still updated even though the disk write failed.
	if snap := s.Snapshot(); snap.Entries["agent"].Sections["notes"].Content != "content" {
		t.Error("expected in-memory state updated despite MkdirAll failure")
	}
}

// TestWhiteboardStore_SaveLocked_WriteFails makes the .tmp path a directory.
func TestWhiteboardStore_SaveLocked_WriteFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whiteboard.json")
	if err := os.MkdirAll(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	s := &WhiteboardStore{path: path}
	s.load()                           // init the empty entries map
	s.Set("agent", "notes", "content") // saveLocked WriteFile fails (EISDIR)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected no whiteboard file after WriteFile failure")
	}
}

// TestWhiteboardStore_LoadCorruptStartsEmpty drives the json.Unmarshal error
// branch in load().
func TestWhiteboardStore_LoadCorruptStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whiteboard.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &WhiteboardStore{path: path}
	s.load()
	// Corrupt JSON → starts with an empty (non-nil) entries map.
	s.Set("a", "notes", "x") // must not panic; map was initialised
}

// TestWhiteboardStore_Append_DefaultSectionAndEmptyAgent drives the
// empty-agent no-op, the default-section fallback, and appending onto an
// empty previous section (no separator newline).
func TestWhiteboardStore_Append_Edges(t *testing.T) {
	dir := t.TempDir()
	s := &WhiteboardStore{path: filepath.Join(dir, "whiteboard.json")}
	s.load()
	// Empty agent → no-op.
	s.Append("", "notes", "ignored")
	// Default section (empty section name) + first append (empty prev).
	s.Append("agent", "", "first line")
	// Second append → newline-separated.
	s.Append("agent", "", "second line")

	snap := s.Snapshot()
	content := snap.Entries["agent"].Sections[defaultWhiteboardSection].Content
	if !strings.Contains(content, "first line") || !strings.Contains(content, "second line") {
		t.Errorf("expected both lines, got %q", content)
	}
	if !strings.Contains(content, "first line\nsecond line") {
		t.Errorf("expected newline separation, got %q", content)
	}
}
