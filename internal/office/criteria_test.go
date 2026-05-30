package office

import (
	"strings"
	"testing"
)

func TestValidateAcceptanceCriterion(t *testing.T) {
	if err := ValidateAcceptanceCriterion("all tests pass"); err != nil {
		t.Fatalf("valid criterion rejected: %v", err)
	}
	if ValidateAcceptanceCriterion("") == nil {
		t.Fatalf("empty must be rejected")
	}
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	if ValidateAcceptanceCriterion(string(long)) == nil {
		t.Fatalf("over-length must be rejected")
	}
}

func TestSetTaskCriteriaAndReject(t *testing.T) {
	// taskQueue is a *TaskQueue (agent.go:170), nil on a bare AgentState{} --
	// initialize it explicitly.
	a := &AgentState{taskQueue: &TaskQueue{Tasks: []*Task{{ID: "t1", Status: TaskInProgress}}}}
	if err := a.SetTaskCriteria("t1", "set", []string{"crit a", "crit b"}); err != nil {
		t.Fatal(err)
	}
	task := findTaskForTest(a, "t1")
	if len(task.AcceptanceCriteria) != 2 {
		t.Fatalf("want 2 criteria, got %d", len(task.AcceptanceCriteria))
	}
	_ = a.SetTaskCriteria("t1", "add", []string{"crit c"})
	if len(findTaskForTest(a, "t1").AcceptanceCriteria) != 3 {
		t.Fatalf("add failed")
	}
	_ = a.SetTaskCriteria("t1", "remove", []string{"0"}) // remove first by index
	if len(findTaskForTest(a, "t1").AcceptanceCriteria) != 2 {
		t.Fatalf("remove failed")
	}
	// reject counter does not transition status
	n := a.IncrementCriteriaReject("t1")
	if n != 1 || findTaskForTest(a, "t1").Status != TaskInProgress {
		t.Fatalf("reject must not change status; n=%d status=%s", n, findTaskForTest(a, "t1").Status)
	}
}

func TestRenderTaskCriteriaSection(t *testing.T) {
	if renderTaskCriteriaSection(&Task{ID: "t1"}) != "" {
		t.Fatalf("no criteria -> empty")
	}
	out := renderTaskCriteriaSection(&Task{ID: "t1", AcceptanceCriteria: []string{"tests pass", "lint clean"}})
	if !strings.Contains(out, "Acceptance Criteria") ||
		!strings.Contains(out, "- tests pass") ||
		!strings.Contains(out, "/goal") {
		t.Fatalf("expected criteria checklist + /goal suggestion:\n%s", out)
	}
}

// TestSetTaskCriteriaRemoveRejectsMultipleIndices verifies that passing more
// than one index to the "remove" op returns an error and leaves the criteria
// slice unchanged.
func TestSetTaskCriteriaRemoveRejectsMultipleIndices(t *testing.T) {
	a := &AgentState{taskQueue: &TaskQueue{Tasks: []*Task{{ID: "t1", Status: TaskInProgress}}}}
	if err := a.SetTaskCriteria("t1", "set", []string{"alpha", "beta"}); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	err := a.SetTaskCriteria("t1", "remove", []string{"0", "1"})
	if err == nil {
		t.Fatal("expected error when remove receives two indices, got nil")
	}
	task := findTaskForTest(a, "t1")
	if len(task.AcceptanceCriteria) != 2 {
		t.Fatalf("criteria must be unchanged after rejected remove, got %v", task.AcceptanceCriteria)
	}
}

// findTaskForTest is a test-only helper (no locking).
func findTaskForTest(a *AgentState, id string) *Task {
	for _, t := range a.taskQueue.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}
