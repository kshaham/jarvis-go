package office

import (
	"strings"
	"testing"
	"time"
)

// TestTriggerLeaderTaskAssignment_MissionPrefixIncluded drives the
// teamMission != "" branch (standup.go:544-546): when a team mission is
// set, the leader prompt is prefixed with a "[Team Mission] ..." block.
// The sibling test in standup_leader_test.go covers the no-mission path.
func TestTriggerLeaderTaskAssignment_MissionPrefixIncluded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic", Role: "Lead"})
	lead := m.GetAgent("sess-lead")

	const mission = "Ship the dark-green theme to production this week"
	m.SetTeamMission(mission)

	transcript := []StandupMessage{
		{Type: "status_report", SpeakerName: "Lead", Content: "reviewing PRs"},
	}

	m.triggerLeaderTaskAssignment("standup-mission", []string{"sess-lead"}, transcript)

	writes := store.writesFor("sess-lead")
	if len(writes) == 0 {
		t.Fatal("expected leader to receive a task-assignment prompt")
	}
	prompt := writes[0].Input
	if !strings.Contains(prompt, "[Team Mission]") {
		t.Errorf("expected mission prefix in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, mission) {
		t.Errorf("expected mission text embedded, got %q", prompt)
	}
	// The mission prefix must come BEFORE the LEADER TASK ASSIGNMENT header.
	mi := strings.Index(prompt, "[Team Mission]")
	hi := strings.Index(prompt, "LEADER TASK ASSIGNMENT")
	if mi < 0 || hi < 0 || mi >= hi {
		t.Errorf("mission prefix should precede the assignment header; mi=%d hi=%d", mi, hi)
	}

	// Drain the background goroutine quickly.
	lead.mu.Lock()
	slot := lead.LeaderTaskSlot
	lead.mu.Unlock()
	if slot != nil {
		slot.deliver("")
	}
	time.Sleep(50 * time.Millisecond)
}
