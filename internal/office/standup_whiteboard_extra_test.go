package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CompleteStandup / StopStandup / StandupAudioPath
// ---------------------------------------------------------------------------

// TestCompleteStandup_MarksInactiveAndBroadcasts verifies CompleteStandup
// (an alias for StopStandup) marks the active standup inactive and emits
// office:standupEnded.
func TestCompleteStandup_MarksInactiveAndBroadcasts(t *testing.T) {
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.SetStandup(&StandupState{ID: "su-1", Active: true})

	m.CompleteStandup()

	got := m.GetStandup()
	if got == nil || got.Active {
		t.Errorf("expected standup marked inactive, got %+v", got)
	}
	if !rec.has("office:standupEnded") {
		t.Error("expected office:standupEnded broadcast")
	}
}

// TestStopStandup_NilStandupSafe verifies StopStandup is a safe no-op (no
// panic) when there is no active standup, and still broadcasts the end.
func TestStopStandup_NilStandupSafe(t *testing.T) {
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.StopStandup() // no standup set
	if !rec.has("office:standupEnded") {
		t.Error("expected office:standupEnded broadcast even with no active standup")
	}
}

// TestStandupAudioPath verifies the audio path is the standup dir + id.mp3.
func TestStandupAudioPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := StandupAudioPath("su-42")
	if !strings.HasSuffix(got, filepath.Join("su-42.mp3")) {
		t.Errorf("expected path ending in su-42.mp3, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// WhiteboardStore.load resilience
// ---------------------------------------------------------------------------

// TestWhiteboardStore_LoadMalformedFileStartsEmpty verifies that a corrupt
// whiteboard.json results in an empty (non-nil) store rather than a crash.
func TestWhiteboardStore_LoadMalformedFileStartsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whiteboard.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := NewWhiteboardStore()
	snap := store.Snapshot()
	if snap.Entries == nil {
		t.Error("expected non-nil Entries map after loading a malformed file")
	}
	if len(snap.Entries) != 0 {
		t.Errorf("expected empty entries after malformed load, got %d", len(snap.Entries))
	}

	// And the store is still usable: a Set + Snapshot round-trips.
	store.Set("Vex", "blockers", "waiting on review")
	snap2 := store.Snapshot()
	entry, ok := snap2.Entries["Vex"]
	if !ok {
		t.Fatal("expected Vex entry after Set")
	}
	if entry.Sections["blockers"].Content != "waiting on review" {
		t.Errorf("unexpected section content: %+v", entry.Sections)
	}
}
