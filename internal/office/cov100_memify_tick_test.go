package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// TestRunMemifyTick_MemifyError drives the RunMemify error-return
// branch (memify_tick.go line ~78). RunMemify errors when the
// experiences path exists but is NOT a directory (ENOTDIR, which is
// not os.IsNotExist). The tick must log + return early without
// reaching the ledger/promotion/purge stages, so no broadcast fires.
func TestRunMemifyTick_MemifyError(t *testing.T) {
	root := t.TempDir()
	// Plant a FILE where the experiences directory is expected.
	if err := os.WriteFile(filepath.Join(root, "experiences"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)

	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	m.runMemifyTick()

	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("no promotion broadcast expected when memify errors; events=%v", broker.topics())
		}
	}
}

// TestRunMemifyTick_LedgerNil drives the `ledger == nil` early-return
// branch (memify_tick.go line ~98). OpenLedger returns (nil, nil) when
// LedgerPath() is empty, which happens when library.Dir() is "". With
// Dir() empty, RunMemify returns (empty, nil) (no error), so we proceed
// to OpenLedger and hit the nil branch.
func TestRunMemifyTick_LedgerNil(t *testing.T) {
	restore := library.SetDirForTest("")
	t.Cleanup(restore)

	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	m.runMemifyTick()

	if len(broker.topics()) != 0 {
		t.Errorf("expected no broadcasts with empty library dir, got %v", broker.topics())
	}
}

// TestRunMemifyTick_PromotionFailed drives the `len(pres.Failed) > 0`
// warning branch (memify_tick.go line ~115). We seed a ledger entry
// that clears the promotion threshold (ReuseCount >= 5, AvgOutcome >=
// 0.75) and points at a workflow path whose file does NOT exist on
// disk, so RunPromotion's os.ReadFile fails and the entry lands in
// pres.Failed without anything getting promoted.
func TestRunMemifyTick_PromotionFailed(t *testing.T) {
	libRoot, skillsRoot := withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	// Ledger entry referencing a workflow file that does not exist.
	ledger, _ := library.OpenLedger()
	for i := 0; i < 6; i++ {
		if err := ledger.Increment("workflows/ghost.md", 0.90); err != nil {
			t.Fatal(err)
		}
	}
	// Sanity: ensure the workflow file is genuinely absent.
	if _, err := os.Stat(filepath.Join(libRoot, "workflows", "ghost.md")); !os.IsNotExist(err) {
		t.Fatalf("precondition: ghost.md should not exist, err=%v", err)
	}

	m.runMemifyTick()

	// No skill should have been promoted (the read failed).
	if _, err := os.Stat(filepath.Join(skillsRoot, "ghost", "SKILL.md")); err == nil {
		t.Errorf("ghost workflow should NOT have been promoted")
	}
	// And no auto-promote broadcast.
	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("unexpected promotion broadcast for a failed entry; events=%v", broker.topics())
		}
	}
}

// TestRunMemifyTick_PurgeDeletesExpired drives the purge log branch
// (memify_tick.go line ~126: purged.Deleted > 0). We seed one
// experience file stamped with a valid_until far enough in the past
// that it's beyond the 30-day grace window, so purge deletes it.
func TestRunMemifyTick_PurgeDeletesExpired(t *testing.T) {
	libRoot, _ := withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	// Write an experience file with a valid_until well past the grace
	// window (default grace = 30 days). 60 days ago + 30-day grace is
	// still in the past, so it qualifies for deletion.
	expDir := filepath.Join(libRoot, "experiences", "vex")
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		t.Fatal(err)
	}
	validUntil := time.Now().Add(-60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := "---\n" +
		"task_id: t-old\n" +
		"agent_role: backend\n" +
		"valid_until: " + validUntil + "\n" +
		"---\n\n## Approach\nstale.\n"
	expFile := filepath.Join(expDir, "t-old.md")
	if err := os.WriteFile(expFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	m.runMemifyTick()

	// The expired experience must be gone.
	if _, err := os.Stat(expFile); !os.IsNotExist(err) {
		t.Errorf("expired experience should have been purged, stat err=%v", err)
	}
}
