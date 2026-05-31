package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseTaskLine
// ---------------------------------------------------------------------------

func TestParseTaskLine_PriorityTitleDescription(t *testing.T) {
	got := parseTaskLine("- [HIGH] Wire up auth | uses jwt middleware")
	if got == nil {
		t.Fatal("expected a parsed task line")
	}
	if got.Priority != "high" {
		t.Errorf("priority=%q want high", got.Priority)
	}
	if got.Title != "Wire up auth" {
		t.Errorf("title=%q want 'Wire up auth'", got.Title)
	}
	if got.Description != "uses jwt middleware" {
		t.Errorf("desc=%q want 'uses jwt middleware'", got.Description)
	}
}

func TestParseTaskLine_NoDescriptionDefaultsMediumWhenNoBracket(t *testing.T) {
	got := parseTaskLine("- Just a title")
	if got == nil {
		t.Fatal("expected parsed line")
	}
	if got.Priority != "medium" {
		t.Errorf("expected default medium priority, got %q", got.Priority)
	}
	if got.Title != "Just a title" || got.Description != "" {
		t.Errorf("unexpected title/desc: %q / %q", got.Title, got.Description)
	}
}

func TestParseTaskLine_EmptyTitleReturnsNil(t *testing.T) {
	// A bracket with no title text after it leaves an empty title, so the
	// parser returns nil.
	if got := parseTaskLine("- [HIGH] "); got != nil {
		t.Errorf("expected nil for empty title, got %+v", got)
	}
	// A bare "-" with a leading pipe split also yields an empty title.
	if got := parseTaskLine("-  | x"); got != nil {
		t.Errorf("expected nil for empty title (pipe form), got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// parseLeaderTaskResponse
// ---------------------------------------------------------------------------

func TestParseLeaderTaskResponse_GroupsByConfigID(t *testing.T) {
	resp := `Here are the assignments:
TASKS_FOR:vex
- [HIGH] Build login | with validation
- [LOW] Polish CSS
TASKS_FOR:bolt
- [MEDIUM] Add API endpoint | REST
Some trailing chatter that isn't a task.`

	got := parseLeaderTaskResponse(resp)

	if len(got["vex"]) != 2 {
		t.Errorf("expected 2 tasks for vex, got %d (%+v)", len(got["vex"]), got["vex"])
	}
	if len(got["bolt"]) != 1 {
		t.Errorf("expected 1 task for bolt, got %d", len(got["bolt"]))
	}
	if got["vex"][0].Title != "Build login" || got["vex"][0].Priority != "high" {
		t.Errorf("unexpected first vex task: %+v", got["vex"][0])
	}
	if got["bolt"][0].Priority != "medium" {
		t.Errorf("unexpected bolt task priority: %+v", got["bolt"][0])
	}
}

func TestParseLeaderTaskResponse_TaskBeforeMarkerIgnored(t *testing.T) {
	resp := `- [HIGH] orphan task with no TASKS_FOR marker
TASKS_FOR:vex
- [HIGH] real one`
	got := parseLeaderTaskResponse(resp)
	if len(got) != 1 {
		t.Fatalf("expected only the vex group, got %d groups", len(got))
	}
	if len(got["vex"]) != 1 || got["vex"][0].Title != "real one" {
		t.Errorf("unexpected vex tasks: %+v", got["vex"])
	}
}

func TestParseLeaderTaskResponse_EmptyResponse(t *testing.T) {
	got := parseLeaderTaskResponse("")
	if len(got) != 0 {
		t.Errorf("expected empty map for empty response, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// durationMillis
// ---------------------------------------------------------------------------

func TestDurationMillis(t *testing.T) {
	cases := map[string]int64{
		"":      0,
		"1s":    1000,
		"2m":    120000,
		"bogus": 0,
	}
	for in, want := range cases {
		if got := durationMillis(in); got != want {
			t.Errorf("durationMillis(%q)=%d want %d", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Telegram config getters/setters (without spawning the update goroutine)
// ---------------------------------------------------------------------------

// TestTelegramConfig_RoundTripNoGoroutine verifies enable/interval flags
// round-trip and the 10-minute interval floor is enforced. No team spawned
// means the periodic-update goroutine is never started, so this is leak-free.
func TestTelegramConfig_RoundTripNoGoroutine(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	enabled, interval, _ := m.GetTelegramConfig()
	if enabled {
		t.Error("expected telegram disabled by default")
	}

	// Below-floor interval clamps to 10 minutes; no team => no goroutine.
	m.SetTelegramConfig(true, 2)
	enabled, interval, _ = m.GetTelegramConfig()
	if !enabled {
		t.Error("expected telegram enabled after SetTelegramConfig(true)")
	}
	if interval != 10 {
		t.Errorf("expected interval floored to 10 minutes, got %d", interval)
	}

	// Disabling is a clean no-op for the goroutine (it was never started).
	m.SetTelegramConfig(false, 30)
	enabled, _, _ = m.GetTelegramConfig()
	if enabled {
		t.Error("expected telegram disabled after SetTelegramConfig(false)")
	}
}

// ---------------------------------------------------------------------------
// LoadExperimentHistory / RecordExperiment round trip
// ---------------------------------------------------------------------------

// TestLoadExperimentHistory_ParsesJSONL verifies LoadExperimentHistory
// parses a JSONL log (skipping blank lines and malformed records) and that
// a missing log yields an empty (non-nil) slice. We write the JSONL file
// directly to avoid RecordExperiment's async library mirror, which writes
// after the test and would race t.TempDir() cleanup.
func TestLoadExperimentHistory_ParsesJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Missing file -> empty slice.
	if got := LoadExperimentHistory(); len(got) != 0 {
		t.Errorf("expected empty history for missing log, got %d", len(got))
	}

	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec := ExperimentRecord{
		Timestamp:    time.Now(),
		TeamTemplate: "alpha",
		Mission:      "ship it",
		TaskCount:    5,
		TopPerformer: "Vex",
	}
	data, _ := json.Marshal(rec)
	// One valid record, one blank line, one malformed line (skipped).
	content := string(data) + "\n\n{not valid json}\n"
	if err := os.WriteFile(filepath.Join(dir, "experiments.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := LoadExperimentHistory()
	if len(got) != 1 {
		t.Fatalf("expected 1 valid record (blank + malformed skipped), got %d", len(got))
	}
	if got[0].TeamTemplate != "alpha" || got[0].TaskCount != 5 || got[0].TopPerformer != "Vex" {
		t.Errorf("parse mismatch: %+v", got[0])
	}
}
