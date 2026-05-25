package office

import (
	"math"
	"testing"
)

func TestCompletionSignal(t *testing.T) {
	cases := []struct {
		status TaskStatus
		want   float64
	}{
		{TaskCompleted, 1.0},
		{TaskFailed, 0.3},
		{TaskPending, 0.0},
		{TaskInProgress, 0.0},
	}
	for _, tc := range cases {
		if got := CompletionSignal(tc.status); got != tc.want {
			t.Errorf("CompletionSignal(%s)=%v want %v", tc.status, got, tc.want)
		}
	}
}

func TestSeverityWeight(t *testing.T) {
	cases := []struct {
		severity string
		want     float64
	}{
		{"blocker", 1.0},
		{"warning", 0.6},
		{"nit", 0.2},
		{"info", 0.0},
		{"none", 0.0},
		{"", 0.0},
		{"made-up", 0.3}, // unknown → mild penalty
	}
	for _, tc := range cases {
		if got := SeverityWeight(tc.severity); got != tc.want {
			t.Errorf("SeverityWeight(%q)=%v want %v", tc.severity, got, tc.want)
		}
	}
}

func TestComputeOutcomeScore_PerfectRun(t *testing.T) {
	// Completed, no review findings, perfect quality, no recovery.
	// Should produce the max score: 0.40 + 0.25 + 0.20 + 0.15 = 1.00
	got := ComputeOutcomeScore(TaskCompleted, "none", 1.0, 0)
	want := 1.0
	if !approxEqual(got, want) {
		t.Errorf("perfect-run score=%v want %v", got, want)
	}
}

func TestComputeOutcomeScore_BlockerHitTanksReviewComponent(t *testing.T) {
	// Completed but with a blocker: review component contributes 0
	// while everything else contributes max.
	// 0.40 + 0 + 0.20 + 0.15 = 0.75
	got := ComputeOutcomeScore(TaskCompleted, "blocker", 1.0, 0)
	want := 0.75
	if !approxEqual(got, want) {
		t.Errorf("blocker-hit score=%v want %v", got, want)
	}
}

func TestComputeOutcomeScore_FailedHalvesCompletionComponent(t *testing.T) {
	// Failed task: completion=0.3. Other components untouched.
	// 0.40*0.3 + 0.25 + 0.20 + 0.15 = 0.12 + 0.60 = 0.72
	got := ComputeOutcomeScore(TaskFailed, "none", 1.0, 0)
	want := 0.72
	if !approxEqual(got, want) {
		t.Errorf("failed score=%v want %v", got, want)
	}
}

func TestComputeOutcomeScore_RecoveryAttemptsScale(t *testing.T) {
	// 0 attempts → full recovery component (0.15)
	// 1 attempt → 0.15 * (1 - 1/3) = 0.10
	// 3 attempts → 0
	want0 := 0.40 + 0.25 + 0.20 + 0.15
	want1 := 0.40 + 0.25 + 0.20 + 0.15*(1.0-1.0/3.0)
	want3 := 0.40 + 0.25 + 0.20 + 0
	for _, tc := range []struct {
		attempts int
		want     float64
	}{
		{0, want0},
		{1, want1},
		{3, want3},
		{99, want3}, // saturates at 3
	} {
		got := ComputeOutcomeScore(TaskCompleted, "none", 1.0, tc.attempts)
		if !approxEqual(got, tc.want) {
			t.Errorf("attempts=%d: got=%v want %v", tc.attempts, got, tc.want)
		}
	}
}

func TestComputeOutcomeScore_QualityClampedTo01(t *testing.T) {
	// Pathological inputs should still produce a value in [0, 1].
	low := ComputeOutcomeScore(TaskCompleted, "none", -5.0, 0)
	high := ComputeOutcomeScore(TaskCompleted, "none", 5.0, 0)
	if low < 0 || low > 1 {
		t.Errorf("negative quality leaked: got %v", low)
	}
	if high < 0 || high > 1 {
		t.Errorf("over-1 quality leaked: got %v", high)
	}
	// Pinned to clamped expected values.
	wantLow := 0.40 + 0.25 + 0.0 + 0.15   // quality clamped to 0
	wantHigh := 0.40 + 0.25 + 0.20 + 0.15 // quality clamped to 1
	if !approxEqual(low, wantLow) {
		t.Errorf("quality=-5 score=%v want %v", low, wantLow)
	}
	if !approxEqual(high, wantHigh) {
		t.Errorf("quality=5 score=%v want %v", high, wantHigh)
	}
}

func TestComputeOutcomeScore_ResultIsInUnitInterval(t *testing.T) {
	// Spot-check: every valid combination produces [0, 1].
	statuses := []TaskStatus{TaskCompleted, TaskFailed, TaskPending}
	severities := []string{"blocker", "warning", "nit", "info", "none", ""}
	qualities := []float64{0, 0.25, 0.5, 0.75, 1.0}
	for _, s := range statuses {
		for _, sev := range severities {
			for _, q := range qualities {
				for _, r := range []int{0, 1, 2, 3, 5} {
					got := ComputeOutcomeScore(s, sev, q, r)
					if got < 0 || got > 1 {
						t.Errorf("ComputeOutcomeScore(%s, %q, %v, %d)=%v escaped [0,1]",
							s, sev, q, r, got)
					}
				}
			}
		}
	}
}

func TestSeverityFromFindings_EmptyReturnsNone(t *testing.T) {
	if got := SeverityFromFindings(nil); got != "none" {
		t.Errorf("empty=%q want none", got)
	}
}

func TestSeverityFromFindings_PicksMaxSeverity(t *testing.T) {
	findings := []UltrareviewFinding{
		{Severity: "nit"},
		{Severity: "warning"},
		{Severity: "info"},
	}
	if got := SeverityFromFindings(findings); got != "warning" {
		t.Errorf("got %q want warning", got)
	}
}

func TestSeverityFromFindings_BlockerWinsOverEverything(t *testing.T) {
	findings := []UltrareviewFinding{
		{Severity: "nit"},
		{Severity: "blocker"},
		{Severity: "warning"},
	}
	if got := SeverityFromFindings(findings); got != "blocker" {
		t.Errorf("got %q want blocker", got)
	}
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
