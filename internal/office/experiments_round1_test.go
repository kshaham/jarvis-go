package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordExperiment_AppendsJSONLAndLoadsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rec := ExperimentRecord{
		Timestamp:      time.Now().UTC(),
		TeamTemplate:   "alpha",
		Mission:        "ship the thing",
		TaskCount:      5,
		CompletedCount: 4,
		AvgScore:       0.82,
		Duration:       "12m30s",
		TopPerformer:   "Vex",
		BottomPerformer: "Bolt",
	}
	if err := RecordExperiment(rec); err != nil {
		t.Fatalf("RecordExperiment: %v", err)
	}
	// RecordExperiment mirrors into the knowledge library from a
	// fire-and-forget goroutine that writes under HOME. Drain it before the
	// test returns so t.TempDir's RemoveAll doesn't race the goroutine's
	// write (which would otherwise produce a flaky "directory not empty").
	waitForExperimentMirror(t, home, rec.Timestamp)

	// File exists at the expected path.
	path := filepath.Join(home, ".jarvis", "office-memory", "experiments.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("experiments.jsonl should exist: %v", err)
	}

	// LoadExperimentHistory reads it back.
	hist := LoadExperimentHistory()
	if len(hist) != 1 {
		t.Fatalf("expected 1 record, got %d", len(hist))
	}
	got := hist[0]
	if got.TeamTemplate != "alpha" || got.TaskCount != 5 || got.CompletedCount != 4 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.TopPerformer != "Vex" || got.BottomPerformer != "Bolt" {
		t.Errorf("performer fields mismatch: %+v", got)
	}

	// A second append accumulates.
	rec2 := rec
	rec2.TeamTemplate = "beta"
	rec2.Timestamp = rec.Timestamp.Add(time.Second) // distinct library ID
	if err := RecordExperiment(rec2); err != nil {
		t.Fatalf("RecordExperiment 2: %v", err)
	}
	waitForExperimentMirror(t, home, rec2.Timestamp)
	if hist2 := LoadExperimentHistory(); len(hist2) != 2 {
		t.Fatalf("expected 2 records after second append, got %d", len(hist2))
	}
}

// waitForExperimentMirror blocks (up to a short deadline) until the
// library mirror goroutine spawned by RecordExperiment has written the
// experiment markdown for the given timestamp. This drains the
// fire-and-forget goroutine deterministically so t.TempDir cleanup can't
// race its write.
func waitForExperimentMirror(t *testing.T, home string, ts time.Time) {
	t.Helper()
	id := experimentIDFor(ts)
	mirror := filepath.Join(home, ".jarvis", "office-memory", "library", "experiments", id+".md")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(mirror); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Not fatal: the canonical jsonl write (the thing under test) already
	// succeeded synchronously. Log so a genuinely-missing mirror is visible.
	t.Logf("experiment mirror %s did not appear within deadline (canonical jsonl already verified)", mirror)
}

func TestLoadExperimentHistory_MissingFileEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := LoadExperimentHistory(); len(got) != 0 {
		t.Errorf("expected empty history for missing file, got %d", len(got))
	}
}

func TestLoadExperimentHistory_SkipsBlankAndCorruptLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Blank line + corrupt line + one valid line.
	content := "\n{not json}\n{\"teamTemplate\":\"gamma\",\"taskCount\":3}\n"
	if err := os.WriteFile(filepath.Join(dir, "experiments.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	hist := LoadExperimentHistory()
	if len(hist) != 1 {
		t.Fatalf("expected 1 valid record (blank+corrupt skipped), got %d", len(hist))
	}
	if hist[0].TeamTemplate != "gamma" || hist[0].TaskCount != 3 {
		t.Errorf("parsed record mismatch: %+v", hist[0])
	}
}

func TestBuildExperimentRecord_FromRosterState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-vex", "vex", cfg)
	m.SetTeamMission("build it well")
	agent := m.GetAgent("sess-vex")
	agent.AddTask(&Task{ID: "t1", Title: "done task", Status: TaskCompleted})
	agent.AddTask(&Task{ID: "t2", Title: "open task", Status: TaskInProgress})

	spawnedAt := time.Now().Add(-5 * time.Minute)
	rec := m.BuildExperimentRecord(spawnedAt)

	if rec.TaskCount != 2 {
		t.Errorf("TaskCount = %d, want 2", rec.TaskCount)
	}
	if rec.CompletedCount != 1 {
		t.Errorf("CompletedCount = %d, want 1", rec.CompletedCount)
	}
	if rec.Mission != "build it well" {
		t.Errorf("Mission = %q", rec.Mission)
	}
	if rec.Duration == "" {
		t.Error("Duration should be set when spawnedAt is non-zero")
	}
}

func TestBuildExperimentRecord_ZeroSpawnNoDuration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	rec := m.BuildExperimentRecord(time.Time{})
	if rec.Duration != "" {
		t.Errorf("zero spawnedAt should yield empty Duration, got %q", rec.Duration)
	}
	if rec.TaskCount != 0 {
		t.Errorf("empty roster should yield 0 tasks, got %d", rec.TaskCount)
	}
}

func TestExperimentIDFor_StableSlug(t *testing.T) {
	ts := time.Date(2026, 5, 30, 14, 5, 6, 0, time.UTC)
	if got := experimentIDFor(ts); got != "exp-20260530-140506" {
		t.Errorf("experimentIDFor = %q", got)
	}
}
