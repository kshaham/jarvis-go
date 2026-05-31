package office

import (
	"testing"
	"time"
)

// TestReassignSessionID_EdgeCases drives every guard return.
func TestReassignSessionID_EdgeCases(t *testing.T) {
	m := newTestManager()
	// Empty / identical args → false.
	if m.ReassignSessionID("", "new") {
		t.Error("expected false for empty old id")
	}
	if m.ReassignSessionID("old", "old") {
		t.Error("expected false for identical ids")
	}
	// Old session not registered → false.
	if m.ReassignSessionID("ghost", "new") {
		t.Error("expected false when old session absent")
	}
	// Collision: new id already taken → false.
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A"})
	m.RegisterAgent("sess-b", "b", &TeamMemberConfig{ID: "b", Name: "B"})
	if m.ReassignSessionID("sess-a", "sess-b") {
		t.Error("expected false on collision with existing session id")
	}
	// Happy path.
	if !m.ReassignSessionID("sess-a", "sess-a2") {
		t.Error("expected reassignment to succeed")
	}
	if m.GetAgent("sess-a2") == nil || m.GetAgent("sess-a") != nil {
		t.Error("expected agent moved to new session id")
	}
}

// TestGetBugTasks_SkipsNil injects a nil bug task so the nil-skip branch
// runs.
func TestGetBugTasks_SkipsNil(t *testing.T) {
	m := newTestManager()
	m.AddBugTask(&BugTask{ID: "bug-real", Status: "open"})
	m.mu.Lock()
	m.bugTasks["bug-nil"] = nil // inject nil
	m.mu.Unlock()
	got := m.GetBugTasks()
	if len(got) != 1 || got[0].ID != "bug-real" {
		t.Errorf("expected only the real bug task, got %v", got)
	}
}

// TestSetDevInstance_InvalidName covers the validDevSlotName guard.
func TestSetDevInstance_InvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstance("../bad slot/name", "http://x", 0, false); err == nil {
		t.Fatal("expected invalid-slot-name error")
	}
}

// TestSetDevInstance_NegativePIDClamped covers the pid<0 clamp.
func TestSetDevInstance_NegativePIDClamped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstance("app", "http://jarvis:3000", -5, false); err != nil {
		t.Fatal(err)
	}
	inst, ok := m.GetDevInstance("app")
	if !ok || inst.PID != 0 {
		t.Errorf("expected pid clamped to 0, got %+v", inst)
	}
}

// TestSetDevInstance_PortConflict covers the port-conflict guard and the
// --force bypass.
func TestSetDevInstance_PortConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstance("app", "http://jarvis:3000", 0, false); err != nil {
		t.Fatal(err)
	}
	// Second slot on the same port without --force → conflict error.
	if err := m.SetDevInstance("api", "http://jarvis:3000", 0, false); err == nil {
		t.Fatal("expected port conflict error")
	}
	// With --force → allowed.
	if err := m.SetDevInstance("api", "http://jarvis:3000", 0, true); err != nil {
		t.Fatalf("expected --force to bypass conflict, got %v", err)
	}
	inst, _ := m.GetDevInstance("api")
	if !inst.ForceAnnounced {
		t.Error("expected ForceAnnounced=true after --force announce")
	}
}

// TestSetDevInstance_LocalhostWarn covers the localhost-warn branch (no
// error, just a log).
func TestSetDevInstance_LocalhostWarn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstance("app", "http://localhost:3000", 0, false); err != nil {
		t.Fatal(err)
	}
}

// TestSetDevInstanceCredentials_InvalidName covers the slot-name guard.
func TestSetDevInstanceCredentials_InvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstanceCredentials("bad name!!", "creds"); err == nil {
		t.Fatal("expected invalid-name error")
	}
}

// TestSetDevInstanceCredentials_CreatesSlot covers the creds-only slot
// creation branch (no URL yet).
func TestSetDevInstanceCredentials_CreatesSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if err := m.SetDevInstanceCredentials("preseed", "user:pw"); err != nil {
		t.Fatal(err)
	}
	inst, ok := m.GetDevInstance("preseed")
	if !ok || inst.Credentials != "user:pw" {
		t.Errorf("expected creds-only slot created, got %+v", inst)
	}
}

// TestGetDevInstance_Absent covers the not-found return.
func TestGetDevInstance_Absent(t *testing.T) {
	m := newTestManager()
	if _, ok := m.GetDevInstance("nope"); ok {
		t.Error("expected absent slot to return false")
	}
}

// TestHandleAgentDeath_DoubleClaimNoop verifies the second HandleAgentDeath
// for the same session is a no-op (ClaimDeath returns false).
func TestHandleAgentDeath_DoubleClaim(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-d", "d", &TeamMemberConfig{ID: "d", Name: "D", Tier: "worker"})
	agent := m.GetAgent("sess-d")
	// Claim death once directly so the next HandleAgentDeath sees it claimed.
	if !agent.ClaimDeath() {
		t.Fatal("expected first ClaimDeath to succeed")
	}
	// HandleAgentDeath should return early without unregistering (because we
	// pre-claimed; the side-effect path is skipped).
	m.HandleAgentDeath("sess-d")
	if m.GetAgent("sess-d") == nil {
		t.Error("expected agent to remain (death side effects skipped after pre-claim)")
	}
}

