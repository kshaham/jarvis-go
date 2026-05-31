package office

import (
	"strings"
	"testing"
)

// TestSnapshotRosterAsTemplateMembers_SkipsNilConfig drives the
// agent.Config==nil skip (spawn_orchestrator.go:210-211): an agent whose
// Config was nulled is omitted from the template-member snapshot.
func TestSnapshotRosterAsTemplateMembers_SkipsNilConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-good", "good", &TeamMemberConfig{ID: "good", Name: "Good", Role: "Backend"})
	m.RegisterAgent("sess-bad", "bad", &TeamMemberConfig{ID: "bad", Name: "Bad"})

	bad := m.GetAgent("sess-bad")
	bad.mu.Lock()
	bad.Config = nil
	bad.mu.Unlock()

	members := m.SnapshotRosterAsTemplateMembers()
	if len(members) != 1 {
		t.Fatalf("expected 1 member (nil-config agent skipped), got %d", len(members))
	}
	if members[0].ID != "good" {
		t.Errorf("expected surviving member 'good', got %q", members[0].ID)
	}
}

// TestPromptNextTask_AppendsAcceptanceCriteria drives the criteria-section
// append in PromptNextTask (spawn_orchestrator.go:348-350): when the
// next task carries acceptance criteria, the rendered checklist is
// appended to the prompt sent to the agent.
func TestPromptNextTask_AppendsAcceptanceCriteria(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := m.GetAgent("sess-vex")

	task := &Task{ID: "t-crit", Title: "Add login flow"}
	agent.AddTask(task)
	// Attach acceptance criteria so renderTaskCriteriaSection returns non-empty.
	if err := agent.SetTaskCriteria("t-crit", "set", []string{
		"build passes", "login redirects to dashboard",
	}); err != nil {
		t.Fatalf("SetTaskCriteria: %v", err)
	}

	m.PromptNextTask("sess-vex", "Add login flow", "t-crit")

	writes := store.writesFor("sess-vex")
	if len(writes) == 0 {
		t.Fatal("expected a next-task prompt written to the agent")
	}
	prompt := writes[len(writes)-1].Input
	if !strings.Contains(prompt, "login redirects to dashboard") {
		t.Errorf("expected acceptance criteria appended to prompt, got:\n%s", prompt)
	}
}

// TestPromptNextTask_NilSessionsReturns drives the sessions==nil early
// return (spawn_orchestrator.go:340-342).
func TestPromptNextTask_NilSessionsReturns(t *testing.T) {
	m := NewManager(nil, func(string, interface{}) {}, nil)
	// Must not panic with a nil session store.
	m.PromptNextTask("sess-x", "title", "task-1")
}
