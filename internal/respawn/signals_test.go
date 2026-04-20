package respawn

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestOutputSilenceDetector_FiresAfterThreshold(t *testing.T) {
	var fired atomic.Bool
	detector := NewOutputSilenceDetector(50*time.Millisecond, func(elapsed time.Duration) {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.RecordOutput()

	time.Sleep(100 * time.Millisecond)

	if !fired.Load() {
		t.Error("detector should have fired after threshold")
	}
}

func TestOutputSilenceDetector_ResetsOnOutput(t *testing.T) {
	var fireCount atomic.Int32
	detector := NewOutputSilenceDetector(80*time.Millisecond, func(elapsed time.Duration) {
		fireCount.Add(1)
	})
	defer detector.Stop()

	// Record output, then record again before threshold
	detector.RecordOutput()
	time.Sleep(40 * time.Millisecond)
	detector.RecordOutput() // resets the timer

	time.Sleep(50 * time.Millisecond) // 90ms from start, 50ms from reset - not enough

	if fireCount.Load() != 0 {
		t.Error("should not have fired yet (timer was reset)")
	}

	time.Sleep(50 * time.Millisecond) // Now past threshold from reset

	if fireCount.Load() != 1 {
		t.Errorf("expected 1 fire, got %d", fireCount.Load())
	}
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

	time.Sleep(200 * time.Millisecond) // 50ms * 3 = 150ms minimum

	if !fired.Load() {
		t.Error("should fire after 3 stable checks")
	}
}

func TestTokenStabilityDetector_ResetsOnChange(t *testing.T) {
	var fired atomic.Bool
	detector := NewTokenStabilityDetector(50*time.Millisecond, 3, func() {
		fired.Store(true)
	})
	defer detector.Stop()

	detector.UpdateTokenCount(1000)
	detector.Start()

	time.Sleep(80 * time.Millisecond) // 1-2 stable checks

	// Change token count -- resets stability counter
	detector.UpdateTokenCount(2000)

	time.Sleep(80 * time.Millisecond) // 1-2 more checks, not enough for 3

	if fired.Load() {
		t.Error("should not fire yet -- token count changed mid-stream")
	}

	time.Sleep(100 * time.Millisecond) // now total 3+ checks at new value

	if !fired.Load() {
		t.Error("should fire after stability restored")
	}
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
