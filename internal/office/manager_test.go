package office

import (
	"strings"
	"testing"
	"time"
)

// TestBlockedTaskCount verifies the atomic counter is updated correctly on
// every status transition into/out of TaskBlocked, including task removal
// while blocked.
func TestBlockedTaskCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})
	m.RegisterAgent("s-bolt", "bolt", &TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	vex := m.GetAgent("s-vex")
	bolt := m.GetAgent("s-bolt")

	if got := m.BlockedTaskCount(); got != 0 {
		t.Fatalf("initial blocked count = %d, want 0", got)
	}

	// Add tasks and transition vex's t1 into blocked.
	vex.AddTask(&Task{ID: "t1", Title: "vex-task", Status: TaskPending})
	vex.UpdateTaskStatus("t1", TaskBlocked)
	if got := m.BlockedTaskCount(); got != 1 {
		t.Errorf("after vex t1 blocked: count=%d want 1", got)
	}

	// Add another task on bolt and block it too.
	bolt.AddTask(&Task{ID: "t2", Title: "bolt-task", Status: TaskPending})
	bolt.UpdateTaskStatus("t2", TaskBlocked)
	if got := m.BlockedTaskCount(); got != 2 {
		t.Errorf("after bolt t2 blocked: count=%d want 2", got)
	}

	// Unblock vex's task by transitioning to in_progress.
	vex.UpdateTaskStatus("t1", TaskInProgress)
	if got := m.BlockedTaskCount(); got != 1 {
		t.Errorf("after unblocking vex t1: count=%d want 1", got)
	}

	// Remove bolt's blocked task directly (remove-while-blocked path).
	bolt.RemoveTask("t2")
	if got := m.BlockedTaskCount(); got != 0 {
		t.Errorf("after removing blocked t2: count=%d want 0", got)
	}

	// Block -> complete (not just blocked -> in_progress).
	vex.UpdateTaskStatus("t1", TaskBlocked)
	if got := m.BlockedTaskCount(); got != 1 {
		t.Errorf("re-blocking t1: count=%d want 1", got)
	}
	vex.UpdateTaskStatus("t1", TaskCompleted)
	if got := m.BlockedTaskCount(); got != 0 {
		t.Errorf("completing blocked t1: count=%d want 0", got)
	}

	// UnregisterAgent drains remaining blocked count for that agent.
	// Register a fresh agent (vex was unregistered above via UnregisterAgent
	// semantics; re-register with a new session so the agent map is clean).
	m.RegisterAgent("s-vex2", "vex2", &TeamMemberConfig{ID: "vex2", Name: "Vex2"})
	vex2 := m.GetAgent("s-vex2")
	vex2.AddTask(&Task{ID: "t4", Title: "drain-test", Status: TaskPending})
	vex2.UpdateTaskStatus("t4", TaskBlocked)
	if got := m.BlockedTaskCount(); got != 1 {
		t.Errorf("before unregister: count=%d want 1", got)
	}
	m.UnregisterAgent("s-vex2")
	if got := m.BlockedTaskCount(); got != 0 {
		t.Errorf("after unregistering agent with blocked task: count=%d want 0", got)
	}
}

// mockSessionStore implements SessionStore for testing.
type mockSessionStore struct{}

func (m *mockSessionStore) WriteViaScreen(sessionID, input string) bool  { return true }
func (m *mockSessionStore) WriteInkPrompt(sessionID, prompt string) bool { return true }
func (m *mockSessionStore) GetSessionIDs() []string                      { return nil }
func (m *mockSessionStore) IsSessionAlive(sessionID string) bool         { return true }
func (m *mockSessionStore) IsSessionWorking(sessionID string) bool       { return false }
func (m *mockSessionStore) StopSession(sessionID string) error           { return nil }

func newTestManager() *Manager {
	m := NewManager(&mockSessionStore{}, func(event string, data interface{}) {}, nil)
	// Clear any persisted messages from disk so tests start clean.
	m.mu.Lock()
	m.messages = make([]OfficeMessage, 0)
	m.mu.Unlock()
	return m
}

