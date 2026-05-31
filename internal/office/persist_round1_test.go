package office

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSubmitStandupResponse_Branches covers the unknown-agent, no-slot, and
// closed-slot (late) branches not exercised by the happy-path test.
func TestSubmitStandupResponse_Branches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// Unknown agent -> false.
	if m.SubmitStandupResponse("ghost", "hi") {
		t.Error("expected false for unknown agent")
	}

	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	agent := m.GetAgent("sess-a")

	// No standup slot installed -> false (late/no-active-standup path).
	if m.SubmitStandupResponse("sess-a", "no slot") {
		t.Error("expected false with no standup slot")
	}

	// Closed slot -> false + late-response counter advances.
	slot := newWaitSlot()
	slot.finish()
	agent.mu.Lock()
	agent.StandupSlot = slot
	agent.mu.Unlock()
	before := LateResponsesDropped()
	if m.SubmitStandupResponse("sess-a", "too late") {
		t.Error("expected false delivering to a closed slot")
	}
	if LateResponsesDropped() <= before {
		t.Error("expected late-response counter to advance")
	}
}

// TestPersistStandup_RoundTrip writes a completed standup to disk and reloads
// it, asserting the responded/timed-out tally is computed from the structured
// TimedOut flag.
func TestPersistStandup_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	id := "standup-rt-1"
	startedAt := time.Now().Add(-2 * time.Minute)
	transcript := []StandupMessage{
		{Type: "opening", Content: "Standup begins"},
		{Type: "status_report", SpeakerName: "Alpha", Content: "shipped X"},
		{Type: "status_report", SpeakerName: "Beta", Content: "(offline - skipped)", TimedOut: true},
		{Type: "status_report", SpeakerName: "Gamma", Content: "fixed a bug"},
	}
	m.persistStandup(id, []string{"sess-a", "sess-b", "sess-g"}, transcript, startedAt)

	loaded, err := LoadPersistedStandup(id)
	if err != nil {
		t.Fatalf("LoadPersistedStandup: %v", err)
	}
	if loaded.RespondedCount != 2 {
		t.Errorf("expected 2 responded, got %d", loaded.RespondedCount)
	}
	if loaded.TimedOutCount != 1 {
		t.Errorf("expected 1 timed out, got %d", loaded.TimedOutCount)
	}
	if loaded.ParticipantCount != 3 {
		t.Errorf("expected 3 participants, got %d", loaded.ParticipantCount)
	}

	// LatestCompletedStandupID should resolve to our standup.
	latest, err := LatestCompletedStandupID()
	if err != nil {
		t.Fatalf("LatestCompletedStandupID: %v", err)
	}
	if latest != id {
		t.Errorf("expected latest standup %q, got %q", id, latest)
	}
}

// TestSetTeamMissionPersistsAndReloads round-trips the team mission through
// disk: a fresh Manager reads it back via loadPersistedMission in its
// constructor.
func TestSetTeamMissionPersistsAndReloads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamMission("  Build the best office automation  ")
	if got := m.GetTeamMission(); got != "Build the best office automation" {
		t.Errorf("expected trimmed mission, got %q", got)
	}

	m2 := newTestManager()
	if got := m2.GetTeamMission(); got != "Build the best office automation" {
		t.Errorf("expected mission reloaded in fresh manager, got %q", got)
	}
}

// TestAppendMissionInsight_AppendsAndGuards covers the guard (empty insight /
// empty mission = no-op) and the happy append + persist path.
func TestAppendMissionInsight_AppendsAndGuards(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// No mission yet: insight is dropped.
	m.AppendMissionInsight("ignored")
	if m.GetTeamMission() != "" {
		t.Error("expected insight ignored when no mission set")
	}

	m.SetTeamMission("Mission One")
	// Empty insight: no-op.
	m.AppendMissionInsight("")
	if m.GetTeamMission() != "Mission One" {
		t.Error("expected empty insight to be a no-op")
	}

	// Real insight appends a [Learned] block and persists.
	m.AppendMissionInsight("agents reflect more")
	got := m.GetTeamMission()
	if got == "Mission One" {
		t.Error("expected mission to grow after a real insight")
	}
	if !strings.Contains(got, "Mission One") || !strings.Contains(got, "[Learned] agents reflect more") {
		t.Errorf("unexpected mission content: %q", got)
	}

	// Reloads in a fresh manager.
	m2 := newTestManager()
	if !strings.Contains(m2.GetTeamMission(), "[Learned] agents reflect more") {
		t.Errorf("expected appended insight reloaded, got %q", m2.GetTeamMission())
	}
}

// TestGetMeetings_SkipsNilEntries verifies GetMeetings returns deep copies and
// skips nil map entries.
func TestGetMeetings_SkipsNilEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	live := &Meeting{ID: "mtg-1", Type: "brainstorm", Active: true, Participants: []string{"a", "b"}}
	m.mu.Lock()
	m.meetings["mtg-1"] = live
	m.meetings["mtg-nil"] = nil // must be skipped
	m.mu.Unlock()

	meetings := m.GetMeetings()
	if len(meetings) != 1 {
		t.Fatalf("expected 1 meeting (nil skipped), got %d", len(meetings))
	}
	if meetings[0].ID != "mtg-1" {
		t.Errorf("unexpected meeting: %+v", meetings[0])
	}
	// Returned value must be a copy: mutating it must not affect the stored one.
	meetings[0].Active = false
	m.mu.RLock()
	stillActive := m.meetings["mtg-1"].Active
	m.mu.RUnlock()
	if !stillActive {
		t.Error("GetMeetings must return a copy; mutating it changed stored state")
	}
}

// TestIsWithinSkillsRoot validates the skills-root containment guard for
// in-root, the root itself, and an out-of-root path.
func TestIsWithinSkillsRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := sharedSkillsRoot()

	if !isWithinSkillsRoot(root) {
		t.Error("the skills root itself must be considered within-root")
	}
	if !isWithinSkillsRoot(filepath.Join(root, "my-skill", "SKILL.md")) {
		t.Error("a path under the skills root must be within-root")
	}
	if isWithinSkillsRoot(filepath.Join(filepath.Dir(root), "outside.md")) {
		t.Error("a sibling-of-root path must NOT be within-root")
	}
	// A look-alike sibling that shares the root prefix but is a different dir.
	if isWithinSkillsRoot(root + "-evil") {
		t.Error("a prefix-sharing sibling must NOT be within-root")
	}
}

