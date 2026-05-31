package handler

// Coverage for the standup arm of collectTimelineEntries + the
// fmtStandupSummary/listPersistedStandups helpers. We drop a standup
// JSON file under the temp HOME (newTimelineContext already isolates
// HOME) and assert it surfaces in the merged timeline.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func writePersistedStandup(t *testing.T, s office.PersistedStandup) {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, s.ID+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestOfficeTimeline_StandupEntry(t *testing.T) {
	ctx := newTimelineContext(t)
	now := time.Now()
	writePersistedStandup(t, office.PersistedStandup{
		ID:               "su-1",
		Participants:     []string{"Vex", "Bolt"},
		CompletedAt:      now.UnixMilli(),
		RespondedCount:   2,
		ParticipantCount: 2,
	})

	req := httptest.NewRequest("GET", "/api/office/timeline?type=standup&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)

	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 standup entry, got %d", len(entries))
	}
	m := entries[0].(map[string]interface{})
	if m["kind"] != "standup" {
		t.Errorf("kind=%v, want standup", m["kind"])
	}
	// Summary comes from fmtStandupSummary: "2/2 responded".
	if m["summary"] != "2/2 responded" {
		t.Errorf("summary=%v, want '2/2 responded'", m["summary"])
	}
	if m["title"] != "Standup: Vex, Bolt" {
		t.Errorf("title=%v, want 'Standup: Vex, Bolt'", m["title"])
	}
}

func TestFmtStandupSummary(t *testing.T) {
	// Zero participants -> empty string.
	if got := fmtStandupSummary(office.PersistedStandup{ParticipantCount: 0}); got != "" {
		t.Errorf("expected empty summary for 0 participants, got %q", got)
	}
	// Populated -> "n/m responded".
	got := fmtStandupSummary(office.PersistedStandup{RespondedCount: 3, ParticipantCount: 5})
	if got != "3/5 responded" {
		t.Errorf("summary=%q, want '3/5 responded'", got)
	}
}

func TestListPersistedStandups_EmptyDir(t *testing.T) {
	// newTimelineContext isolates HOME to an empty temp dir, so with no
	// standups written the helper returns an empty slice (no panic).
	newTimelineContext(t)
	if got := listPersistedStandups(); len(got) != 0 {
		t.Errorf("expected 0 persisted standups, got %d", len(got))
	}
}

func TestOfficeTimeline_NilManager503(t *testing.T) {
	ctx := &Context{} // no OfficeManager
	req := httptest.NewRequest("GET", "/api/office/timeline", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when OfficeManager nil, got %d", rec.Code)
	}
}
