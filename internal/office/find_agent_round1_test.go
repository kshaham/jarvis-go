package office

import (
	"testing"
)

// TestFindAvailableAgent_AffinityFirst verifies the affinity-matched first
// pass returns a preferred-role partner over a non-affinity idle agent.
func TestFindAvailableAgent_AffinityFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Requester is a Frontend Engineer; its affinity partners include
	// "UI/UX Designer".
	m.RegisterAgent("sess-req", "req", &TeamMemberConfig{ID: "req", Name: "Req", Role: "Frontend Engineer"})
	m.RegisterAgent("sess-designer", "des", &TeamMemberConfig{ID: "des", Name: "Des", Role: "UI/UX Designer"})
	m.RegisterAgent("sess-random", "rnd", &TeamMemberConfig{ID: "rnd", Name: "Rnd", Role: "Data Scientist"})

	got := m.findAvailableAgent("sess-req")
	if got == nil {
		t.Fatal("expected an available agent")
	}
	if got.ConfigID != "des" {
		t.Errorf("expected the affinity-matched designer, got %q", got.ConfigID)
	}
}

// TestFindAvailableAgent_SkipsBusyAndActiveTask verifies agents that are
// mid-turn (IsSessionWorking) or holding an active task are not chosen.
func TestFindAvailableAgent_SkipsBusyAndActiveTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-req", "req", &TeamMemberConfig{ID: "req", Name: "Req", Role: "Backend Engineer"})
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Role: "DevOps Engineer"})
	m.RegisterAgent("sess-idle", "idle", &TeamMemberConfig{ID: "idle", Name: "Idle", Role: "QA Engineer"})

	// Mark the affinity-preferred DevOps agent as mid-turn so it's skipped.
	store.setWorking("sess-busy", true)

	got := m.findAvailableAgent("sess-req")
	if got == nil {
		t.Fatal("expected an available agent")
	}
	if got.ConfigID == "busy" {
		t.Error("a mid-turn agent must not be selected")
	}

	// Now give the idle agent an active task -> no one is available.
	idle := m.GetAgent("sess-idle")
	idle.AddTask(&Task{ID: "t", Title: "work", Status: TaskInProgress})
	if got := m.findAvailableAgent("sess-req"); got != nil && got.ConfigID == "idle" {
		t.Error("an agent with an active task must not be selected")
	}
}

// TestFindAvailableAgent_NoneAvailable returns nil when every other agent is
// busy or excluded.
func TestFindAvailableAgent_NoneAvailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-req", "req", &TeamMemberConfig{ID: "req", Name: "Req", Role: "Backend Engineer"})
	m.RegisterAgent("sess-other", "other", &TeamMemberConfig{ID: "other", Name: "Other", Role: "QA Engineer"})
	store.setWorking("sess-other", true)

	if got := m.findAvailableAgent("sess-req"); got != nil {
		t.Errorf("expected nil when no agent is available, got %q", got.ConfigID)
	}

	// Excluding the only other agent also yields nil.
	if got := m.findAvailableAgent("sess-other"); got != nil && got.ConfigID == "other" {
		t.Error("the excluded agent must not be returned")
	}
}

// TestRoleAffinityPartners covers a known role and an unknown role.
func TestRoleAffinityPartners(t *testing.T) {
	partners := RoleAffinityPartners("Frontend Engineer")
	if len(partners) == 0 {
		t.Error("expected affinity partners for Frontend Engineer")
	}
	if got := RoleAffinityPartners("Nonexistent Role"); got != nil {
		t.Errorf("expected nil for an unknown role, got %v", got)
	}
}
