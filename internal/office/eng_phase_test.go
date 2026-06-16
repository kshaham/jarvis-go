package office

import "testing"

func TestValidateEngPhase(t *testing.T) {
	for _, p := range EngPhases {
		if err := ValidateEngPhase(p); err != nil {
			t.Errorf("expected %q valid, got %v", p, err)
		}
	}
	if err := ValidateEngPhase("SHIP"); err != nil {
		t.Errorf("expected case-insensitive accept of SHIP, got %v", err)
	}
	for _, bad := range []string{"", "deploy", "qa", "done"} {
		if err := ValidateEngPhase(bad); err == nil {
			t.Errorf("expected %q invalid", bad)
		}
	}
}

func TestSetTaskPhase(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Pat", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-p")
	agent.AddTask(&Task{ID: "t-1", Title: "Build feature", Status: TaskInProgress})

	if err := agent.SetTaskPhase("t-1", "build"); err != nil {
		t.Fatalf("SetTaskPhase: %v", err)
	}
	var got string
	for _, tk := range agent.GetTasks() {
		if tk.ID == "t-1" {
			got = tk.EngPhase
		}
	}
	if got != "build" {
		t.Errorf("expected EngPhase=build, got %q", got)
	}
	// Status must be untouched (advisory only).
	if agent.ActiveTask() == nil || agent.ActiveTask().Status != TaskInProgress {
		t.Error("SetTaskPhase must not change task status")
	}

	if err := agent.SetTaskPhase("t-1", "nope"); err == nil {
		t.Error("expected invalid phase to error")
	}
	if err := agent.SetTaskPhase("missing", "ship"); err == nil {
		t.Error("expected unknown task to error")
	}
}