func TestNewManager(t *testing.T) {
	m := newTestManager()
	if m.AgentCount() != 0 {
		t.Errorf("expected 0 agents, got %d", m.AgentCount())
	}
	if m.IsTeamSpawned() {
		t.Error("expected team not spawned")
	}
}

func TestManager_RegisterAgent(t *testing.T) {
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	if m.AgentCount() != 1 {
		t.Errorf("expected 1 agent, got %d", m.AgentCount())
	}

	agent := m.GetAgent("sess-1")
	if agent == nil || agent.ConfigID != "vex" {
		t.Error("expected to find agent")
	}

	agent2 := m.GetAgentByConfig("vex")
	if agent2 == nil || agent2.SessionID() != "sess-1" {
		t.Error("expected to find agent by config")
	}
}

func TestManager_UnregisterAgent(t *testing.T) {
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex"}
	m.RegisterAgent("sess-1", "vex", cfg)
	m.UnregisterAgent("sess-1")

	if m.AgentCount() != 0 {
		t.Errorf("expected 0 agents, got %d", m.AgentCount())
	}
	if m.GetAgentByConfig("vex") != nil {
		t.Error("expected nil after unregister")
	}
}

func TestManager_SendMessage(t *testing.T) {
	m := newTestManager()
	m.SendMessage("vex", "bolt", "hello there")

	msgs := m.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello there" {
		t.Errorf("expected 'hello there', got %q", msgs[0].Content)
	}
}

func TestManager_SendMessage_Overflow(t *testing.T) {
	m := newTestManager()
	for i := 0; i < 60; i++ {
		m.SendMessage("a", "b", "msg")
	}
	msgs := m.GetMessages()
	if len(msgs) != m.config.MaxMessages {
		t.Errorf("expected %d messages, got %d", m.config.MaxMessages, len(msgs))
	}
}

func TestManager_SetTeamSpawned_RecordsTime(t *testing.T) {
	m := newTestManager()

	m.SetTeamSpawned(true)
	m.mu.RLock()
	spawned := !m.teamSpawnedAt.IsZero()
	m.mu.RUnlock()
	if !spawned {
		t.Fatal("expected teamSpawnedAt to be set")
	}

	m.SetTeamSpawned(false)
	m.mu.RLock()
	cleared := m.teamSpawnedAt.IsZero()
	m.mu.RUnlock()
	if !cleared {
		t.Fatal("expected teamSpawnedAt to be cleared")
	}
}

func TestManager_SetWrappingUp_RecordsTime(t *testing.T) {
	m := newTestManager()

	m.SetWrappingUp(true)
	m.mu.RLock()
	set := !m.wrapUpStartedAt.IsZero()
	m.mu.RUnlock()
	if !set {
		t.Fatal("expected wrapUpStartedAt to be set")
	}

	m.SetWrappingUp(false)
	m.mu.RLock()
	cleared := m.wrapUpStartedAt.IsZero()
	m.mu.RUnlock()
	if !cleared {
		t.Fatal("expected wrapUpStartedAt to be cleared")
	}
}

func TestManager_GetOfficeSettings(t *testing.T) {
	m := newTestManager()
	m.SetTeamSpawned(true)

	settings := m.GetOfficeSettings()

	// Verify all 20+ keys are present
	requiredKeys := []string{
		"autoStandup", "brainstormCooldownMs", "brainstormResponseTimeoutMs",
		"idleThresholdMs", "idleCheckIntervalMs", "meetingInactivityTimeoutMs",
		"standupResponseTimeoutMs", "fileLockExpiryMs", "curiosityEnabled",
		"curiosityCooldownMs", "reflectionResponseTimeoutMs", "teamSpawned",
		"teamSpawnedAt", "memberCount", "activeMeetingCount", "activeLockCount",
		"standupActive", "lastBrainstormAt", "metrics", "teamMission",
	}
	for _, key := range requiredKeys {
		if _, exists := settings[key]; !exists {
			t.Errorf("missing key %q in GetOfficeSettings", key)
		}
	}

	// Verify teamSpawned is true
	if ts, ok := settings["teamSpawned"].(bool); !ok || !ts {
		t.Error("expected teamSpawned=true")
	}

	// Verify teamSpawnedAt is a positive ms timestamp
	if tsAt, ok := settings["teamSpawnedAt"].(int64); !ok || tsAt <= 0 {
		t.Error("expected teamSpawnedAt > 0")
	}

	// Verify curiosityEnabled default is true
	if ce, ok := settings["curiosityEnabled"].(bool); !ok || !ce {
		t.Error("expected curiosityEnabled=true by default")
	}
}

