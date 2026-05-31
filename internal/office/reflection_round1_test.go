package office

import (
	"strings"
	"testing"
)

// TestSubmitReflectionResponse_PersistsAndSuggestsSkill drives the full
// reflection path: channel delivery, learning persistence, memory append
// (workingDir set), and the technique-category skill suggestion.
func TestSubmitReflectionResponse_PersistsAndSuggestsSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, rec := newCtrlManager(t, DefaultConfig())

	// Unknown agent -> false.
	if m.SubmitReflectionResponse("ghost", "x", "", "", "") {
		t.Fatal("expected false for unknown agent")
	}

	m.SetSpawnContext("alpha-team", t.TempDir()) // sets lastWorkingDir for memory append
	m.RegisterAgent("sess-r", "reflector", &TeamMemberConfig{ID: "reflector", Name: "Reflector", Tier: "worker"})
	agent := m.GetAgent("sess-r")
	ch := make(chan string, 1)
	agent.mu.Lock()
	agent.ReflectionCh = ch
	agent.mu.Unlock()

	// Technique category -> skill-suggestion prompt to the agent.
	if !m.SubmitReflectionResponse("sess-r", "Use table-driven tests", "technique", "task-9", "Add tests") {
		t.Fatal("expected reflection accepted")
	}
	// Channel received the learning.
	select {
	case got := <-ch:
		if got != "Use table-driven tests" {
			t.Errorf("reflection channel got %q", got)
		}
	default:
		t.Error("expected learning delivered on reflection channel")
	}

	// Learning persisted: a fresh load shows the entry.
	ld := LoadLearningData("reflector")
	if len(ld.Learnings) == 0 {
		t.Error("expected a persisted learning entry")
	}

	// Skill-suggestion prompt delivered (technique category).
	var sawSuggestion bool
	for _, w := range store.writesFor("sess-r") {
		if strings.Contains(w.Input, "SKILL SUGGESTION") {
			sawSuggestion = true
		}
	}
	if !sawSuggestion {
		t.Error("expected a SKILL SUGGESTION prompt for a technique reflection")
	}

	if !rec.has("office:reflectionResponse") {
		t.Error("expected office:reflectionResponse broadcast")
	}
}

// TestSubmitReflectionResponse_GeneralCategoryNoSuggestion verifies a general
// reflection persists without triggering a skill-suggestion prompt, and that
// the empty category defaults to "general".
func TestSubmitReflectionResponse_GeneralCategoryNoSuggestion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-g", "gen", &TeamMemberConfig{ID: "gen", Name: "Gen", Tier: "worker"})

	// Nil reflection channel + empty category -> still accepted, defaults to general.
	if !m.SubmitReflectionResponse("sess-g", "Be patient with builds", "", "", "") {
		t.Fatal("expected reflection accepted with nil channel")
	}
	for _, w := range store.writesFor("sess-g") {
		if strings.Contains(w.Input, "SKILL SUGGESTION") {
			t.Error("did not expect a skill suggestion for a general reflection")
		}
	}
	ld := LoadLearningData("gen")
	if len(ld.Learnings) == 0 || ld.Learnings[len(ld.Learnings)-1].Category != "general" {
		t.Errorf("expected the empty category to default to 'general', got %+v", ld.Learnings)
	}
}

// TestCreateSnapshot_WithWhiteboardAndSessionIDs covers the member-whiteboard
// copy and the claudeSessionIDsByConfigID mapping branches.
func TestCreateSnapshot_WithWhiteboardAndSessionIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.SetTeamMission("ship the thing")
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "FE"})
	alpha := m.GetAgent("sess-a")
	alpha.AddTask(&Task{ID: "t1", Title: "Build UI", Status: TaskInProgress})
	m.SetWhiteboardNote("sess-a", "Remember to test edge cases")

	snap, err := m.CreateSnapshot("snap-rich", "/work/dir", map[string]string{"alpha": "claude-sid-42"})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if len(snap.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(snap.Members))
	}
	member := snap.Members[0]
	if member.ClaudeSessionID != "claude-sid-42" {
		t.Errorf("expected claude session ID mapped, got %q", member.ClaudeSessionID)
	}
	if member.Whiteboard == nil || !strings.Contains(member.Whiteboard.Content, "edge cases") {
		t.Errorf("expected whiteboard snapshotted, got %+v", member.Whiteboard)
	}
	if len(member.Tasks) != 1 || member.Tasks[0].Title != "Build UI" {
		t.Errorf("expected the in-progress task captured, got %+v", member.Tasks)
	}

	// Round-trip: load it back.
	loaded, err := LoadSnapshot("snap-rich")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.TeamMission != "ship the thing" || len(loaded.Members) != 1 {
		t.Errorf("snapshot did not round-trip cleanly: %+v", loaded)
	}
}
