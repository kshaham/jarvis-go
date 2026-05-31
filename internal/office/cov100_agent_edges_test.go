package office

import (
	"testing"
)

// TestDeepCopyTask_Nil covers the nil-input early return.
func TestDeepCopyTask_Nil(t *testing.T) {
	if deepCopyTask(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

// TestTaskGateInfo_NotFound covers the not-found return.
func TestTaskGateInfo_NotFound(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	_, _, _, found := a.TaskGateInfo("ghost")
	if found {
		t.Error("expected found=false for unknown task")
	}
}

// TestTaskGateInfo_Found covers the populated branch (criteria, gated,
// reject count).
func TestTaskGateInfo_Found(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	a.AddTask(&Task{
		ID:                  "t",
		AcceptanceCriteria:  []string{"build passes", "tests pass"},
		CriteriaRejectCount: 2,
	})
	criteria, _, rejectCount, found := a.TaskGateInfo("t")
	if !found {
		t.Fatal("expected found=true")
	}
	if len(criteria) != 2 || rejectCount != 2 {
		t.Errorf("unexpected gate info: %v reject=%d", criteria, rejectCount)
	}
}

// TestUpdateTaskStatus_NotFound covers the not-found false return.
func TestUpdateTaskStatus_NotFound(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	if a.UpdateTaskStatus("ghost", TaskCompleted) {
		t.Error("expected false for unknown task")
	}
}

// TestSetTaskProgress_NotFound covers the not-found false return.
func TestSetTaskProgress_NotFound(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	if a.SetTaskProgress("ghost", "progress") {
		t.Error("expected false for unknown task")
	}
}

// TestSetTaskProgress_LogCap drives the progress-log cap trim by appending
// many entries to the same task.
func TestSetTaskProgress_LogCap(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	a.AddTask(&Task{ID: "t"})
	for i := 0; i < taskProgressLogCap+5; i++ {
		if !a.SetTaskProgress("t", "step") {
			t.Fatal("expected progress set to succeed")
		}
	}
	tasks := a.GetTasksCopy()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if len(tasks[0].ProgressLog) > taskProgressLogCap {
		t.Errorf("expected progress log capped at %d, got %d", taskProgressLogCap, len(tasks[0].ProgressLog))
	}
}

// TestRecordTurnUsage_CostHistoryCap drives the recentTurnCosts cap trim.
func TestRecordTurnUsage_CostHistoryCap(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	for i := 0; i < recentTurnCostsCap+10; i++ {
		a.RecordTurnUsage(1, 1, 0.001)
	}
	a.mu.Lock()
	n := len(a.RecentTurnCosts)
	a.mu.Unlock()
	if n > recentTurnCostsCap {
		t.Errorf("expected recent turn costs capped at %d, got %d", recentTurnCostsCap, n)
	}
}

// TestSnapshotConfig_NilConfig covers the nil-config zero-value return.
func TestSnapshotConfig_NilConfig(t *testing.T) {
	a := NewAgentState("s", "c", nil)
	cfg := a.SnapshotConfig()
	if cfg.ID != "" || cfg.Name != "" {
		t.Errorf("expected zero config, got %+v", cfg)
	}
}
