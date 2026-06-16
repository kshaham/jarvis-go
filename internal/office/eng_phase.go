package office

import (
	"errors"
	"strings"
)

// EngPhases are the 7 engineering phases of the agent-skills SDLC, in order.
// Agents record a task's current phase via `cm task phase`; advisory only.
var EngPhases = []string{"define", "plan", "build", "verify", "review", "simplify", "ship"}

// engPhaseSet is the lookup form of EngPhases.
var engPhaseSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(EngPhases))
	for _, p := range EngPhases {
		m[p] = struct{}{}
	}
	return m
}()

// ValidateEngPhase returns an error when phase is not one of the 7 known phases.
// The empty string is NOT valid here -- callers clear a phase by not setting it.
func ValidateEngPhase(phase string) error {
	p := strings.ToLower(strings.TrimSpace(phase))
	if p == "" {
		return errors.New("phase must not be empty")
	}
	if _, ok := engPhaseSet[p]; !ok {
		return errors.New("unknown phase (want one of: " + strings.Join(EngPhases, ", ") + ")")
	}
	return nil
}

// SetTaskPhase sets a task's EngPhase. Validates against the 7 known phases,
// locks a.mu, and NEVER changes task Status -- mirrors SetTaskCriteria.
func (a *AgentState) SetTaskPhase(taskID, phase string) error {
	p := strings.ToLower(strings.TrimSpace(phase))
	if err := ValidateEngPhase(p); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t != nil && t.ID == taskID {
			t.EngPhase = p
			return nil
		}
	}
	return errors.New("task not found")
}
