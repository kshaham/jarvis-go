package office

// R36 · Task labels + cost-by-feature rollups.
//
// Labels are free-form strings on Task. Operators use them to
// attribute work to features / sprints / initiatives and roll up
// cost + turn totals in the Insights "Cost by label" card.
//
// Accuracy note: per-task cost is the agent's cumulative cost
// delta between the task's in_progress and completed transitions
// (see Task.StartCostUSD / Task.FinalCostUSD, captured in
// UpdateTaskStatus). This is approximate when an agent runs
// concurrent tasks -- SDK sessions serialize turns so this is rare
// in practice, and the approximation is fine for the operator-level
// accountability this feature targets.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// TaskLabelMaxPerTask caps how many labels a single task can carry.
// Exists to stop accidental label-spam and keep the task-card UI
// tidy. Operators who legitimately need more can split the work
// into multiple tasks.
const TaskLabelMaxPerTask = 10

// TaskLabelMaxLength bounds a single label's length.
const TaskLabelMaxLength = 40

// labelRE is the canonical label validator. Lowercase ASCII, digits,
// and '-'; must start with alphanumeric; 1..40 chars.
var labelRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// NormalizeTaskLabel lowercases + trims whitespace. Agents sometimes
// pass "Auth-Fix " or " AUTH-FIX"; we accept and canonicalize.
func NormalizeTaskLabel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ErrTooManyTaskLabels is returned by NormalizeAndValidateLabels
// when the deduped label set exceeds TaskLabelMaxPerTask. Caller
// (handler) maps this to 400.
var ErrTooManyTaskLabels = fmt.Errorf("too many labels (max %d)", TaskLabelMaxPerTask)

// NormalizeAndValidateLabels takes a raw operator-supplied label
// list (from a JSON request body) and returns a deduped, lowercased,
// trimmed, individually-validated slice. Returns:
//   - validation error from ValidateTaskLabel when any single label
//     fails (handler returns 400 with the error text);
//   - ErrTooManyTaskLabels when the deduped set exceeds
//     TaskLabelMaxPerTask;
//   - the canonical slice + nil otherwise.
//
// Empty input yields an empty slice (not nil) so the caller can
// assign to Task.Labels without a guard.
//
// Pure helper — no I/O, no globals. Used by task-self-add and the
// labels editor endpoint.
func NormalizeAndValidateLabels(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		l := NormalizeTaskLabel(r)
		if err := ValidateTaskLabel(l); err != nil {
			return nil, err
		}
		if seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	if len(out) > TaskLabelMaxPerTask {
		return nil, ErrTooManyTaskLabels
	}
	return out, nil
}

// ValidateTaskLabel returns a descriptive error or nil. The string
// should already be normalized via NormalizeTaskLabel.
func ValidateTaskLabel(s string) error {
	if s == "" {
		return fmt.Errorf("label is empty")
	}
	if len(s) > TaskLabelMaxLength {
		return fmt.Errorf("label %q is %d chars (max %d)", s, len(s), TaskLabelMaxLength)
	}
	if !labelRE.MatchString(s) {
		return fmt.Errorf("label %q must match lowercase alphanumeric + '-' (start alphanumeric)", s)
	}
	return nil
}

// AddLabel normalizes and appends the label to the task's Labels
// slice if not already present. Returns an error on invalid input
// or cap breach. Thread-safe under the agent mutex.
func (a *AgentState) AddLabel(taskID, raw string) error {
	label := NormalizeTaskLabel(raw)
	if err := ValidateTaskLabel(label); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID != taskID {
			continue
		}
		// Dedup -- silent no-op on already-present label.
		for _, existing := range t.Labels {
			if existing == label {
				return nil
			}
		}
		if len(t.Labels) >= TaskLabelMaxPerTask {
			return fmt.Errorf("task already has %d labels (max %d)", len(t.Labels), TaskLabelMaxPerTask)
		}
		t.Labels = append(t.Labels, label)
		return nil
	}
	return fmt.Errorf("task not found")
}

// RemoveLabel normalizes and drops the label if present. Missing
// label is a silent no-op. Thread-safe.
func (a *AgentState) RemoveLabel(taskID, raw string) error {
	label := NormalizeTaskLabel(raw)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID != taskID {
			continue
		}
		for i, existing := range t.Labels {
			if existing == label {
				t.Labels = append(t.Labels[:i], t.Labels[i+1:]...)
				return nil
			}
		}
		return nil
	}
	return fmt.Errorf("task not found")
}

// ---------------------------------------------------------------------------
// Roll-ups
// ---------------------------------------------------------------------------

// LabelRollup is one row of the cost-by-label view.
type LabelRollup struct {
	Label           string  `json:"label"`
	TaskCount       int     `json:"taskCount"`
	Completed       int     `json:"completed"`
	InFlight        int     `json:"inFlight"`
	TotalCostUSD    float64 `json:"totalCostUSD"`
	TotalTurnCount  int64   `json:"totalTurnCount"`
	AvgTurnsPerTask float64 `json:"avgTurnsPerTask"`
}

// GetLabelRollups returns one LabelRollup per distinct label across
// the roster, sorted descending by TotalCostUSD (ties broken
// alphabetically). Empty list when no tasks carry labels.
func (m *Manager) GetLabelRollups() []LabelRollup {
	buckets := make(map[string]*LabelRollup)
	for _, agent := range m.GetRoster() {
		for _, t := range agent.GetTasks() {
			if len(t.Labels) == 0 {
				continue
			}
			completed := t.Status == TaskCompleted
			inFlight := t.Status == TaskInProgress || t.Status == TaskBlocked || t.Status == TaskHandoffPending
			for _, label := range t.Labels {
				b, ok := buckets[label]
				if !ok {
					b = &LabelRollup{Label: label}
					buckets[label] = b
				}
				b.TaskCount++
				if completed {
					b.Completed++
					b.TotalCostUSD += t.FinalCostUSD
					b.TotalTurnCount += t.FinalTurnCount
				}
				if inFlight {
					b.InFlight++
				}
			}
		}
	}
	out := make([]LabelRollup, 0, len(buckets))
	for _, b := range buckets {
		if b.Completed > 0 {
			b.AvgTurnsPerTask = float64(b.TotalTurnCount) / float64(b.Completed)
		}
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCostUSD != out[j].TotalCostUSD {
			return out[i].TotalCostUSD > out[j].TotalCostUSD
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// GetAllLabels returns the distinct label set across the roster,
// alphabetically sorted. Drives the UI autocomplete + filter chip
// dropdown.
func (m *Manager) GetAllLabels() []string {
	seen := make(map[string]struct{})
	for _, agent := range m.GetRoster() {
		for _, t := range agent.GetTasks() {
			for _, l := range t.Labels {
				seen[l] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
