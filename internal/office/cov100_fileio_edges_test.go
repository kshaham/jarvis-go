package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestListSnapshots_CorruptAndUnreadableEntries drives two branches in
// ListSnapshots: the os.ReadFile error continue (snapshot.go ~203-204,
// a <name>.json entry that's actually a directory) and the
// json.Unmarshal-failure path that still emits a Name-only summary
// (~220-222, a valid-named .json holding corrupt JSON).
func TestListSnapshots_CorruptAndUnreadableEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 1) A directory named like a snapshot file => os.ReadFile errors
	//    (EISDIR) and the entry is skipped.
	if err := os.MkdirAll(filepath.Join(dir, "dirsnap.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 2) A valid-named .json with corrupt JSON => Unmarshal fails =>
	//    Name-only summary is appended.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	// The corrupt entry contributes a Name-only summary; the directory
	// entry is skipped. So exactly one summary, named "corrupt".
	var sawCorrupt bool
	for _, s := range list {
		if s.Name == "corrupt" {
			sawCorrupt = true
		}
		if s.Name == "dirsnap" {
			t.Error("directory-named .json entry should have been skipped")
		}
	}
	if !sawCorrupt {
		t.Errorf("expected a Name-only summary for the corrupt snapshot, got %+v", list)
	}
}

// TestDeleteSnapshot_RemoveError drives the os.Remove non-IsNotExist
// error branch (snapshot.go ~257-259): a NON-EMPTY directory named
// <name>.json can't be removed by os.Remove, so the error surfaces.
func TestDeleteSnapshot_RemoveError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := snapshotDir()
	target := filepath.Join(dir, "stuck.json")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make it non-empty so os.Remove fails with ENOTEMPTY.
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSnapshot("stuck"); err == nil {
		t.Fatal("expected remove error for a non-empty directory")
	}
}

// TestDeleteSnapshot_MissingIsNoError verifies removing an absent
// snapshot is treated as success (IsNotExist swallowed).
func TestDeleteSnapshot_MissingIsNoError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := DeleteSnapshot("never-existed"); err != nil {
		t.Errorf("deleting a missing snapshot should be a no-op, got %v", err)
	}
}

// TestPersistStandup_WriteTmpFails drives the os.WriteFile(tmp) error
// branch (standup.go ~489-492): planting a DIRECTORY at the tmp path
// makes the write fail.
func TestPersistStandup_WriteTmpFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a directory at <id>.json.tmp so WriteFile fails (EISDIR).
	if err := os.MkdirAll(filepath.Join(dir, "s-wt.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Must log + return without panic. No final file is produced.
	m.persistStandup("s-wt", []string{"a"}, []StandupMessage{
		{Type: "status_report", SpeakerName: "A", Content: "done"},
	}, time.Now())
	if _, err := os.Stat(filepath.Join(dir, "s-wt.json")); !os.IsNotExist(err) {
		t.Errorf("no final standup file should exist on write failure, err=%v", err)
	}
}

// TestPersistStandup_RenameFails drives the os.Rename error branch
// (standup.go ~493-497): planting a DIRECTORY at the final
// <id>.json path makes the rename fail (can't overwrite a dir with a
// file).
func TestPersistStandup_RenameFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Final target is a non-empty directory => Rename(tmp -> dir) fails.
	finalDir := filepath.Join(dir, "s-rn.json")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Must log + return without panic.
	m.persistStandup("s-rn", []string{"a"}, []StandupMessage{
		{Type: "status_report", SpeakerName: "A", Content: "done"},
	}, time.Now())
}

// TestWhiteboardStore_LoadNilEntriesMap drives the `s.data.Entries ==
// nil` re-initialization branch in load() (whiteboard_store.go ~80-82):
// a valid JSON object that omits "entries" unmarshals with a nil
// Entries map, which load() must re-initialize.
func TestWhiteboardStore_LoadNilEntriesMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whiteboard.json")
	// Valid JSON, but no "entries" key => Entries unmarshals to nil.
	if err := os.WriteFile(path, []byte(`{"updatedAt":"2026-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &WhiteboardStore{path: path}
	s.load()
	// Set must not panic — load() should have re-initialized the map.
	s.Set("agent", "notes", "hello")
	snap := s.Snapshot()
	if snap.Entries == nil {
		t.Fatal("Entries map should be non-nil after load")
	}
	if snap.Entries["agent"].Sections["notes"].Content != "hello" {
		t.Errorf("expected stored content, got %+v", snap.Entries["agent"])
	}
}