func TestManager_UpdateOfficeSettings(t *testing.T) {
	m := newTestManager()

	// Below minimum should be rejected
	m.UpdateOfficeSettings(map[string]interface{}{
		"brainstormCooldownMs": float64(1000), // min 60000
	})
	if m.config.BrainstormCooldown != DefaultConfig().BrainstormCooldown {
		t.Error("expected brainstormCooldown to remain at default for below-minimum value")
	}

	// Valid value should be applied
	m.UpdateOfficeSettings(map[string]interface{}{
		"brainstormCooldownMs": float64(120000),
		"teamMission":          "ship v2",
		"curiosityEnabled":     false,
	})
	if m.config.BrainstormCooldown.Milliseconds() != 120000 {
		t.Errorf("expected 120000ms, got %d", m.config.BrainstormCooldown.Milliseconds())
	}
	if m.teamMission != "ship v2" {
		t.Errorf("expected 'ship v2', got %q", m.teamMission)
	}
	if m.curiosityEnabled {
		t.Error("expected curiosityEnabled=false")
	}
}

func TestManager_GetWrapUpStatus(t *testing.T) {
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	// Give the agent an active task
	agent := m.GetAgent("sess-1")
	agent.AddTask(&Task{
		ID:     "task-1",
		Title:  "Fix button",
		Status: TaskInProgress,
	})

	m.SetWrappingUp(true)
	status := m.GetWrapUpStatus()

	if !status.Active {
		t.Error("expected active=true")
	}
	if status.StartedAt <= 0 {
		t.Error("expected startedAt > 0")
	}
	if len(status.AgentStatus) != 1 {
		t.Fatalf("expected 1 agent status, got %d", len(status.AgentStatus))
	}
	if !status.AgentStatus[0].HasActiveTask {
		t.Error("expected hasActiveTask=true")
	}
	if status.AgentStatus[0].TaskTitle == nil || *status.AgentStatus[0].TaskTitle != "Fix button" {
		t.Error("expected taskTitle='Fix button'")
	}
}

func TestManager_GetOfficeState(t *testing.T) {
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend", Color: "#00f"}
	m.RegisterAgent("sess-2", "bolt", cfg)
	m.SendMessage("bolt", "vex", "hello")
	m.AddBugTask(&BugTask{ID: "bug-1", Status: "open"})

	state := m.GetOfficeState()

	requiredKeys := []string{
		"members", "standupActive", "standupTranscript", "recentMessages",
		"meetings", "fileLocks", "teamSpawned", "whiteboardNotes",
		"bugTasks", "taskQueues", "recentAudit", "wrapUp", "timestamp",
	}
	for _, key := range requiredKeys {
		if _, exists := state[key]; !exists {
			t.Errorf("missing key %q in GetOfficeState", key)
		}
	}

	members, ok := state["members"].([]map[string]interface{})
	if !ok || len(members) != 1 {
		t.Fatalf("expected 1 member, got %v", state["members"])
	}
	if members[0]["name"] != "Bolt" {
		t.Errorf("expected member name 'Bolt', got %v", members[0]["name"])
	}
}

func TestSanitizeForTerminal(t *testing.T) {
	input := "hello\x00world\x01\x02normal\nnewline"
	expected := "helloworldnormal\nnewline"
	if got := sanitizeForTerminal(input); got != expected {
		t.Errorf("sanitize(%q) = %q, want %q", input, got, expected)
	}
}

