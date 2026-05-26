package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedExpiredExperience writes an experience and immediately stamps
// it with the given valid_until. Used by purge tests to simulate
// memify's decay action without running the full memify pass.
func seedExpiredExperience(t *testing.T, configID, taskID string, validUntil time.Time) {
	t.Helper()
	rec := ExperienceRecord{
		AgentConfigID:    configID,
		TaskID:           taskID,
		TaskTitle:        "old " + taskID,
		CompletionStatus: "completed",
		OutcomeScore:     0.5,
		StartedAt:        validUntil.Add(-24 * time.Hour),
		CompletedAt:      validUntil.Add(-24 * time.Hour),
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	// Append valid_until via the same mechanism memify uses.
	path := experiencePath(Dir(), configID, taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const fmEnd = "\n---\n"
	idx := strings.Index(string(data), fmEnd)
	if idx < 0 {
		t.Fatal("seeded file has no frontmatter")
	}
	stamped := string(data[:idx]) + "\nvalid_until: " + validUntil.UTC().Format(time.RFC3339) + string(data[idx:])
	if err := writeAtomic(path, []byte(stamped)); err != nil {
		t.Fatal(err)
	}
}

func TestPurge_EmptyLibraryNoOp(t *testing.T) {
	withTempLibrary(t)
	res, err := PurgeExpiredExperiences(PurgeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 || res.Scanned != 0 {
		t.Errorf("non-zero counters on empty library: %+v", res)
	}
}

func TestPurge_DeletesPastGracePeriod(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// valid_until = 60 days before now → 30-day grace expired.
	seedExpiredExperience(t, "vex", "old-task", now.Add(-60*24*time.Hour))

	path := filepath.Join(Dir(), "experiences", "vex", "old-task.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	res, err := PurgeExpiredExperiences(PurgeOptions{
		NowFunc: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted=%d want 1", res.Deleted)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after purge: %v", err)
	}
}

func TestPurge_GracePeriodProtectsRecentDecays(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// valid_until = 10 days before now → still inside 30-day grace.
	seedExpiredExperience(t, "vex", "recent-decay", now.Add(-10*24*time.Hour))

	res, err := PurgeExpiredExperiences(PurgeOptions{
		NowFunc: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 {
		t.Errorf("Deleted=%d want 0 (still in grace window)", res.Deleted)
	}
	path := filepath.Join(Dir(), "experiences", "vex", "recent-decay.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("recently-decayed file should be preserved: %v", err)
	}
}

func TestPurge_UnstampedFilesUntouched(t *testing.T) {
	// Experiences without valid_until are alive — memify hasn't
	// decayed them. Purge must NOT delete them.
	withTempLibrary(t)
	rec := ExperienceRecord{
		AgentConfigID:    "vex",
		TaskID:           "fresh",
		TaskTitle:        "Fresh task",
		CompletionStatus: "completed",
		OutcomeScore:     0.9,
		CompletedAt:      time.Now(),
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	res, _ := PurgeExpiredExperiences(PurgeOptions{})
	if res.Deleted != 0 {
		t.Errorf("Deleted=%d want 0 for unstamped file", res.Deleted)
	}
	path := filepath.Join(Dir(), "experiences", "vex", "fresh.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("unstamped file should be preserved: %v", err)
	}
}

func TestPurge_MaxPurgePerTickCapsDeletes(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// 10 expired files, cap to 3 per tick.
	for i := 0; i < 10; i++ {
		seedExpiredExperience(t, "vex", "t-"+itoaPurge(i),
			now.Add(-60*24*time.Hour))
	}

	res, _ := PurgeExpiredExperiences(PurgeOptions{
		NowFunc:         func() time.Time { return now },
		MaxPurgePerTick: 3,
	})
	if res.Deleted != 3 {
		t.Errorf("Deleted=%d want 3 (cap)", res.Deleted)
	}
	if res.Skipped != 7 {
		t.Errorf("Skipped=%d want 7", res.Skipped)
	}
}

func TestPurge_OnlyTouchesExperiences(t *testing.T) {
	// Workflows + anti-patterns + standups must be left alone.
	withTempLibrary(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Drop an expired-looking workflow + anti-pattern + standup
	// directly on disk to verify purge ignores them.
	for _, sub := range []string{"workflows", "anti-patterns", "standups"} {
		path := filepath.Join(Dir(), sub, "dummy.md")
		body := "---\nkind: workflow\nvalid_until: " +
			now.Add(-60*24*time.Hour).UTC().Format(time.RFC3339) +
			"\n---\n# x\n"
		if err := writeAtomic(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	_, _ = PurgeExpiredExperiences(PurgeOptions{
		NowFunc: func() time.Time { return now },
	})

	for _, sub := range []string{"workflows", "anti-patterns", "standups"} {
		path := filepath.Join(Dir(), sub, "dummy.md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s/dummy.md was deleted; purge should only touch experiences", sub)
		}
	}
}

func TestPurge_RobustToMalformedFrontmatter(t *testing.T) {
	// A file with garbage frontmatter shouldn't cause purge to
	// panic or count it as deleted — just skip it.
	withTempLibrary(t)
	path := filepath.Join(Dir(), "experiences", "vex", "garbage.md")
	body := "no frontmatter here, just text\n"
	if err := writeAtomic(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	res, err := PurgeExpiredExperiences(PurgeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 {
		t.Errorf("Deleted=%d want 0 (garbage file should be skipped)", res.Deleted)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("garbage file should be preserved: %v", err)
	}
}

// itoaPurge mirrors the existing test helper. Inlined here so the
// purge test file has no dependency on memify_test's seedExperience.
func itoaPurge(i int) string {
	return fmt.Sprintf("%d", i)
}
