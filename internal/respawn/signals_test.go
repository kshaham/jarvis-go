package respawn

import (
	"sync/atomic"
	"testing"
	"time"
)

// waitForCondition polls fn every ~5ms until it returns true or the
// deadline elapses. Replaces the brittle `time.Sleep(N); assert ...`
// pattern that flakes under CI load. Failure message includes the
// elapsed wait so flake diagnostics get a real signal instead of
// "assertion failed at line N".
func waitForCondition(t *testing.T, fn func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForCondition: %s (timeout %v)", msg, timeout)
}

// waitForStable asserts a condition that should REMAIN false for the
// full settle window. Used to verify "should not fire yet" without
// the original 50ms-margin time.Sleep games. Returns early with a
// failure if fn becomes true at any sample point.
func waitForStable(t *testing.T, fn func() bool, settle time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(settle)
	for time.Now().Before(deadline) {
		if fn() {
			t.Fatalf("waitForStable: %s (condition became true during %v settle)", msg, settle)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOutputSilenceDetector_FiresAfterThreshold(t *testing.T) {
	var fired atomic.Bool
	detector := NewOutputSilenceDetector(50*time.Millisecond, func(elapsed time.Duration) {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.RecordOutput()

	waitForCondition(t, fired.Load, 500*time.Millisecond,
		"detector should fire after the 50ms silence threshold")
}

func TestOutputSilenceDetector_ResetsOnOutput(t *testing.T) {
	var fireCount atomic.Int32
	detector := NewOutputSilenceDetector(80*time.Millisecond, func(elapsed time.Duration) {
		fireCount.Add(1)
	})
	defer detector.Stop()

	// Record output, then record again well before the 80ms threshold
	// to reset the timer.
	detector.RecordOutput()
	time.Sleep(40 * time.Millisecond)
	detector.RecordOutput() // resets the 80ms timer

	// Settle window: 50ms after the reset, the new 80ms timer can't
	// possibly have fired yet. Use waitForStable instead of a bare
	// sleep — that way an aggressive scheduler that fires early gets
	// caught as a real bug, not silently absorbed by the sleep.
	waitForStable(t, func() bool { return fireCount.Load() > 0 }, 50*time.Millisecond,
		"timer fired before its 80ms-from-reset threshold")

	// After the reset's threshold elapses, expect exactly one fire.
	// Generous deadline (500ms) so CI scheduler hiccups don't flake.
	waitForCondition(t, func() bool { return fireCount.Load() == 1 }, 500*time.Millisecond,
		"detector should fire exactly once after the post-reset threshold")
}

func TestOutputSilenceDetector_Stop(t *testing.T) {
	var fired atomic.Bool
	detector := NewOutputSilenceDetector(50*time.Millisecond, func(elapsed time.Duration) {
		fired.Store(true)
	})

	detector.RecordOutput()
	detector.Stop()

	time.Sleep(100 * time.Millisecond)

	if fired.Load() {
		t.Error("stopped detector should not fire")
	}
}

func TestOutputSilenceDetector_Reset(t *testing.T) {
	var fired atomic.Bool
	detector := NewOutputSilenceDetector(50*time.Millisecond, func(elapsed time.Duration) {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.RecordOutput()
	detector.Reset()

	time.Sleep(100 * time.Millisecond)

	if fired.Load() {
		t.Error("reset detector should not fire until new output recorded")
	}

	// After reset, recording should restart monitoring
	detector.RecordOutput()
	time.Sleep(100 * time.Millisecond)

	if !fired.Load() {
		t.Error("detector should fire after new output post-reset")
	}
}

func TestTokenStabilityDetector_FiresAfterStableChecks(t *testing.T) {
	var fired atomic.Bool
	detector := NewTokenStabilityDetector(50*time.Millisecond, 3, func() {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.UpdateTokenCount(1000)
	detector.Start()

	// Need ~3 ticker callbacks (50ms each = 150ms minimum). Poll until
	// the callback fires; deadline 800ms swallows worst-case CI jitter.
	waitForCondition(t, fired.Load, 800*time.Millisecond,
		"should fire after 3 stable checks")
}

func TestTokenStabilityDetector_ResetsOnChange(t *testing.T) {
	var fired atomic.Bool
	detector := NewTokenStabilityDetector(50*time.Millisecond, 3, func() {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.UpdateTokenCount(1000)
	detector.Start()

	// Give the detector enough wall-clock for 1-2 stable checks at the
	// pre-change value (but not 3 — otherwise it'd fire). 100ms is
	// long enough for ~2 ticks but short enough not to hit the third.
	time.Sleep(100 * time.Millisecond)

	// Change resets the stability counter.
	detector.UpdateTokenCount(2000)

	// 50ms settle: still well below the 3*50=150ms post-change quorum,
	// so a fire here would be a real bug (premature fire). The poll
	// loop in waitForStable detects an early fire instantly instead
	// of waiting out the bare sleep.
	waitForStable(t, fired.Load, 50*time.Millisecond,
		"detector fired before its post-change stability quorum elapsed")

	// Now wait for stability to be re-established at the new value.
	// Deadline 800ms; same jitter-swallow rationale as above.
	waitForCondition(t, fired.Load, 800*time.Millisecond,
		"should fire once stability restored at the new token count")
}

func TestTokenStabilityDetector_Stop(t *testing.T) {
	var fired atomic.Bool
	detector := NewTokenStabilityDetector(50*time.Millisecond, 2, func() {
		fired.Store(true)
	})

	detector.UpdateTokenCount(1000)
	detector.Start()
	detector.Stop()

	time.Sleep(200 * time.Millisecond)

	if fired.Load() {
		t.Error("stopped detector should not fire")
	}
}

func TestCompletionMessageDetector_MatchesPattern(t *testing.T) {
	tests := []struct {
		input   string
		matches bool
	}{
		{"Worked for 2m 46s", true},
		{"Worked for 46s", true},
		{"Worked for 1h 2m 3s", true},
		{"worked for 5m", true},         // case insensitive
		{"wait for 5s", false},          // not "Worked for"
		{"run for 2m", false},           // not "Worked for"
		{"for 3s the system...", false}, // not "Worked for"
		{"no match here", false},
		{"", false},
	}

	for _, tc := range tests {
		var detected string
		detector := NewCompletionMessageDetector(func(text string) {
			detected = text
		})

		detector.Scan(tc.input)

		if tc.matches && detected == "" {
			t.Errorf("expected match for %q, got none", tc.input)
		}
		if !tc.matches && detected != "" {
			t.Errorf("expected no match for %q, got %q", tc.input, detected)
		}
	}
}

func TestCompletionTimePattern_Regex(t *testing.T) {
	positives := []string{
		"Worked for 2m 46s",
		"Worked for 1h",
		"Worked for 46s",
		"Worked for 1h 2m 3s",
		"worked for 5m 30s",
	}
	for _, s := range positives {
		if !CompletionTimePattern.MatchString(s) {
			t.Errorf("expected match for %q", s)
		}
	}

	negatives := []string{
		"wait for 5s",
		"for 2m running",
		"some random text",
	}
	for _, s := range negatives {
		if CompletionTimePattern.MatchString(s) {
			t.Errorf("expected no match for %q", s)
		}
	}
}