// TestHandleAgentDeath_OtherLeadAliveKeepsDevInstance covers the
// otherLeadAlive=true branch: a second strategic agent exists, so the dead
// lead's dev instance is NOT cleared.
func TestHandleAgentDeath_OtherLeadAlive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-lead1", "lead1", &TeamMemberConfig{ID: "lead1", Name: "Lead1", Tier: "strategic"})
	m.RegisterAgent("sess-lead2", "lead2", &TeamMemberConfig{ID: "lead2", Name: "Lead2", Tier: "strategic"})
	// Give lead1 a pending task so the reassign-to-strategic loop runs.
	m.GetAgent("sess-lead1").AddTask(&Task{ID: "t1", Title: "x", Status: TaskPending})

	m.HandleAgentDeath("sess-lead1")

	if m.GetAgent("sess-lead1") != nil {
		t.Error("expected dead lead removed")
	}
	if m.GetAgent("sess-lead2") == nil {
		t.Error("expected surviving lead to remain")
	}
	// The reassigned task should now live on lead2.
	found := false
	for _, tk := range m.GetAgent("sess-lead2").GetTasksCopy() {
		if tk.ID == "t1" {
			found = true
		}
	}
	if !found {
		t.Error("expected pending task reassigned to surviving strategic lead")
	}
}

// TestSubmitLeaderTasksResponse_NilAgent covers the agent==nil false path.
func TestSubmitLeaderTasksResponse_NilAgent(t *testing.T) {
	m := newTestManager()
	if m.SubmitLeaderTasksResponse("ghost", "do tasks") {
		t.Error("expected false for unknown session")
	}
}

// TestSubmitReflectionResponse_NilAgent covers the agent==nil false path.
func TestSubmitReflectionResponse_NilAgent(t *testing.T) {
	m := newTestManager()
	if m.SubmitReflectionResponse("ghost", "learn", "technique", "", "") {
		t.Error("expected false for unknown session")
	}
}

// TestSubmitReflectionResponse_AppendsToMemory covers the memory-append
// branch: lastWorkingDir is set, so MemoryAppendSection runs.
func TestSubmitReflectionResponse_AppendsToMemory(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()
	m := newTestManager()
	m.mu.Lock()
	m.lastWorkingDir = "/proj/reflect"
	m.mu.Unlock()
	m.RegisterAgent("sess-r", "rcfg", &TeamMemberConfig{ID: "rcfg", Name: "R", Tier: "worker"})

	if !m.SubmitReflectionResponse("sess-r", "use idempotent writes", "technique", "task-1", "Refactor") {
		t.Fatal("expected reflection to succeed")
	}
	// Memory file should now contain the learning line.
	mem := LoadAgentMemory("rcfg", "/proj/reflect")
	if mem == "" {
		t.Error("expected reflection appended to agent memory")
	}
}

// TestBuildMemberPayload_NilAgent covers the a==nil / Config==nil guard.
func TestBuildMemberPayload_NilGuards(t *testing.T) {
	m := newTestManager()
	if m.BuildMemberPayload(nil) != nil {
		t.Error("expected nil for nil agent")
	}
	// Agent with nil config.
	a := NewAgentState("sess-x", "cfg", nil)
	if m.BuildMemberPayload(a) != nil {
		t.Error("expected nil for agent with nil config")
	}
}

// TestBuildMemberPayload_WorkingViaTaskStatus covers the
// in-progress-task status loop and the LastTurnAt populated branch.
func TestBuildMemberPayload_WorkingViaTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-w", "wcfg", &TeamMemberConfig{ID: "wcfg", Name: "Worker"})
	a := m.GetAgent("sess-w")
	// Add an in-progress task but no ActiveTask so status falls into the loop.
	a.AddTask(&Task{ID: "t", Title: "x", Status: TaskInProgress})
	// Record a turn so LastTurnAt is non-zero.
	a.RecordTurnUsage(10, 5, 0.01)

	payload := m.BuildMemberPayload(a)
	if payload == nil {
		t.Fatal("expected payload")
	}
	if payload["status"] != "working" {
		t.Errorf("expected status=working via in-progress task, got %v", payload["status"])
	}
}

// TestGetWrapUpStatus_SkipsNilConfig injects an agent with a nil config so
// the nil-config skip branch runs.
func TestGetWrapUpStatus_SkipsNilConfig(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-good", "good", &TeamMemberConfig{ID: "good", Name: "Good"})
	// Inject a nil-config agent directly.
	nilCfgAgent := NewAgentState("sess-nilcfg", "nilcfg", nil)
	m.mu.Lock()
	m.agents["sess-nilcfg"] = nilCfgAgent
	m.mu.Unlock()

	status := m.GetWrapUpStatus()
	// Only the good agent should appear (nil-config skipped).
	for _, as := range status.AgentStatus {
		if as.ConfigID == "nilcfg" {
			t.Error("expected nil-config agent skipped in wrap-up status")
		}
	}
}

// TestLoadPersistedMetrics_RoundTrip persists then reloads metrics so the
// LoadPersistedMetrics success path is covered alongside PersistMetrics.
func TestLoadPersistedMetrics_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.metrics.TotalLearnings.Add(7)
	m.PersistMetrics()

	m2 := newTestManager()
	m2.LoadPersistedMetrics()
	snap := m2.metrics.Snapshot()
	if snap.TotalLearnings != 7 {
		t.Errorf("expected reloaded TotalLearnings=7, got %d", snap.TotalLearnings)
	}
	_ = time.Now()
}
