package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStandupStateHasID(t *testing.T) {
	st := &StandupState{ID: "abc"}
	if st.ID != "abc" {
		t.Fatalf("ID field missing or wrong")
	}
}

func TestLatestCompletedStandupID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id string, completedAt int64) {
		p := PersistedStandup{ID: id, CompletedAt: completedAt, Transcript: []StandupMessage{{SessionID: "a", Type: "status_report", Content: "did work"}}}
		b, _ := json.MarshalIndent(p, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, id+".json"), b, 0o644)
	}
	write("older", 1000)
	write("newer", 2000)

	id, err := LatestCompletedStandupID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "newer" {
		t.Fatalf("latest = %q, want newer", id)
	}
	ps, err := LoadPersistedStandup("newer")
	if err != nil || ps.ID != "newer" {
		t.Fatalf("load failed: %v %+v", err, ps)
	}
}

// TestLatestCompletedStandupIDMtime verifies the mtime-based fast path (Fix 5a):
// the file with the highest CompletedAt is also given the newest mtime via
// os.Chtimes, and LatestCompletedStandupID must return it without parsing all files.
func TestLatestCompletedStandupIDMtime(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	writeWithMtime := func(id string, completedAt int64, mtime time.Time) {
		p := PersistedStandup{
			ID:          id,
			CompletedAt: completedAt,
			Transcript:  []StandupMessage{{SessionID: "a", Type: "status_report", Content: "did work"}},
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		path := filepath.Join(dir, id+".json")
		_ = os.WriteFile(path, b, 0o644)
		_ = os.Chtimes(path, mtime, mtime)
	}

	// Three standups; "middle" has highest CompletedAt AND newest mtime.
	writeWithMtime("oldest-file", 3000, base.Add(1*time.Second))
	writeWithMtime("middle", 9000, base.Add(3*time.Second)) // newest mtime + highest CompletedAt
	writeWithMtime("third", 500, base.Add(2*time.Second))

	id, err := LatestCompletedStandupID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "middle" {
		t.Fatalf("latest = %q, want middle", id)
	}
}

// TestLatestCompletedStandupIDCorruptFallback verifies that if the newest-mtime
// file is corrupt, LatestCompletedStandupID falls through to the next candidate
// (Fix 5a fallback).
func TestLatestCompletedStandupIDCorruptFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Write a valid standup with an older mtime.
	validPath := filepath.Join(dir, "valid.json")
	p := PersistedStandup{
		ID:          "valid",
		CompletedAt: 5000,
		Transcript:  []StandupMessage{{SessionID: "a", Type: "status_report", Content: "ok"}},
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	_ = os.WriteFile(validPath, b, 0o644)
	_ = os.Chtimes(validPath, base.Add(1*time.Second), base.Add(1*time.Second))

	// Write a corrupt file with a newer mtime so it sorts first.
	corruptPath := filepath.Join(dir, "corrupt.json")
	_ = os.WriteFile(corruptPath, []byte("not valid json {{{"), 0o644)
	_ = os.Chtimes(corruptPath, base.Add(2*time.Second), base.Add(2*time.Second))

	id, err := LatestCompletedStandupID()
	if err != nil {
		t.Fatalf("expected fallback to valid standup, got err: %v", err)
	}
	if id != "valid" {
		t.Fatalf("expected fallback id = valid, got %q", id)
	}
}