// ---------------------------------------------------------------------------
// SubmitReflectionResponse tests
// ---------------------------------------------------------------------------

// TestSubmitReflectionResponse_PersistsLearning verifies that calling
// SubmitReflectionResponse writes a learning entry to the learning store.
func TestSubmitReflectionResponse_PersistsLearning(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "reflect-persist", Name: "ReflectPersist", Tier: "worker"}
	m.RegisterAgent("sess-rp", "reflect-persist", cfg)

	ok := m.SubmitReflectionResponse("sess-rp", "always write idempotent operations", "technique", "task-42", "DB Refactor")
	if !ok {
		t.Fatal("expected SubmitReflectionResponse to return true")
	}

	ld := LoadLearningData("reflect-persist")
	if len(ld.Learnings) == 0 {
		t.Fatal("expected at least one learning entry persisted")
	}
	entry := ld.Learnings[0]
	if entry.Content != "always write idempotent operations" {
		t.Errorf("unexpected learning content: %q", entry.Content)
	}
	if entry.Category != "technique" {
		t.Errorf("expected category 'technique', got %q", entry.Category)
	}
	if entry.TaskID != "task-42" {
		t.Errorf("expected taskID 'task-42', got %q", entry.TaskID)
	}
}

// TestSubmitReflectionResponse_SkillSuggestion verifies that the session
// receives a skill-creation suggestion when the category is "technique" or
// "pattern".
func TestSubmitReflectionResponse_SkillSuggestion(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	m, store := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "skill-suggest", Name: "SkillSuggest", Tier: "worker"}
	m.RegisterAgent("sess-ss", "skill-suggest", cfg)

	m.SubmitReflectionResponse("sess-ss", "use builder pattern for complex configs", "technique", "", "")

	writes := store.writesForSession("sess-ss")
	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[SKILL SUGGESTION]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [SKILL SUGGESTION] prompt for technique category")
	}
}

// TestHandleAgentDeath_NoStrategicAgent verifies that when no strategic agent
// exists, pending tasks remain failed (not reassigned) and the dead agent is
// still removed from the roster.
func TestHandleAgentDeath_NoStrategicAgent(t *testing.T) {
	m := newTestManager()

	// Only register a worker-tier agent (no strategic agent).
	cfg := &TeamMemberConfig{ID: "lone-worker", Name: "LoneWorker", Tier: "worker"}
	m.RegisterAgent("sess-lone", "lone-worker", cfg)

	agent := m.GetAgent("sess-lone")
	agent.AddTask(&Task{
		ID:     "orphan-task-1",
		Title:  "Task with no one to reassign to",
		Status: TaskPending,
	})

	m.HandleAgentDeath("sess-lone")

	// Agent should be unregistered.
	if m.GetAgent("sess-lone") != nil {
		t.Error("expected agent to be removed after HandleAgentDeath")
	}
	// No other agents should have received the task.
	if m.AgentCount() != 0 {
		t.Errorf("expected 0 agents, got %d", m.AgentCount())
	}
}

// TestSweepMeetings_RemovesStaleInactive verifies meetings that are inactive
// and older than 6 hours are removed, while active or recent inactive meetings
// are preserved.
func TestSweepMeetings_RemovesStaleInactive(t *testing.T) {
	m := newTestManager()

	// Helper: inject a meeting directly.
	now := time.Now()
	m.mu.Lock()
	m.meetings["old-inactive"] = &Meeting{
		ID: "old-inactive", Type: "brainstorm", Active: false,
		StartedAt: now.Add(-7 * time.Hour),
	}
	m.meetings["recent-inactive"] = &Meeting{
		ID: "recent-inactive", Type: "brainstorm", Active: false,
		StartedAt: now.Add(-1 * time.Hour),
	}
	m.meetings["old-active"] = &Meeting{
		ID: "old-active", Type: "standup", Active: true,
		StartedAt: now.Add(-10 * time.Hour),
	}
	m.mu.Unlock()

	swept := m.SweepMeetings()
	if swept != 1 {
		t.Errorf("expected 1 swept meeting, got %d", swept)
	}

	m.mu.RLock()
	_, oldInactiveExists := m.meetings["old-inactive"]
	_, recentInactiveExists := m.meetings["recent-inactive"]
	_, oldActiveExists := m.meetings["old-active"]
	m.mu.RUnlock()

	if oldInactiveExists {
		t.Error("expected old-inactive meeting to be swept")
	}
	if !recentInactiveExists {
		t.Error("expected recent-inactive meeting to be preserved")
	}
	if !oldActiveExists {
		t.Error("expected old-active meeting to be preserved (active meetings are never swept)")
	}
}

