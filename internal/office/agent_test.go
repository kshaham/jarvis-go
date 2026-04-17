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

func TestTask_ProgressLogAppendsAndCaps(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	a.AddTask(&Task{ID: "t1", Title: "Build", Status: TaskInProgress})

	// Push 25 notes; only the latest taskProgressLogCap (20) should remain.
	for i := 0; i < 25; i++ {
		if !a.SetTaskProgress("t1", "note-"+itoa(i)) {
			t.Fatalf("SetTaskProgress returned false for iteration %d", i)
		}
	}

	tasks := a.GetTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0].ProgressLog
	if len(got) != taskProgressLogCap {
		t.Fatalf("expected %d progress entries after cap, got %d", taskProgressLogCap, len(got))
	}
	// Latest entry wins: description should reflect the last note.
	if tasks[0].Description != "note-24" {
		t.Errorf("expected Description note-24, got %q", tasks[0].Description)
	}
	// Oldest retained entry should be note-5 (25 pushed - 20 kept).
	if got[0].Note != "note-5" {
		t.Errorf("expected oldest retained note-5, got %q", got[0].Note)
	}
	if got[len(got)-1].Note != "note-24" {
		t.Errorf("expected newest note-24, got %q", got[len(got)-1].Note)
	}
}

func TestAgentState_AccumulatesCost(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})

	a.RecordTurnUsage(100, 50, 0.01)
	a.RecordTurnUsage(200, 75, 0.02)
	a.RecordTurnUsage(50, 20, 0.005)

	u := a.Usage()
	if u.TurnCount != 3 {
		t.Errorf("expected 3 turns, got %d", u.TurnCount)
	}
	if u.TotalInputTokens != 350 {
		t.Errorf("expected 350 input tokens, got %d", u.TotalInputTokens)
	}
	if u.TotalOutputTokens != 145 {
		t.Errorf("expected 145 output tokens, got %d", u.TotalOutputTokens)
	}
	// Float-equality tolerance for summed floats.
	if got := u.TotalCostUSD; got < 0.0349 || got > 0.0351 {
		t.Errorf("expected ~0.035 total cost, got %f", got)
	}
	if len(u.RecentTurnCosts) != 3 {
		t.Errorf("expected 3 recent turn costs, got %d", len(u.RecentTurnCosts))
	}
	if u.LastTurnAt.IsZero() {
		t.Error("expected LastTurnAt to be set")
	}
}

// Minimal int-to-string helper so the test file has no extra imports.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
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
