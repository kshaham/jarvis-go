package respawn

import (
	"context"
	"testing"
	"time"
)

func TestController_NewController(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	if rc.State() != StateStopped {
		t.Errorf("expected StateStopped, got %s", rc.State())
	}
	if rc.CycleCount() != 0 {
		t.Errorf("expected 0 cycles, got %d", rc.CycleCount())
	}
}

func TestController_Start(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	if rc.State() != StateWatching {
		t.Errorf("expected StateWatching, got %s", rc.State())
	}
}

func TestController_DoubleStart(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	ctx := context.Background()

	_ = rc.Start(ctx)
	defer rc.Stop()

	err := rc.Start(ctx)
	if err == nil {
		t.Error("double start should return error")
	}
}

func TestController_Stop(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	ctx := context.Background()
	_ = rc.Start(ctx)
	rc.Stop()

	if rc.State() != StateStopped {
		t.Errorf("expected StateStopped after stop, got %s", rc.State())
	}
}

func TestController_InvalidTransition(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	// From Stopped, can only go to Watching
	err := rc.setState(StateSendingUpdate)
	if err == nil {
		t.Error("expected error for invalid transition")
	}
}

func TestController_StopHook(t *testing.T) {
	cfg := DefaultRespawnConfig()
	cfg.ConfidenceThreshold = 50
	rc := NewController("test-session", cfg)
	ctx := context.Background()
	_ = rc.Start(ctx)
	defer rc.Stop()

	rc.OnStopHook()

	// Should have transitioned to ConfirmingIdle (confidence = 100 > 50)
	time.Sleep(50 * time.Millisecond) // Allow goroutine to process
	if rc.State() != StateConfirmingIdle {
		t.Errorf("expected StateConfirmingIdle after stop hook, got %s", rc.State())
	}
}

func TestController_WorkingDetectedReset(t *testing.T) {
	cfg := DefaultRespawnConfig()
	cfg.ConfidenceThreshold = 50
	rc := NewController("test-session", cfg)
	ctx := context.Background()
	_ = rc.Start(ctx)
	defer rc.Stop()

	// Add signals to build confidence
	rc.OnStopHook()
	time.Sleep(50 * time.Millisecond)

	// Now simulate working detected
	rc.OnWorkingDetected("thinking spinner")

	// Should be back to watching and confidence reset
	if rc.State() != StateWatching {
		t.Errorf("expected StateWatching after working detected, got %s", rc.State())
	}
	if rc.Confidence() != 0 {
		t.Errorf("expected 0 confidence after reset, got %d", rc.Confidence())
	}
}

func TestController_WeightedConfidence(t *testing.T) {
	cfg := DefaultRespawnConfig()
	cfg.ConfidenceThreshold = 80 // High threshold
	rc := NewController("test-session", cfg)
	ctx := context.Background()
	_ = rc.Start(ctx)
	defer rc.Stop()

	// Add completion (40) + silence (25) = 65 < 80
	rc.OnCompletionMessage("worked for 5m")
	rc.OnOutputSilence(10 * time.Second)

	time.Sleep(50 * time.Millisecond)
	if rc.State() != StateWatching {
		t.Errorf("65 confidence should not trigger at threshold 80, state = %s", rc.State())
	}

	// Add token stability (20) -> 65+20 = 85 > 80
	rc.OnTokenStability()
	time.Sleep(50 * time.Millisecond)
	if rc.State() != StateConfirmingIdle {
		t.Errorf("85 confidence should trigger at threshold 80, state = %s", rc.State())
	}
}

func TestController_Events(t *testing.T) {
	rc := NewController("test-session", DefaultRespawnConfig())
	ctx := context.Background()
	_ = rc.Start(ctx)

	// Should receive state change event
	select {
	case event := <-rc.Events():
		if _, ok := event.(StateChangedEvent); !ok {
			t.Errorf("expected StateChangedEvent, got %T", event)
		}
	case <-time.After(1 * time.Second):
		t.Error("no event received within 1s")
	}

	rc.Stop()
}
