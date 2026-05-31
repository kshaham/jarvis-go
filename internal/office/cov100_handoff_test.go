package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withNoHome makes os.UserHomeDir fail by setting $HOME to empty, which
// the runtime treats as "$HOME is not defined". t.Setenv restores the
// prior value on cleanup, so this is safe under parallel-free tests.
func withNoHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "")
}

// TestHandoffsPath_NoHome forces os.UserHomeDir to fail by clearing the
// home env so handoffsPath returns "". On unix UserHomeDir reads $HOME.
func TestHandoffsPath_NoHome(t *testing.T) {
	withNoHome(t)
	if p := handoffsPath(); p != "" {
		t.Errorf("expected empty path with no HOME, got %q", p)
	}
}

// TestLoadHandoffStaleThreshold_NoHome hits the UserHomeDir-error branch.
func TestLoadHandoffStaleThreshold_NoHome(t *testing.T) {
	withNoHome(t)
	if d := loadHandoffStaleThreshold(); d != DefaultHandoffStale {
		t.Errorf("expected default stale threshold with no HOME, got %v", d)
	}
}

// TestLoadPersistedHandoffs_NoHome hits the p=="" early return.
func TestLoadPersistedHandoffs_NoHome(t *testing.T) {
	withNoHome(t)
	if got := loadPersistedHandoffs(); got != nil {
		t.Errorf("expected nil with no HOME, got %v", got)
	}
}

// TestSavePersistedHandoffs_NoHome hits the "no home dir" error branch.
func TestSavePersistedHandoffs_NoHome(t *testing.T) {
	withNoHome(t)
	err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}})
	if err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestSavePersistedHandoffs_MkdirAllFails makes office-memory's parent a
// regular file so MkdirAll(filepath.Dir(p)) fails.
func TestSavePersistedHandoffs_MkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create ~/.jarvis as a FILE so MkdirAll of .jarvis/office-memory fails.
	jarvis := filepath.Join(home, ".jarvis")
	if err := os.WriteFile(jarvis, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}})
	if err == nil {
		t.Fatal("expected MkdirAll error when .jarvis is a file")
	}
}

// TestSavePersistedHandoffs_WriteFails makes the tmp target path a
// directory so os.WriteFile fails.
func TestSavePersistedHandoffs_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create handoffs.json.tmp as a directory → WriteFile fails (EISDIR).
	if err := os.MkdirAll(filepath.Join(dir, "handoffs.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}})
	if err == nil {
		t.Fatal("expected WriteFile error when tmp path is a directory")
	}
}

// TestSavePersistedHandoffs_RenameFails makes the final target a
// non-empty directory so os.Rename(tmp, target) fails.
func TestSavePersistedHandoffs_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make handoffs.json a NON-EMPTY directory: rename of a file onto a
	// non-empty dir fails with ENOTEMPTY/EEXIST on every platform.
	target := filepath.Join(dir, "handoffs.json")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}})
	if err == nil {
		t.Fatal("expected Rename error when target is a non-empty directory")
	}
	// The tmp file should have been cleaned up.
	if _, statErr := os.Stat(target + ".tmp"); !os.IsNotExist(statErr) {
		t.Error("expected tmp file to be removed after rename failure")
	}
}

// TestLoadPersistedHandoffs_QuarantineRenameFails drives the
// quarantine-rename failure branch: handoffs.json is corrupt AND its parent
// dir is read-only so os.Rename to the .corrupt path fails.
func TestLoadPersistedHandoffs_QuarantineRenameFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir rename would still succeed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoffs.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	// Returns nil even when quarantine fails (best-effort).
	if got := loadPersistedHandoffs(); got != nil {
		t.Errorf("expected nil from corrupt handoffs, got %v", got)
	}
}

// TestAcceptHandoff_WrongReceiver verifies the "not your handoff" guard
// in AcceptHandoff (h.ToSessionID != receiverSessionID).
func TestAcceptHandoff_WrongReceiver(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	_, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx")
	if err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	_ = agentVex
	// Accept with a session that is neither sender nor receiver.
	_, err = m.AcceptHandoff("sess-someone-else", task.ID)
	if err == nil {
		t.Fatal("expected 'not your handoff' error")
	}
	if err.Error() != "not your handoff" {
		t.Errorf("expected 'not your handoff', got %v", err)
	}
}

