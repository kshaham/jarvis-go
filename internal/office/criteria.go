package office

import (
	"errors"
	"strconv"
	"strings"
)

const (
	maxCriteriaPerTask = 10
	maxCriterionLen    = 200
)

// ValidateAcceptanceCriterion returns an error when the criterion string
// is empty (after trimming) or exceeds maxCriterionLen characters.
func ValidateAcceptanceCriterion(s string) error {
	t := strings.TrimSpace(s)
	if t == "" {
		return errors.New("criterion must not be empty")
	}
	if len(t) > maxCriterionLen {
		return errors.New("criterion too long")
	}
	return nil
}

// SetTaskCriteria mutates a task's AcceptanceCriteria. op is "set" | "add" | "remove".
// For "remove", values are 0-based indices (as strings); pass a SINGLE index per call
// (removal mutates the slice, so multiple indices in one call would shift positions).
// Locks a.mu; never changes task status.
func (a *AgentState) SetTaskCriteria(taskID, op string, values []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var task *Task
	for _, t := range a.taskQueue.Tasks {
		if t != nil && t.ID == taskID {
			task = t
			break
		}
	}
	if task == nil {
		return errors.New("task not found")
	}
	switch op {
	case "set", "add":
		base := []string{}
		if op == "add" {
			base = append(base, task.AcceptanceCriteria...)
		}
		for _, v := range values {
			if err := ValidateAcceptanceCriterion(v); err != nil {
				return err
			}
			base = append(base, strings.TrimSpace(v))
		}
		if len(base) > maxCriteriaPerTask {
			return errors.New("too many acceptance criteria (max 10)")
		}
		task.AcceptanceCriteria = base
	case "remove":
		for _, v := range values {
			idx, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || idx < 0 || idx >= len(task.AcceptanceCriteria) {
				return errors.New("invalid criterion index")
			}
			task.AcceptanceCriteria = append(task.AcceptanceCriteria[:idx], task.AcceptanceCriteria[idx+1:]...)
		}
	default:
		return errors.New("unknown op")
	}
	return nil
}

// renderTaskCriteriaSection returns a markdown checklist for the task's
// AcceptanceCriteria plus a suggested /goal line. Returns empty string when
// the task has no criteria so callers can unconditionally append it.
func renderTaskCriteriaSection(task *Task) string {
	if task == nil || len(task.AcceptanceCriteria) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Acceptance Criteria\n")
	for _, c := range task.AcceptanceCriteria {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	b.WriteString("\nSuggested: `/goal All acceptance criteria met: ")
	b.WriteString(strings.Join(task.AcceptanceCriteria, "; "))
	b.WriteString("; then cm task complete ")
	b.WriteString(task.ID)
	b.WriteString("`")
	return b.String()
}

// IncrementCriteriaReject bumps the reject counter without touching status.
// Returns the new count (0 if task not found).
func (a *AgentState) IncrementCriteriaReject(taskID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t != nil && t.ID == taskID {
			t.CriteriaRejectCount++
			return t.CriteriaRejectCount
		}
	}
	return 0
}
