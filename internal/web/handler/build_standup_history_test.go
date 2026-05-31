package handler

// Round-4 branch fills for buildStandupHistory's residual arms:
//   - ReadDir error -> empty slice (1842-1844)
//   - non-.json / subdir entries skipped (1853-1854)
//   - per-standup LoadPersistedStandup parse error skipped (1875-1876)
//   - the >maxStandupHistoryEntries cap (1867-1869)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestBuildStandupHistory_ReadDirErrorReturnsEmpty(t *testing.T) {
	t.Parallel()
	// A path that does not exist -> os.ReadDir errors -> empty result.
	got := buildStandupHistory(filepath.Join(t.TempDir(), "does-not-exist"))
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for a missing dir, got %d", len(got))
	}
}

func TestBuildStandupHistory_SkipsNonJSONCorruptAndSubdirs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 1) Valid standup -> survives.
	valid := office.PersistedStandup{
		ID:           "valid-standup",
		Participants: []string{"alice"},
		StartedAt:    time.Now().Add(-time.Hour).UnixMilli(),
		CompletedAt:  time.Now().UnixMilli(),
		Transcript:   []office.StandupMessage{{SessionID: "alice", Content: "hi"}},
	}
	vd, _ := json.Marshal(valid)
	if err := os.WriteFile(filepath.Join(dir, valid.ID+".json"), vd, 0o644); err != nil {
		t.Fatalf("write valid: %v", err)
	}

	// 2) Corrupt .json -> LoadPersistedStandup errors -> skipped (1875-1876).
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	// 3) Non-.json file -> skipped by the suffix guard (1853-1854).
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	// 4) Subdirectory -> skipped by the IsDir guard (1853-1854).
	if err := os.MkdirAll(filepath.Join(dir, "subdir.json"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	got := buildStandupHistory(dir)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 valid standup (corrupt/txt/subdir skipped), got %d: %+v", len(got), got)
	}
	if got[0].ID != "valid-standup" {
		t.Errorf("expected valid-standup, got %q", got[0].ID)
	}
	if got[0].MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1", got[0].MessageCount)
	}
	if got[0].EndedAt == "" {
		t.Error("expected EndedAt set for a completed standup")
	}
}

func TestBuildStandupHistory_CapsAtMaxEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Seed more than maxStandupHistoryEntries valid standups so the cap arm
	// (jsonFiles[:maxStandupHistoryEntries]) trims the parse set.
	total := maxStandupHistoryEntries + 10
	base := time.Now().Add(-time.Duration(total) * time.Minute)
	for i := 0; i < total; i++ {
		id := "standup-" + strconv.Itoa(i)
		ps := office.PersistedStandup{
			ID:          id,
			StartedAt:   base.Add(time.Duration(i) * time.Minute).UnixMilli(),
			CompletedAt: base.Add(time.Duration(i)*time.Minute + 30*time.Second).UnixMilli(),
		}
		d, _ := json.Marshal(ps)
		p := filepath.Join(dir, id+".json")
		if err := os.WriteFile(p, d, 0o644); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
		// Stagger mtimes so the newest-first selection is deterministic.
		mt := base.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, mt, mt)
	}

	got := buildStandupHistory(dir)
	if len(got) != maxStandupHistoryEntries {
		t.Fatalf("expected cap of %d entries, got %d", maxStandupHistoryEntries, len(got))
	}
}