// TestAcceptHandoff_NoPending exercises resolveHandoffPair's
// "no pending handoff for task" path.
func TestAcceptHandoff_NoPending(t *testing.T) {
	m, _, _, _ := setupTeamForHandoff(t)
	_, err := m.AcceptHandoff("sess-bolt", "no-such-task")
	if err == nil {
		t.Fatal("expected error for missing handoff")
	}
}

// TestRejectHandoff_WrongReceiver hits the RejectHandoff ownership guard.
func TestRejectHandoff_WrongReceiver(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	_, err := m.RejectHandoff("sess-not-receiver", task.ID, "nope")
	if err == nil || err.Error() != "not your handoff" {
		t.Fatalf("expected 'not your handoff', got %v", err)
	}
}

// TestForceRejectHandoff_NoPending hits ForceRejectHandoff's
// resolveHandoffPair error branch.
func TestForceRejectHandoff_NoPending(t *testing.T) {
	m, _, _, _ := setupTeamForHandoff(t)
	if _, err := m.ForceRejectHandoff("missing-task", "reason"); err == nil {
		t.Fatal("expected error for missing pending handoff")
	}
}

// TestForceRejectHandoff_Succeeds exercises the operator-auth reject path
// (finishReject) without a receiver session token.
func TestForceRejectHandoff_Succeeds(t *testing.T) {
	m, _, agentBolt, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	rejected, err := m.ForceRejectHandoff(task.ID, "operator override")
	if err != nil {
		t.Fatalf("ForceRejectHandoff: %v", err)
	}
	if rejected.Status != HandoffRejected {
		t.Errorf("expected rejected status, got %v", rejected.Status)
	}
	if rejected.RejectReason != "operator override" {
		t.Errorf("unexpected reason %q", rejected.RejectReason)
	}
	// Task must be back in-progress on the sender, handoff cleared.
	tasks := m.GetAgent("sess-vex").GetTasksCopy()
	if len(tasks) != 1 || tasks[0].Status != TaskInProgress || tasks[0].Handoff != nil {
		t.Errorf("expected task restored to in_progress with no handoff, got %+v", tasks)
	}
	_ = agentBolt
}

// TestCancelHandoff_NoLongerPending forces the "handoff no longer pending"
// branch by accepting before cancel.
func TestCancelHandoff_NoLongerPending(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	if _, err := m.AcceptHandoff("sess-bolt", task.ID); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}
	// Task moved to receiver; sender no longer has a pending handoff for it,
	// so resolveHandoffPair errors out.
	if _, err := m.CancelHandoff("sess-vex", task.ID); err == nil {
		t.Fatal("expected error cancelling an already-accepted handoff")
	}
}

// TestResolveHandoffPair_MissingAgent forces the "handoff references
// missing agent" branch by unregistering the receiver after a handoff is
// pending (the live task still references the gone receiver session).
func TestResolveHandoffPair_MissingReceiver(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	// Drop the receiver from the roster; the pending handoff still points
	// at sess-bolt as its ToSessionID.
	m.UnregisterAgent("sess-bolt")
	_, _, _, _, err := m.resolveHandoffPair(task.ID, "")
	if err == nil {
		t.Fatal("expected error when receiver agent is missing")
	}
}

// TestSweepStaleHandoffs_DefaultThreshold drives the threshold<=0 default
// branch and the "task already resolved" (target==nil) skip.
func TestSweepStaleHandoffs_DefaultThreshold(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	// Backdate the handoff so it is older than DefaultHandoffStale, and
	// remove the receiver so the sweep auto-rejects it.
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.Handoff != nil {
			tk.Handoff.CreatedAt = time.Now().Add(-DefaultHandoffStale - time.Hour)
		}
	}
	agentVex.mu.Unlock()
	m.UnregisterAgent("sess-bolt")

	// threshold<=0 → uses DefaultHandoffStale internally.
	swept := m.SweepStaleHandoffs(0)
	if swept != 1 {
		t.Fatalf("expected 1 swept handoff, got %d", swept)
	}
}

// TestHandoffStore_AuditCapFrontDrops appends more than HandoffAuditCap
// entries so the front-drop branch in (*handoffStore).append runs.
func TestHandoffStore_AuditCapFrontDrops(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestManager()
	store := m.ensureHandoffStore()
	for i := 0; i < HandoffAuditCap+5; i++ {
		store.append(HandoffRequest{ID: "h", Status: HandoffAccepted})
	}
	if got := len(store.snapshot()); got != HandoffAuditCap {
		t.Errorf("expected audit log capped at %d, got %d", HandoffAuditCap, got)
	}
}

