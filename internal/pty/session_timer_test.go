package pty

import (
	"context"
	"testing"
	"time"
)

// TestStopAndDrainTimer_StopsAndAllowsReset exercises stopAndDrainTimer on a
// timer that has fired. Under Go 1.23+ timer semantics (this module is go 1.25)
// the runtime auto-drains Timer.C, so Stop() returns true and the manual
// `<-timer.C` drain arm is never taken -- see the 'unreachable' note. The helper
// must still leave the timer in a state where a Reset behaves: no stale tick is
// observable afterward.
func TestStopAndDrainTimer_FiredTimer(t *testing.T) {
	timer := time.NewTimer(1 * time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let it fire

	stopAndDrainTimer(timer)

	// No stale tick must be observable (the new runtime auto-drains; the helper
	// is a no-op-safe wrapper around that).
	select {
	case <-timer.C:
		t.Error("a stale tick was observable after stopAndDrainTimer")
	default:
	}

	// Reset must work cleanly and deliver exactly one fresh tick.
	timer.Reset(1 * time.Millisecond)
	select {
	case <-timer.C:
		// fresh tick delivered
	case <-time.After(time.Second):
		t.Fatal("Reset after stopAndDrainTimer did not deliver a tick")
	}
}

// TestStopAndDrainTimer_NotYetFired covers the Stop()==true path on an unfired
// timer: it is stopped cleanly and no drain is attempted, and no tick is
// delivered afterward.
func TestStopAndDrainTimer_NotYetFired(t *testing.T) {
	timer := time.NewTimer(time.Hour) // far future -> Stop returns true
	stopAndDrainTimer(timer)
	select {
	case <-timer.C:
		t.Error("unfired timer should have no tick to receive")
	default:
	}
}

// TestSession_IdleDetector_ResetUnderLoad drives idleDetector through many
// resets while its internal timer is repeatedly armed, exercising the
// reset-then-rearm path (the stopAndDrainTimer call site) and confirming the
// detector stays alive and exits cleanly on cancel. This is the integration
// counterpart to the unit tests above.
func TestSession_IdleDetector_ResetUnderLoad(t *testing.T) {
	origDelay := idleDetectionDelay
	idleDetectionDelay = 1 * time.Millisecond
	defer func() { idleDetectionDelay = origDelay }()

	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.wg.Add(1)
	go func() {
		s.idleDetector(ctx)
		close(done)
	}()

	for i := 0; i < 500; i++ {
		select {
		case s.idleReset <- struct{}{}:
		default:
		}
		if i%50 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("idleDetector did not exit after cancel")
	}
}
