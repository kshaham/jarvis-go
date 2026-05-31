package office

import (
	"os/exec"
	"testing"
	"time"
)

// TestSubmitBrainstormResponse_AllBranches covers the four exit branches:
// unknown agent, no pending slot, successful delivery, and a closed slot
// (late response).
func TestSubmitBrainstormResponse_AllBranches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// Unknown agent -> false.
	if m.SubmitBrainstormResponse("ghost", "x") {
		t.Error("expected false for unknown agent")
	}

	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	agent := m.GetAgent("sess-a")

	// No pending slot -> false.
	if m.SubmitBrainstormResponse("sess-a", "no slot") {
		t.Error("expected false with no pending brainstorm slot")
	}

	// Successful delivery.
	slot := newWaitSlot()
	agent.mu.Lock()
	agent.BrainstormSlot = slot
	agent.mu.Unlock()
	if !m.SubmitBrainstormResponse("sess-a", "great idea") {
		t.Fatal("expected delivery to succeed")
	}
	if got := <-slot.ch(); got != "great idea" {
		t.Errorf("slot got %q", got)
	}

	// Closed slot -> false, and the late-response counter advances.
	before := LateResponsesDropped()
	slot.finish()
	if m.SubmitBrainstormResponse("sess-a", "too late") {
		t.Error("expected false delivering to a finished slot")
	}
	if LateResponsesDropped() <= before {
		t.Error("expected late-response counter to advance for a closed slot")
	}
}

// TestSweepBugTasks_CapsAtMaxBugTasks exercises the cap-enforcement branch:
// with MaxBugTasks=2 and 4 bugs (mix of resolved/open), the oldest resolved
// then oldest open are dropped until within the limit.
func TestSweepBugTasks_CapsAtMaxBugTasks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBugTasks = 2
	m, _, _ := newCtrlManager(t, cfg)

	now := time.Now()
	// Resolved but recent (so the 24h sweep doesn't catch them) + open.
	m.AddBugTask(&BugTask{ID: "res-old", Status: "resolved", CreatedAt: now.Add(-2 * time.Hour)})
	m.AddBugTask(&BugTask{ID: "res-new", Status: "resolved", CreatedAt: now.Add(-1 * time.Hour)})
	m.AddBugTask(&BugTask{ID: "open-old", Status: "open", CreatedAt: now.Add(-3 * time.Hour)})
	m.AddBugTask(&BugTask{ID: "open-new", Status: "open", CreatedAt: now})

	swept := m.SweepBugTasks()
	if swept != 2 {
		t.Errorf("expected 2 bugs swept to enforce cap, got %d", swept)
	}
	remaining := map[string]bool{}
	for _, bt := range m.GetBugTasks() {
		remaining[bt.ID] = true
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining bugs, got %d (%v)", len(remaining), remaining)
	}
	// Oldest resolved must be removed first.
	if remaining["res-old"] {
		t.Error("oldest resolved bug should have been removed first")
	}
	// Newest open must survive.
	if !remaining["open-new"] {
		t.Error("newest open bug should survive the cap")
	}
}

// TestCheckDevInstanceHealth_IteratesAllSlots verifies the public wrapper
// walks every announced slot (only those with a URL). Using an unreachable
// URL drives the failure path through checkOneDevInstance for each.
func TestCheckDevInstanceHealth_IteratesAllSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// Two announced slots (unreachable) + one with no URL (skipped).
	if err := m.SetDevInstance("app", "http://127.0.0.1:1/", 0, true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDevInstance("api", "http://127.0.0.1:2/", 0, true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDevInstance("empty", "", 0, true); err != nil {
		t.Fatal(err)
	}

	// Should not panic and should record a failure on each reachable slot.
	m.CheckDevInstanceHealth()

	app, ok := m.GetDevInstance("app")
	if !ok || app.HealthFailures == 0 {
		t.Errorf("expected app slot to record a health failure, got %+v", app)
	}
	api, ok := m.GetDevInstance("api")
	if !ok || api.HealthFailures == 0 {
		t.Errorf("expected api slot to record a health failure, got %+v", api)
	}
}

// TestDevInstancePersistRoundTripWithCredentials verifies persistDevInstance
// (0600 mode for credentials) and loadDevInstances reload the slot in a
// fresh manager.
func TestDevInstancePersistRoundTripWithCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstance("app", "http://jarvis:3000", 12345, true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDevInstanceCredentials("app", "user:secret"); err != nil {
		t.Fatal(err)
	}

	// Fresh manager loads from disk in its constructor.
	m2 := newTestManager()
	inst, ok := m2.GetDevInstance("app")
	if !ok {
		t.Fatal("expected app dev instance to reload from disk")
	}
	if inst.URL != "http://jarvis:3000" || inst.PID != 12345 {
		t.Errorf("reloaded instance mismatch: %+v", inst)
	}
	if inst.Credentials != "user:secret" {
		t.Errorf("expected credentials reloaded, got %q", inst.Credentials)
	}
}

// TestClearAllDevInstances removes every slot.
func TestClearAllDevInstances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	_ = m.SetDevInstance("app", "http://jarvis:3000", 0, true)
	_ = m.SetDevInstance("api", "http://jarvis:4000", 0, true)

	m.ClearAllDevInstances(false)

	if got := len(m.ListDevInstances()); got != 0 {
		t.Errorf("expected all dev instances cleared, got %d", got)
	}
}

// TestPgrepChildren_NoChildren verifies pgrep with a parent that has no
// children returns an empty list (exit code 1 mapped to nil, nil).
func TestPgrepChildren_NoChildren(t *testing.T) {
	// A very high, almost-certainly-nonexistent PID. pgrep -P returns exit
	// status 1 (no matches), which the code maps to (nil, nil).
	children, err := pgrepChildren(999999)
	if err != nil {
		// If pgrep isn't installed the function returns an error; skip then.
		if _, lookErr := exec.LookPath("pgrep"); lookErr != nil {
			t.Skip("pgrep not available on PATH")
		}
		t.Fatalf("unexpected error from pgrepChildren: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected no children for a nonexistent PID, got %v", children)
	}
}

// TestCollectDescendants_LeafProcess returns an empty descendant list for a
// PID with no children.
func TestCollectDescendants_LeafProcess(t *testing.T) {
	if _, lookErr := exec.LookPath("pgrep"); lookErr != nil {
		t.Skip("pgrep not available on PATH")
	}
	got := collectDescendants(999999)
	if len(got) != 0 {
		t.Errorf("expected no descendants for a leaf/nonexistent PID, got %v", got)
	}
}

// TestKillProcessTree_GuardsLowPIDs verifies the low-PID guard returns early
// without signaling anything (PIDs < 100 are system processes we never touch).
func TestKillProcessTree_GuardsLowPIDs(t *testing.T) {
	// Must not panic or signal anything for a guarded PID.
	killProcessTree(1)
	killProcessTree(99)
	killProcessTree(0)
}

// TestPgrepChildren_StubbedOutput exercises the parse path with a stubbed
// execCommand that returns a fixed PID list.
func TestPgrepChildren_StubbedOutput(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		// `echo` prints the PID list pgrep would return.
		return exec.Command("printf", "%s", "111\n222\n\nbogus\n333\n")
	}
	pids, err := pgrepChildren(1234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{111, 222, 333}
	if len(pids) != len(want) {
		t.Fatalf("expected %v, got %v", want, pids)
	}
	for i := range want {
		if pids[i] != want[i] {
			t.Errorf("pid[%d] = %d, want %d", i, pids[i], want[i])
		}
	}
}
