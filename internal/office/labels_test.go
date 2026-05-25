package office

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeAndValidateLabels_EmptyInputEmptySlice(t *testing.T) {
	got, err := NormalizeAndValidateLabels(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestNormalizeAndValidateLabels_LowercasesAndTrims(t *testing.T) {
	got, err := NormalizeAndValidateLabels([]string{"  Needs-Review  ", "BUG"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "needs-review" || got[1] != "bug" {
		t.Errorf("normalisation wrong: %+v", got)
	}
}

func TestNormalizeAndValidateLabels_DedupesAfterNormalisation(t *testing.T) {
	// Three inputs that all normalise to the same label.
	got, err := NormalizeAndValidateLabels([]string{"bug", "BUG", "  Bug  "})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0] != "bug" {
		t.Errorf("expected dedupe to [bug], got %+v", got)
	}
}

func TestNormalizeAndValidateLabels_InvalidLabelReturnsError(t *testing.T) {
	// Empty after trim → ValidateTaskLabel returns "label is empty".
	_, err := NormalizeAndValidateLabels([]string{"valid", "   "})
	if err == nil {
		t.Fatal("expected error for empty-after-trim label")
	}
	if errors.Is(err, ErrTooManyTaskLabels) {
		t.Error("should not return ErrTooManyTaskLabels for per-label validation error")
	}
}

func TestNormalizeAndValidateLabels_OverCapReturnsSentinel(t *testing.T) {
	// Build a list of TaskLabelMaxPerTask+1 unique labels.
	raw := make([]string, TaskLabelMaxPerTask+1)
	for i := range raw {
		raw[i] = "label-" + string(rune('a'+i))
	}
	_, err := NormalizeAndValidateLabels(raw)
	if !errors.Is(err, ErrTooManyTaskLabels) {
		t.Errorf("expected ErrTooManyTaskLabels, got %v", err)
	}
}

func TestNormalizeAndValidateLabels_DupesUnderCapStillFit(t *testing.T) {
	// TaskLabelMaxPerTask+5 labels, but all duplicates → dedupe
	// brings it well under the cap. Should NOT error.
	raw := make([]string, TaskLabelMaxPerTask+5)
	for i := range raw {
		raw[i] = "needs-review"
	}
	got, err := NormalizeAndValidateLabels(raw)
	if err != nil {
		t.Errorf("dedupe-under-cap should not error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len=%d want 1", len(got))
	}
}

func TestValidateTaskLabel(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"auth-fix", false},
		{"q2-bugs", false},
		{"feature1", false},
		{"1abc", false},
		{"", true},
		{"UPPER", true},
		{"-leading-dash", true},
		{"has spaces", true},
		{"trailing-", false}, // dash mid/end is fine; leading is not
		{"unicode-é", true},
		{strings.Repeat("x", 41), true},
		{strings.Repeat("x", 40), false},
	}
	for _, c := range cases {
		err := ValidateTaskLabel(c.in)
		if c.wantErr && err == nil {
			t.Errorf("ValidateTaskLabel(%q) should have errored", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("ValidateTaskLabel(%q) unexpected error: %v", c.in, err)
		}
	}
}

func TestNormalizeTaskLabel(t *testing.T) {
	if got := NormalizeTaskLabel("  Auth-Fix  "); got != "auth-fix" {
		t.Errorf("normalize got %q", got)
	}
}

func TestAddLabelToTask_Happy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t-1", Status: TaskPending})
	if err := a.AddLabel("t-1", "AUTH-FIX"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	tasks := a.GetTasks()
	if len(tasks[0].Labels) != 1 || tasks[0].Labels[0] != "auth-fix" {
		t.Errorf("labels after add: %v (should be [auth-fix])", tasks[0].Labels)
	}
	// Second add is a no-op (dedup).
	if err := a.AddLabel("t-1", "auth-fix"); err != nil {
		t.Errorf("dup AddLabel should be no-op, got %v", err)
	}
	if len(a.GetTasks()[0].Labels) != 1 {
		t.Error("dup add should not grow labels")
	}
}

func TestAddLabelToTask_CapEnforced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t-1", Status: TaskPending})
	for i := 0; i < TaskLabelMaxPerTask; i++ {
		label := "label-" + string(rune('a'+i))
		if err := a.AddLabel("t-1", label); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	err := a.AddLabel("t-1", "one-too-many")
	if err == nil || !strings.Contains(err.Error(), "max") {
		t.Fatalf("expected cap error, got %v", err)
	}
}

func TestAddLabelToTask_InvalidRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t-1", Status: TaskPending})
	err := a.AddLabel("t-1", "BAD LABEL WITH SPACES")
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestRemoveLabelFromTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t-1", Status: TaskPending, Labels: []string{"auth", "q2"}})
	if err := a.RemoveLabel("t-1", "auth"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	tasks := a.GetTasks()
	if len(tasks[0].Labels) != 1 || tasks[0].Labels[0] != "q2" {
		t.Errorf("after remove: %v", tasks[0].Labels)
	}
	// Remove missing label is silent no-op.
	if err := a.RemoveLabel("t-1", "not-there"); err != nil {
		t.Errorf("remove-missing should be no-op: %v", err)
	}
}

func TestCostDeltaOnCompletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t-1", Status: TaskPending})

	// Cost before task starts = 1.0, turn count = 3.
	a.RecordTurnUsage(100, 50, 1.0)
	a.RecordTurnUsage(50, 25, 0.0)
	// Totals now: cost=1.0, turns=2 (TurnCount increments once per call)
	// Start the task -- should snapshot both.
	if !a.UpdateTaskStatus("t-1", TaskInProgress) {
		t.Fatal("status update returned false")
	}
	task := a.GetTasks()[0]
	if task.StartCostUSD != 1.0 {
		t.Errorf("start cost = %v, want 1.0", task.StartCostUSD)
	}
	if task.StartTurnCount != 2 {
		t.Errorf("start turn count = %d, want 2", task.StartTurnCount)
	}

	// More cost during the task.
	a.RecordTurnUsage(200, 100, 2.5)
	a.RecordTurnUsage(50, 25, 0.0)
	// Totals now: cost=3.5, turns=4.
	if !a.UpdateTaskStatus("t-1", TaskCompleted) {
		t.Fatal("status update returned false")
	}
	task = a.GetTasks()[0]
	if task.FinalCostUSD != 2.5 {
		t.Errorf("final cost delta = %v, want 2.5", task.FinalCostUSD)
	}
	if task.FinalTurnCount != 2 {
		t.Errorf("final turn delta = %d, want 2", task.FinalTurnCount)
	}
}

func TestGetLabelRollups(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	m.RegisterAgent("s-bolt", "bolt", &TeamMemberConfig{ID: "bolt", Tier: "worker"})
	vex := m.GetAgent("s-vex")
	bolt := m.GetAgent("s-bolt")

	// auth: 2 completed tasks (cost 1.0 + 2.0 = 3.0), 1 in-flight.
	vex.AddTask(&Task{ID: "t1", Status: TaskCompleted, Labels: []string{"auth"}, FinalCostUSD: 1.0, FinalTurnCount: 3})
	bolt.AddTask(&Task{ID: "t2", Status: TaskCompleted, Labels: []string{"auth", "q2"}, FinalCostUSD: 2.0, FinalTurnCount: 5})
	vex.AddTask(&Task{ID: "t3", Status: TaskInProgress, Labels: []string{"auth"}})
	// q2: 1 completed (cost 2.0), 0 in-flight.
	// q3: 1 in-flight, 0 completed.
	vex.AddTask(&Task{ID: "t4", Status: TaskBlocked, Labels: []string{"q3"}})

	roll := m.GetLabelRollups()
	// Expect 3 labels, sorted by total cost desc: auth(3.0) > q2(2.0) > q3(0.0)
	if len(roll) != 3 {
		t.Fatalf("expected 3 rollups, got %d", len(roll))
	}
	if roll[0].Label != "auth" || roll[0].TotalCostUSD != 3.0 || roll[0].Completed != 2 || roll[0].InFlight != 1 {
		t.Errorf("auth rollup wrong: %+v", roll[0])
	}
	if roll[1].Label != "q2" || roll[1].TotalCostUSD != 2.0 || roll[1].Completed != 1 {
		t.Errorf("q2 rollup wrong: %+v", roll[1])
	}
	if roll[2].Label != "q3" || roll[2].InFlight != 1 || roll[2].TotalCostUSD != 0 {
		t.Errorf("q3 rollup wrong: %+v", roll[2])
	}
	if roll[0].AvgTurnsPerTask != 4.0 {
		t.Errorf("auth avg turns = %v, want 4 (3+5)/2", roll[0].AvgTurnsPerTask)
	}
}

func TestGetAllLabels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Tier: "worker"})
	a := m.GetAgent("s-vex")
	a.AddTask(&Task{ID: "t1", Labels: []string{"auth", "q2"}})
	a.AddTask(&Task{ID: "t2", Labels: []string{"auth", "frontend"}})
	got := m.GetAllLabels()
	want := []string{"auth", "frontend", "q2"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
