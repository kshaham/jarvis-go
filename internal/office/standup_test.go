package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
