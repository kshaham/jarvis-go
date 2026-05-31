package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPersistStandup_MkdirFails makes the standups parent a file so
// MkdirAll fails.
func TestPersistStandup_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mem := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mem, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	// Should log+return without panic; no file produced.
	m.persistStandup("s1", []string{"a"}, []StandupMessage{
		{Type: "status_report", SpeakerName: "A", Content: "done", TimedOut: false},
	}, time.Now())
}

// TestPersistStandup_RoundTripWithTimeout drives the timed-out count branch
// and round-trips the persisted standup back via LoadPersistedStandup.
func TestPersistStandup_RoundTripWithTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	transcript := []StandupMessage{
		{Type: "status_report", SpeakerName: "A", Content: "made progress", TimedOut: false},
		{Type: "status_report", SpeakerName: "B", Content: "(offline - skipped)", TimedOut: true},
		{Type: "opening", SpeakerName: "Jarvis", Content: "start"}, // non-status_report skipped
	}
	m.persistStandup("s-timeout", []string{"a", "b"}, transcript, time.Now())

	got, err := LoadPersistedStandup("s-timeout")
	if err != nil {
		t.Fatalf("LoadPersistedStandup: %v", err)
	}
	if got.RespondedCount != 1 {
		t.Errorf("expected 1 responded, got %d", got.RespondedCount)
	}
	if got.TimedOutCount != 1 {
		t.Errorf("expected 1 timed out, got %d", got.TimedOutCount)
	}
}

// TestBuildTelegramStandupSummary_EmptySpeakerAndTruncate drives the
// empty-speaker skip and the >200-char truncation.
func TestBuildTelegramStandupSummary_EmptySpeakerAndTruncate(t *testing.T) {
	transcript := []StandupMessage{
		{Type: "status_report", SpeakerName: "", Content: "ignored"}, // empty speaker skipped
		{Type: "status_report", SpeakerName: "Long", Content: strings.Repeat("x", 250)},
		{Type: "status_report", SpeakerName: "NoResp", Content: ""}, // becomes "(no response)"
		{Type: "opening", SpeakerName: "J", Content: "start"},        // non-status_report skipped
	}
	out := buildTelegramStandupSummary("s1", []string{"a", "b", "c"}, transcript)
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation ellipsis, got %q", out)
	}
	if !strings.Contains(out, "(no response)") {
		t.Errorf("expected (no response) fallback, got %q", out)
	}
	if strings.Contains(out, "ignored") {
		t.Error("expected empty-speaker line to be skipped")
	}
}

// TestLatestCompletedStandupID_SkipsNonJsonAllCorrupt drives the non-.json
// skip and the all-corrupt → ErrNotExist fall-through.
func TestLatestCompletedStandupID_AllCorrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-.json file (skipped) and a corrupt .json (parse fails).
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestCompletedStandupID(); err != os.ErrNotExist {
		t.Fatalf("expected ErrNotExist when all candidates corrupt, got %v", err)
	}
}
