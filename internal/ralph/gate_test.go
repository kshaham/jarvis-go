package ralph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubGate is a test double returning a fixed GateResult.
type stubGate struct {
	result GateResult
	calls  int
}

func (g *stubGate) Check(workingDir string) GateResult {
	g.calls++
	return g.result
}

func TestDetectVerifyCommand(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"go.mod", "go test ./... -count=1"},
		{"Cargo.toml", "cargo test"},
		{"package.json", "npm test --silent"},
		{"pyproject.toml", "pytest -q"},
		{"Makefile", "make test"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			got := joinArgv(DetectVerifyCommand(dir))
			if got != tc.want {
				t.Errorf("detect(%s) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

func TestDetectVerifyCommand_NoneMatches(t *testing.T) {
	if cmd := DetectVerifyCommand(t.TempDir()); cmd != nil {
		t.Errorf("expected nil for empty dir, got %v", cmd)
	}
}

func TestBasicCommandGate_Pass(t *testing.T) {
	g := NewBasicCommandGate("true")
	res := g.Check(t.TempDir())
	if !res.Pass || res.ExitCode != 0 {
		t.Fatalf("expected pass exit 0, got pass=%v code=%d err=%q", res.Pass, res.ExitCode, res.Err)
	}
	if res.Skipped {
		t.Error("explicit command should not be skipped")
	}
}

func TestBasicCommandGate_Fail(t *testing.T) {
	g := NewBasicCommandGate("false")
	res := g.Check(t.TempDir())
	if res.Pass {
		t.Fatal("expected fail")
	}
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit code, got 0")
	}
	if res.Skipped {
		t.Error("a real non-zero exit is a failure, not a skip")
	}
}

func TestBasicCommandGate_MissingBinarySkips(t *testing.T) {
	g := NewBasicCommandGate("this-binary-does-not-exist-jarvis")
	res := g.Check(t.TempDir())
	if res.Pass {
		t.Fatal("missing binary must not pass")
	}
	if !res.Skipped {
		t.Errorf("missing binary should be Skipped (not a hard fail), got %+v", res)
	}
}

func TestBasicCommandGate_NoCommandSkips(t *testing.T) {
	g := NewBasicCommandGate("") // empty + empty dir => no auto-detect
	res := g.Check(t.TempDir())
	if !res.Skipped {
		t.Errorf("expected skip when no command available, got %+v", res)
	}
}

func TestBasicCommandGate_Timeout(t *testing.T) {
	g := &BasicCommandGate{Command: "sleep 5", Timeout: 100 * time.Millisecond}
	res := g.Check(t.TempDir())
	if res.Pass {
		t.Fatal("timed-out command must not pass")
	}
	if !res.TimedOut {
		t.Errorf("expected TimedOut, got %+v", res)
	}
}

func joinArgv(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// tripBreaker drives the tracker to the point where the dual-condition circuit
// breaker has tripped and a completion phrase fires, invoking the gate path.
func tripBreaker(t *testing.T, tr *Tracker) {
	t.Helper()
	tr.Configure("LOOP_DONE_TOKEN", 0)
	tr.ProcessOutput("all tasks are done\n")
	tr.ProcessOutput("everything is done\n")
	tr.ProcessOutput("---RALPH_STATUS---\nEXIT_SIGNAL: true\n---END_RALPH_STATUS---\n")
	tr.ProcessOutput("LOOP_DONE_TOKEN\n")
}

// collectEvents drains the tracker event channel for d and returns all events.
func collectEvents(tr *Tracker, d time.Duration) []TrackerEvent {
	var events []TrackerEvent
	deadline := time.After(d)
	for {
		select {
		case ev := <-tr.Events():
			events = append(events, ev)
		case <-deadline:
			return events
		}
	}
}

func hasEvent[T TrackerEvent](events []TrackerEvent) bool {
	return countEvent[T](events) > 0
}

func countEvent[T TrackerEvent](events []TrackerEvent) int {
	n := 0
	for _, ev := range events {
		if _, ok := ev.(T); ok {
			n++
		}
	}
	return n
}

func TestTracker_GatePass_EmitsExitGate(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.SetGate(&stubGate{result: GateResult{Pass: true, ExitCode: 0}}, t.TempDir())
	tripBreaker(t, tr)

	events := collectEvents(tr, 500*time.Millisecond)
	if !hasEvent[ExitGateMet](events) {
		t.Error("expected ExitGateMet when gate passes")
	}
	if hasEvent[GateCheckFailed](events) {
		t.Error("did not expect GateCheckFailed when gate passes")
	}
}

func TestTracker_GateFail_BlocksExitAndResetsBreaker(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.SetGate(&stubGate{result: GateResult{Pass: false, ExitCode: 1, Command: "go test ./..."}}, t.TempDir())
	tripBreaker(t, tr)

	events := collectEvents(tr, 500*time.Millisecond)
	if hasEvent[ExitGateMet](events) {
		t.Error("ExitGateMet must NOT be emitted when the objective gate fails (Ralph Wiggum fix)")
	}
	if !hasEvent[GateCheckFailed](events) {
		t.Fatal("expected GateCheckFailed when gate fails")
	}
	// Breaker must be reset so the loop can continue and re-trip later.
	if tr.GetCircuitBreakerStatus().Tripped {
		t.Error("circuit breaker should be reset after a gate failure")
	}
	if tr.GetState().GateFailureCount != 1 {
		t.Errorf("expected GateFailureCount=1, got %d", tr.GetState().GateFailureCount)
	}
}

func TestTracker_SetGate_ResetsFailureCount(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.SetGate(&stubGate{result: GateResult{Pass: false, ExitCode: 1}}, t.TempDir())
	tripBreaker(t, tr)

	// Wait for the failing gate to register a failure.
	events := collectEvents(tr, 500*time.Millisecond)
	if !hasEvent[GateCheckFailed](events) {
		t.Fatal("expected a GateCheckFailed from the failing gate")
	}
	if tr.GetState().GateFailureCount != 1 {
		t.Fatalf("expected GateFailureCount=1, got %d", tr.GetState().GateFailureCount)
	}

	// Swapping in a new gate must clear the consecutive-failure budget so the
	// fresh gate doesn't inherit the old count (regression guard).
	tr.SetGate(&stubGate{result: GateResult{Pass: true}}, t.TempDir())
	if got := tr.GetState().GateFailureCount; got != 0 {
		t.Errorf("expected GateFailureCount reset to 0 after SetGate, got %d", got)
	}
}

func TestTracker_NoGate_PreservesLegacyExit(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tripBreaker(t, tr) // no gate configured

	events := collectEvents(tr, 300*time.Millisecond)
	if !hasEvent[ExitGateMet](events) {
		t.Error("with no gate, dual-condition trip should emit ExitGateMet (legacy behavior)")
	}
}

func TestTracker_MaxIterations_EmitsLimitReachedExactlyOnce(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.Configure("CYCLE_TOKEN", 2)
	tr.ProcessOutput("CYCLE_TOKEN\n") // iteration 1
	tr.ProcessOutput("CYCLE_TOKEN\n") // iteration 2 -> cap reached (fires)
	tr.ProcessOutput("CYCLE_TOKEN\n") // iteration 3 -> past cap, must NOT re-fire

	events := collectEvents(tr, 300*time.Millisecond)
	// The hard-stop signal is one-shot; a completion past the cap must not
	// re-broadcast it (regression guard for the >= vs == bug).
	if got := countEvent[LoopLimitReached](events); got != 1 {
		t.Errorf("expected exactly one LoopLimitReached, got %d", got)
	}
}