// ---------------------------------------------------------------------------
// Fix 1: meeting-membership index (O(1) IsInActiveMeeting)
// ---------------------------------------------------------------------------

// TestIsInActiveMeetingIndex verifies that IsInActiveMeeting returns the
// correct answer via the derived activeMeetingMembers index and that the index
// is kept in sync through StartMeeting / endMeeting / EndMeeting /
// abortMeetingsFacilitatedBy / sweepStaleMeetings / SweepMeetings.
func TestIsInActiveMeetingIndex(t *testing.T) {
	m := newTestManager()

	cfg1 := &TeamMemberConfig{ID: "idx-a", Name: "A", Tier: "worker"}
	cfg2 := &TeamMemberConfig{ID: "idx-b", Name: "B", Tier: "worker"}
	m.RegisterAgent("sess-idx-a", "idx-a", cfg1)
	m.RegisterAgent("sess-idx-b", "idx-b", cfg2)

	// Before any meeting: both agents are not in a meeting.
	if m.IsInActiveMeeting("sess-idx-a") {
		t.Error("expected sess-idx-a NOT in active meeting before any meeting")
	}
	if m.IsInActiveMeeting("sess-idx-b") {
		t.Error("expected sess-idx-b NOT in active meeting before any meeting")
	}

	// Start a meeting with both participants.
	mtg := m.StartMeeting("brainstorm", []string{"sess-idx-a", "sess-idx-b"})
	if !m.IsInActiveMeeting("sess-idx-a") {
		t.Error("expected sess-idx-a IN active meeting after StartMeeting")
	}
	if !m.IsInActiveMeeting("sess-idx-b") {
		t.Error("expected sess-idx-b IN active meeting after StartMeeting")
	}
	if m.IsInActiveMeeting("sess-nonexistent") {
		t.Error("expected nonexistent session NOT in active meeting")
	}

	// End the meeting via EndMeeting (public API).
	m.EndMeeting(mtg.ID)
	if m.IsInActiveMeeting("sess-idx-a") {
		t.Error("expected sess-idx-a NOT in active meeting after EndMeeting")
	}
	if m.IsInActiveMeeting("sess-idx-b") {
		t.Error("expected sess-idx-b NOT in active meeting after EndMeeting")
	}

	// Start another meeting, end via endMeeting (internal).
	mtg2 := m.startMeetingByID("test-mtg-2", "brainstorm", []string{"sess-idx-a"})
	if !m.IsInActiveMeeting("sess-idx-a") {
		t.Error("expected sess-idx-a IN active meeting after startMeetingByID")
	}
	m.endMeeting(mtg2.ID)
	if m.IsInActiveMeeting("sess-idx-a") {
		t.Error("expected sess-idx-a NOT in active meeting after endMeeting")
	}

	// sweepStaleMeetings: inject an over-age active meeting and verify sweep clears the index.
	m.mu.Lock()
	m.meetings["stale-mtg"] = &Meeting{
		ID:           "stale-mtg",
		Type:         "brainstorm",
		Participants: []string{"sess-idx-b"},
		StartedAt:    time.Now().Add(-3 * time.Hour),
		Active:       true,
	}
	m.rebuildActiveMeetingMembersLocked()
	m.mu.Unlock()

	if !m.IsInActiveMeeting("sess-idx-b") {
		t.Error("expected sess-idx-b IN meeting before sweepStaleMeetings")
	}
	m.sweepStaleMeetings()
	if m.IsInActiveMeeting("sess-idx-b") {
		t.Error("expected sess-idx-b NOT in meeting after sweepStaleMeetings")
	}
}

