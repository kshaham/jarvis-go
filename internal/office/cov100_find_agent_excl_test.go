package office

import (
	"testing"
)

// TestFindAvailableAgentExcluding_NilConfigIneligible drives the
// `a == nil || a.Config == nil` eligibility guard (questions.go ~456).
// We register a peer then null its Config; it must be skipped, so the
// only candidate is gone and the result is nil.
func TestFindAvailableAgentExcluding_NilConfigIneligible(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, peer)

	// Null the peer's config so the eligibility check rejects it.
	a := m.GetAgent("sess-qa")
	a.mu.Lock()
	a.Config = nil
	a.mu.Unlock()

	if got := m.findAvailableAgentExcluding("sess-vex", nil); got != nil {
		t.Errorf("nil-config peer should be ineligible, got %s", got.SessionID())
	}
}

// TestFindAvailableAgentExcluding_ActiveTaskIneligible drives the
// `ActiveTask() != nil || PendingTaskCount() > 1` guard (~462). A peer
// with an in-progress task is busy and must be skipped.
func TestFindAvailableAgentExcluding_ActiveTaskIneligible(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, peer)

	a := m.GetAgent("sess-qa")
	a.AddTask(&Task{ID: "t1", Title: "busy", Status: TaskInProgress})

	if got := m.findAvailableAgentExcluding("sess-vex", nil); got != nil {
		t.Errorf("peer with active task should be ineligible, got %s", got.SessionID())
	}
}

// TestFindAvailableAgentExcluding_TooManyPendingIneligible drives the
// PendingTaskCount() > 1 half of the same guard.
func TestFindAvailableAgentExcluding_TooManyPendingIneligible(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, peer)

	a := m.GetAgent("sess-qa")
	a.AddTask(&Task{ID: "p1", Title: "pending1", Status: TaskPending})
	a.AddTask(&Task{ID: "p2", Title: "pending2", Status: TaskPending})

	if got := m.findAvailableAgentExcluding("sess-vex", nil); got != nil {
		t.Errorf("peer with >1 pending tasks should be ineligible, got %s", got.SessionID())
	}
}

// TestFindAvailableAgentExcluding_WorkingIneligible drives the
// `sessions.IsSessionWorking` guard (~465). A peer reporting working is
// mid-turn and must be skipped.
func TestFindAvailableAgentExcluding_WorkingIneligible(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, store := newQuestionTestManager(t, asker, peer)

	store.setWorking("sess-qa", true)

	if got := m.findAvailableAgentExcluding("sess-vex", nil); got != nil {
		t.Errorf("working peer should be ineligible, got %s", got.SessionID())
	}
}

// TestFindAvailableAgentExcluding_AffinityPassRejectsBusyMatch drives
// the role-affinity first-pass branch where the affinity-role candidate
// is ALSO checked by isEligible (questions.go ~478-482): the affinity
// match is busy (active task) so the affinity pass rejects it and the
// fallback pass picks the unrelated idle teammate instead.
func TestFindAvailableAgentExcluding_AffinityPassRejectsBusyMatch(t *testing.T) {
	affinityRoles := RoleAffinityPartners("Frontend")
	if len(affinityRoles) == 0 {
		t.Skip("Frontend has no affinity partners in this build")
	}
	preferred := affinityRoles[0]

	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	// Affinity-role match, but busy => affinity pass rejects it.
	busyMatch := TeamMemberConfig{ID: "match", Name: "Match", Role: preferred, Tier: "worker"}
	idle := TeamMemberConfig{ID: "idle", Name: "Idle", Role: "Designer", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, busyMatch, idle)

	mm := m.GetAgent("sess-match")
	mm.AddTask(&Task{ID: "t1", Title: "busy", Status: TaskInProgress})

	got := m.findAvailableAgentExcluding("sess-vex", nil)
	if got == nil {
		t.Fatal("expected fallback to the idle teammate")
	}
	if got.SessionID() != "sess-idle" {
		t.Errorf("expected fallback to idle teammate, got %s", got.SessionID())
	}
}
