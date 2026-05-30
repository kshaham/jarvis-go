package office

import (
	"sync"
	"testing"
)

func TestNewAgentState(t *testing.T) {
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend Engineer"}
	a := NewAgentState("sess-1", "vex", cfg)

	if a.SessionID() != "sess-1" {
		t.Errorf("expected sess-1, got %s", a.SessionID())
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

func TestAgentState_CompleteTask_HappyPath(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	a.AddTask(&Task{ID: "t1", Title: "Ship the feature", Status: TaskInProgress})

	res := a.CompleteTask("t1")
	if !res.Found {
		t.Fatal("Found=false; expected true")
	}
	if res.AlreadyComplete {
		t.Error("AlreadyComplete=true on first call; want false")
	}
	if res.Title != "Ship the feature" {
		t.Errorf("Title=%q want Ship the feature", res.Title)
	}
	if res.Task == nil {
		t.Fatal("Task=nil; want non-nil pointer")
	}
	if res.Task.Status != TaskCompleted {
		t.Errorf("Status=%s want TaskCompleted", res.Task.Status)
	}
	if res.Task.CompletedAt.IsZero() {
		t.Error("CompletedAt not set by applyTaskStatusLocked")
	}
}

func TestAgentState_CompleteTask_AlreadyComplete(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	a.AddTask(&Task{ID: "t1", Title: "Done", Status: TaskCompleted})

	res := a.CompleteTask("t1")
	if !res.Found || !res.AlreadyComplete {
		t.Errorf("Found=%v AlreadyComplete=%v; want both true",
			res.Found, res.AlreadyComplete)
	}
}

func TestAgentState_CompleteTask_NotFound(t *testing.T) {
	a := NewAgentState("s1", "vex", &TeamMemberConfig{})
	a.AddTask(&Task{ID: "t1", Status: TaskPending})

	res := a.CompleteTask("ghost-task")
	if res.Found {
		t.Error("Found=true for missing task; want false")
	}
	if res.Task != nil {
		t.Errorf("Task=%+v; want nil for missing task", res.Task)
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

func TestAgentStateSetVoice(t *testing.T) {
	cfg := &TeamMemberConfig{ID: "v1", Name: "Alice"}
	a := NewAgentState("sess-1", "v1", cfg)

	a.SetVoice("marin")
	if a.Config.Voice != "marin" {
		t.Errorf("Config.Voice = %q, want marin", a.Config.Voice)
	}

	// Overwrite with a different voice.
	a.SetVoice("cedar")
	if a.Config.Voice != "cedar" {
		t.Errorf("Config.Voice = %q, want cedar after overwrite", a.Config.Voice)
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

func TestAgentStateGetVoiceUnderLock(t *testing.T) {
	a := &AgentState{Config: &TeamMemberConfig{}}
	if a.GetVoice() != "" {
		t.Fatalf("expected empty voice initially")
	}
	a.SetVoice("marin")
	if got := a.GetVoice(); got != "marin" {
		t.Fatalf("GetVoice = %q, want marin", got)
	}
	// nil Config must not panic.
	var b AgentState
	if b.GetVoice() != "" {
		t.Fatalf("nil-config GetVoice should be empty")
	}
}

// TestTaskGateInfoReturnsCopiedCriteria verifies TaskGateInfo returns correct
// values and that mutating the returned slice does not affect the live task.
func TestTaskGateInfoReturnsCopiedCriteria(t *testing.T) {
	a := &AgentState{taskQueue: &TaskQueue{Tasks: []*Task{{
		ID:                  "tg1",
		Status:              TaskInProgress,
		AcceptanceCriteria:  []string{"crit-a", "crit-b"},
		Labels:              []string{"gated"},
		CriteriaRejectCount: 3,
	}}}}

	criteria, gated, rejectCount, found := a.TaskGateInfo("tg1")
	if !found {
		t.Fatal("TaskGateInfo: found=false, want true")
	}
	if !gated {
		t.Error("TaskGateInfo: gated=false, want true")
	}
	if rejectCount != 3 {
		t.Errorf("TaskGateInfo: rejectCount=%d, want 3", rejectCount)
	}
	if len(criteria) != 2 || criteria[0] != "crit-a" || criteria[1] != "crit-b" {
		t.Errorf("TaskGateInfo: criteria=%v, want [crit-a crit-b]", criteria)
	}

	// Mutate the returned slice; the live task must be unaffected.
	criteria[0] = "MUTATED"
	task := a.taskQueue.Tasks[0]
	if task.AcceptanceCriteria[0] != "crit-a" {
		t.Errorf("live task criteria[0] was mutated to %q, want crit-a", task.AcceptanceCriteria[0])
	}
}

// TestTaskGateInfoRaceWithSetCriteria runs TaskGateInfo concurrently with
// SetTaskCriteria to detect data races under -race.
func TestTaskGateInfoRaceWithSetCriteria(t *testing.T) {
	a := &AgentState{taskQueue: &TaskQueue{Tasks: []*Task{{
		ID:                 "race-task",
		Status:             TaskInProgress,
		AcceptanceCriteria: []string{"initial"},
	}}}}

	const iters = 100
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			a.TaskGateInfo("race-task")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.SetTaskCriteria("race-task", "set", []string{"updated"})
		}
	}()
	wg.Wait()
}
