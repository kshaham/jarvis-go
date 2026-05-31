package office

import (
	"strings"
	"testing"
)

func criteriaAgent() *AgentState {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	a.AddTask(&Task{ID: "t1"})
	return a
}

// TestSetTaskCriteria_TaskNotFound covers the task-not-found error.
func TestSetTaskCriteria_TaskNotFound(t *testing.T) {
	a := criteriaAgent()
	if err := a.SetTaskCriteria("ghost", "set", []string{"x"}); err == nil {
		t.Fatal("expected task-not-found error")
	}
}

// TestSetTaskCriteria_ValidationError covers the empty-criterion rejection
// inside the set/add loop.
func TestSetTaskCriteria_ValidationError(t *testing.T) {
	a := criteriaAgent()
	if err := a.SetTaskCriteria("t1", "add", []string{"   "}); err == nil {
		t.Fatal("expected validation error for empty criterion")
	}
}

// TestSetTaskCriteria_TooMany covers the maxCriteriaPerTask guard.
func TestSetTaskCriteria_TooMany(t *testing.T) {
	a := criteriaAgent()
	vals := make([]string, maxCriteriaPerTask+1)
	for i := range vals {
		vals[i] = "criterion"
	}
	if err := a.SetTaskCriteria("t1", "set", vals); err == nil {
		t.Fatal("expected too-many-criteria error")
	}
}

// TestSetTaskCriteria_RemoveWrongCount covers the "remove takes exactly one
// index" guard.
func TestSetTaskCriteria_RemoveWrongCount(t *testing.T) {
	a := criteriaAgent()
	if err := a.SetTaskCriteria("t1", "remove", []string{"0", "1"}); err == nil {
		t.Fatal("expected remove-count error")
	}
}

// TestSetTaskCriteria_RemoveInvalidIndex covers the invalid-index guard
// (non-numeric and out-of-range).
func TestSetTaskCriteria_RemoveInvalidIndex(t *testing.T) {
	a := criteriaAgent()
	_ = a.SetTaskCriteria("t1", "set", []string{"only one"})
	if err := a.SetTaskCriteria("t1", "remove", []string{"notnum"}); err == nil {
		t.Fatal("expected invalid-index error for non-numeric")
	}
	if err := a.SetTaskCriteria("t1", "remove", []string{"5"}); err == nil {
		t.Fatal("expected invalid-index error for out-of-range")
	}
}

// TestSetTaskCriteria_RemoveValid removes a real criterion.
func TestSetTaskCriteria_RemoveValid(t *testing.T) {
	a := criteriaAgent()
	if err := a.SetTaskCriteria("t1", "set", []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTaskCriteria("t1", "remove", []string{"1"}); err != nil {
		t.Fatal(err)
	}
	crit, _, _, _ := a.TaskGateInfo("t1")
	if len(crit) != 2 || crit[0] != "a" || crit[1] != "c" {
		t.Errorf("expected [a c] after removing index 1, got %v", crit)
	}
}

// TestSetTaskCriteria_UnknownOp covers the default error branch.
func TestSetTaskCriteria_UnknownOp(t *testing.T) {
	a := criteriaAgent()
	if err := a.SetTaskCriteria("t1", "frobnicate", []string{"x"}); err == nil {
		t.Fatal("expected unknown-op error")
	}
}

// TestIncrementCriteriaReject_NotFound covers the 0-return for missing task.
func TestIncrementCriteriaReject_NotFound(t *testing.T) {
	a := criteriaAgent()
	if got := a.IncrementCriteriaReject("ghost"); got != 0 {
		t.Errorf("expected 0 for missing task, got %d", got)
	}
}

// TestValidateAcceptanceCriterion_TooLong covers the length guard.
func TestValidateAcceptanceCriterion_TooLong(t *testing.T) {
	if err := ValidateAcceptanceCriterion(strings.Repeat("x", maxCriterionLen+1)); err == nil {
		t.Fatal("expected too-long error")
	}
}