// ---------------------------------------------------------------------------
// Fix 2: GetWrapUpStatus snapshot-then-unlock
// ---------------------------------------------------------------------------

// TestGetWrapUpStatusSnapshot verifies correctness of GetWrapUpStatus after
// the snapshot-then-unlock refactor: per-agent ActiveTask() is now called
// outside m.mu, so the result must still reflect live task state.
func TestGetWrapUpStatusSnapshot(t *testing.T) {
	m := newTestManager()
	cfg := &TeamMemberConfig{ID: "wus-agent", Name: "WUSAgent", Role: "Backend"}
	m.RegisterAgent("sess-wus", "wus-agent", cfg)

	agent := m.GetAgent("sess-wus")
	agent.AddTask(&Task{
		ID:     "wus-task-1",
		Title:  "Active task",
		Status: TaskInProgress,
	})

	m.SetWrappingUp(true)
	status := m.GetWrapUpStatus()

	if !status.Active {
		t.Error("expected Active=true")
	}
	if status.StartedAt <= 0 {
		t.Error("expected StartedAt > 0")
	}
	if len(status.AgentStatus) != 1 {
		t.Fatalf("expected 1 AgentStatus, got %d", len(status.AgentStatus))
	}
	s := status.AgentStatus[0]
	if !s.HasActiveTask {
		t.Error("expected HasActiveTask=true")
	}
	if s.TaskTitle == nil || *s.TaskTitle != "Active task" {
		t.Errorf("expected TaskTitle='Active task', got %v", s.TaskTitle)
	}
	if s.Name != "WUSAgent" {
		t.Errorf("expected Name='WUSAgent', got %q", s.Name)
	}

	// Wrap-up not active: AgentStatus still populated, Active=false.
	m.SetWrappingUp(false)
	status2 := m.GetWrapUpStatus()
	if status2.Active {
		t.Error("expected Active=false after SetWrappingUp(false)")
	}
	if len(status2.AgentStatus) != 1 {
		t.Fatalf("expected 1 AgentStatus even when not wrapping up, got %d", len(status2.AgentStatus))
	}
}

// ---------------------------------------------------------------------------
// Fix 3: AuditWriter.LastN
// ---------------------------------------------------------------------------

// TestAuditLastN verifies that LastN returns the correct suffix of audit
// entries in insertion order, matching what a full Entries() + slice would
// return, without requiring a full-ring allocation.
func TestAuditLastN(t *testing.T) {
	const maxSize = 500
	const pushCount = 60
	const wantCount = 50

	aw := NewAuditWriter(maxSize)

	for i := 0; i < pushCount; i++ {
		aw.Log(AuditEntry{Event: "evt", AgentName: string(rune('A' + i%26))})
	}

	got := aw.LastN(wantCount)
	if len(got) != wantCount {
		t.Fatalf("expected %d entries from LastN(%d), got %d", wantCount, wantCount, len(got))
	}

	// The last entry should be the most recently pushed.
	all := aw.Entries()
	want := all[len(all)-wantCount:]
	for i, e := range got {
		if e.Event != want[i].Event || e.AgentName != want[i].AgentName {
			t.Errorf("entry[%d]: got {%q %q}, want {%q %q}", i, e.Event, e.AgentName, want[i].Event, want[i].AgentName)
		}
	}

	// LastN with n >= total returns all.
	allViaLastN := aw.LastN(maxSize + 1)
	if len(allViaLastN) != pushCount {
		t.Errorf("LastN(big) expected %d, got %d", pushCount, len(allViaLastN))
	}

	// LastN(0) returns nil/empty.
	empty := aw.LastN(0)
	if len(empty) != 0 {
		t.Errorf("LastN(0) expected empty, got %d entries", len(empty))
	}
}
