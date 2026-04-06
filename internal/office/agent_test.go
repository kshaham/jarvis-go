package office

import "testing"

func TestNewAgentState(t *testing.T) {
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend Engineer"}
	a := NewAgentState("sess-1", "vex", cfg)

	if a.SessionID != "sess-1" {
		t.Errorf("expected sess-1, got %s", a.SessionID)
	}
	if a.ConfigID != "vex" {
		t.Errorf("expected vex, got %s", a.ConfigID)
	}
	if len(a.GetTasks()) != 0 {
		t.Error("expected empty task queue")
	}
}

func TestAgentState_ActiveTask(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	if a.ActiveTask() != nil {
		t.Error("expected nil active task")
	}

	a.AddTask(&Task{
		ID: "t1", Title: "Fix bug", Status: TaskPending,
	})
	if a.ActiveTask() != nil {
		t.Error("pending task should not be active")
	}

	a.UpdateTaskStatus("t1", TaskInProgress)
	active := a.ActiveTask()
	if active == nil || active.ID != "t1" {
		t.Error("expected active task t1")
	}
}

func TestAgentState_PendingTaskCount(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	a.AddTask(&Task{ID: "t1", Status: TaskPending})
	a.AddTask(&Task{ID: "t2", Status: TaskAcknowledged})
	a.AddTask(&Task{ID: "t3", Status: TaskInProgress})
	a.AddTask(&Task{ID: "t4", Status: TaskCompleted})
	if count := a.PendingTaskCount(); count != 2 {
		t.Errorf("expected 2 pending, got %d", count)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.StandupMaxNudges != 3 {
		t.Errorf("expected 3 max nudges, got %d", cfg.StandupMaxNudges)
	}
	if cfg.MaxBugTasks != 100 {
		t.Errorf("expected 100 max bug tasks, got %d", cfg.MaxBugTasks)
	}
	if cfg.FileLockTTL != 30*60*1e9 {
		t.Errorf("expected 30min lock TTL, got %v", cfg.FileLockTTL)
	}
}