// TestCancelHandoff_ProgressLogCap drives the taskProgressLogCap trim inside
// CancelHandoff by pre-filling the task's progress log.
func TestCancelHandoff_ProgressLogCap(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	// Fill the progress log to the cap before initiating the handoff.
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.ID == task.ID {
			for i := 0; i < taskProgressLogCap; i++ {
				tk.ProgressLog = append(tk.ProgressLog, TaskProgressEntry{Note: "n"})
			}
		}
	}
	agentVex.mu.Unlock()

	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	if _, err := m.CancelHandoff("sess-vex", task.ID); err != nil {
		t.Fatalf("CancelHandoff: %v", err)
	}
	tasks := m.GetAgent("sess-vex").GetTasksCopy()
	if len(tasks) != 1 || len(tasks[0].ProgressLog) > taskProgressLogCap {
		t.Errorf("expected progress log capped at %d, got %d", taskProgressLogCap, len(tasks[0].ProgressLog))
	}
}

// TestRejectHandoff_ProgressLogCap drives the cap trim in finishReject.
func TestRejectHandoff_ProgressLogCap(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.ID == task.ID {
			for i := 0; i < taskProgressLogCap; i++ {
				tk.ProgressLog = append(tk.ProgressLog, TaskProgressEntry{Note: "n"})
			}
		}
	}
	agentVex.mu.Unlock()

	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	if _, err := m.RejectHandoff("sess-bolt", task.ID, "no thanks"); err != nil {
		t.Fatalf("RejectHandoff: %v", err)
	}
	tasks := m.GetAgent("sess-vex").GetTasksCopy()
	if len(tasks[0].ProgressLog) > taskProgressLogCap {
		t.Errorf("expected progress log capped at %d, got %d", taskProgressLogCap, len(tasks[0].ProgressLog))
	}
}

// TestSweepStaleHandoffs_RecentSkipped drives the "too recent" continue
// branch: a fresh pending handoff is not swept.
func TestSweepStaleHandoffs_RecentSkipped(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	// Receiver is gone but the handoff was just created → too recent to sweep.
	m.UnregisterAgent("sess-bolt")
	if swept := m.SweepStaleHandoffs(time.Hour); swept != 0 {
		t.Errorf("expected 0 sweeps for a recent handoff, got %d", swept)
	}
}

// TestSweepStaleHandoffs_ProgressLogCap drives the cap trim in the sweep.
func TestSweepStaleHandoffs_ProgressLogCap(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.Handoff != nil {
			tk.Handoff.CreatedAt = time.Now().Add(-2 * time.Hour)
			for i := 0; i < taskProgressLogCap; i++ {
				tk.ProgressLog = append(tk.ProgressLog, TaskProgressEntry{Note: "n"})
			}
		}
	}
	agentVex.mu.Unlock()
	m.UnregisterAgent("sess-bolt")

	if swept := m.SweepStaleHandoffs(time.Minute); swept != 1 {
		t.Fatalf("expected 1 sweep, got %d", swept)
	}
	tasks := m.GetAgent("sess-vex").GetTasksCopy()
	if len(tasks[0].ProgressLog) > taskProgressLogCap {
		t.Errorf("expected progress log capped at %d, got %d", taskProgressLogCap, len(tasks[0].ProgressLog))
	}
}

// TestSweepStaleHandoffs_SenderGone skips handoffs whose SENDER has also
// left (sender==nil branch).
func TestSweepStaleHandoffs_SenderGone(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.Handoff != nil {
			tk.Handoff.CreatedAt = time.Now().Add(-2 * time.Hour)
		}
	}
	agentVex.mu.Unlock()
	// Remove BOTH ends. The pending handoff snapshot is captured by
	// ListPendingHandoffs before we drop the sender — but since we drop the
	// sender too, the sweep's sender lookup returns nil and skips.
	pending := m.ListPendingHandoffs()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending handoff, got %d", len(pending))
	}
	m.UnregisterAgent("sess-bolt")
	m.UnregisterAgent("sess-vex")
	swept := m.SweepStaleHandoffs(time.Minute)
	if swept != 0 {
		t.Errorf("expected 0 sweeps when sender is gone, got %d", swept)
	}
}
