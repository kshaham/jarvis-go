package office

import (
	"testing"
	"time"
)

// TestGetOfficeSettings_PopulatedCounters drives GetOfficeSettings with a
// spawned team, a dev instance, an active meeting, and an active lock so the
// not-zero-time and counter branches are all exercised.
func TestGetOfficeSettings_PopulatedCounters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	m.SetTeamSpawned(true)
	m.SetSpawnContext("alpha-team", "/work")
	m.SetTeamMission("ship v2")
	_ = m.SetDevInstance("app", "http://jarvis:3000", 0, true)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})

	// An active meeting + an active lock for the counters.
	m.StartMeeting("brainstorm", []string{"sess-a"})
	if _, err := m.AcquireLock("/work/file.go", "sess-a", "Alpha"); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	// A non-zero lastBrainstormAt.
	m.mu.Lock()
	m.lastBrainstormAt = time.Now().Add(-time.Minute)
	m.mu.Unlock()

	settings := m.GetOfficeSettings()

	if settings["teamMission"] != "ship v2" {
		t.Errorf("expected team mission, got %v", settings["teamMission"])
	}
	if got, ok := settings["teamSpawnedAt"].(int64); !ok || got == 0 {
		t.Errorf("expected non-zero teamSpawnedAt, got %v", settings["teamSpawnedAt"])
	}
	if got, ok := settings["activeMeetingCount"].(int); !ok || got != 1 {
		t.Errorf("expected 1 active meeting, got %v", settings["activeMeetingCount"])
	}
	if got, ok := settings["activeLockCount"].(int); !ok || got != 1 {
		t.Errorf("expected 1 active lock, got %v", settings["activeLockCount"])
	}
	if got, ok := settings["lastBrainstormAt"].(int64); !ok || got == 0 {
		t.Errorf("expected non-zero lastBrainstormAt, got %v", settings["lastBrainstormAt"])
	}
}

// TestGetOfficeState_PopulatedOffice builds a rich office (members, standup
// transcript, whiteboard, messages, paused) and asserts GetOfficeState returns
// a coherent payload. This exercises BuildMemberPayload, GetMeetings,
// GetLocks, and the whiteboard/transcript copy branches.
func TestGetOfficeState_PopulatedOffice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "FE"})
	alpha := m.GetAgent("sess-a")
	alpha.AddTask(&Task{ID: "t1", Title: "Build", Status: TaskInProgress})

	// Standup with a transcript.
	m.SetStandup(&StandupState{
		ID:     "su-1",
		Active: true,
		Transcript: []StandupMessage{
			{Type: "status_report", SpeakerName: "Alpha", Content: "all good"},
		},
	})
	m.SetWhiteboardNote("sess-a", "wb note")
	m.SetPaused(true, "operator pause")

	state := m.GetOfficeState()

	if state["standupActive"] != true {
		t.Errorf("expected standupActive true, got %v", state["standupActive"])
	}
	if state["standupTranscript"] == nil {
		t.Error("expected standup transcript present")
	}
	members, ok := state["members"].([]map[string]interface{})
	if !ok || len(members) != 1 {
		t.Fatalf("expected 1 member payload, got %v", state["members"])
	}
	if state["paused"] != true {
		t.Errorf("expected paused true, got %v", state["paused"])
	}
}
