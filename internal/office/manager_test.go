package office

import (
	"strings"
	"testing"
	"time"
)

// mockSessionStore implements SessionStore for testing.
type mockSessionStore struct{}

func (m *mockSessionStore) WriteViaScreen(sessionID, input string) bool  { return true }
func (m *mockSessionStore) WriteInkPrompt(sessionID, prompt string) bool { return true }
func (m *mockSessionStore) GetSessionIDs() []string                      { return nil }
func (m *mockSessionStore) IsSessionAlive(sessionID string) bool         { return true }
func (m *mockSessionStore) IsSessionWorking(sessionID string) bool       { return false }

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
	if agent2 == nil || agent2.SessionID != "sess-1" {
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
		"teamMission":         "ship v2",
		"curiosityEnabled":    false,
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

