package office

import (
	"testing"
	"time"
)

func newPRTestManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	return NewManager(&recordingSessionStore{}, func(string, interface{}) {}, nil)
}

func TestSetTeamPRNumber_RoundTrip(t *testing.T) {
	m := newPRTestManager(t)
	if got := m.GetTeamPRNumber(); got != 0 {
		t.Errorf("default GetTeamPRNumber = %d, want 0", got)
	}
	m.SetTeamPRNumber(1234)
	if got := m.GetTeamPRNumber(); got != 1234 {
		t.Errorf("after SetTeamPRNumber(1234) = %d, want 1234", got)
	}
}

func TestSetTeamPRNumber_ZeroClears(t *testing.T) {
	m := newPRTestManager(t)
	m.SetTeamPRNumber(7)
	m.SetTeamPRNumber(0)
	if got := m.GetTeamPRNumber(); got != 0 {
		t.Errorf("after clear = %d, want 0", got)
	}
}

func TestGetOfficeState_IncludesPRNumber(t *testing.T) {
	m := newPRTestManager(t)
	m.SetTeamPRNumber(42)
	state := m.GetOfficeState()
	pr, ok := state["prNumber"]
	if !ok {
		t.Fatal("GetOfficeState missing 'prNumber' key")
	}
	// JSON-like map: integer is stored as int when set via SetTeamPRNumber.
	if pr != 42 {
		t.Errorf("prNumber=%v, want 42", pr)
	}
}

func TestGetOfficeState_PRNumberZeroDefault(t *testing.T) {
	m := newPRTestManager(t)
	state := m.GetOfficeState()
	pr, ok := state["prNumber"]
	if !ok {
		t.Fatal("GetOfficeState missing 'prNumber' key even on default state")
	}
	if pr != 0 {
		t.Errorf("default prNumber=%v, want 0", pr)
	}
}
