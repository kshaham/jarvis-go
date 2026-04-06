package respawn

import "testing"

func TestRespawnState_String(t *testing.T) {
	tests := []struct {
		state    RespawnState
		expected string
	}{
		{StateStopped, "STOPPED"},
		{StateWatching, "WATCHING"},
		{StateConfirmingIdle, "CONFIRMING_IDLE"},
		{StateAiChecking, "AI_CHECKING"},
		{StateSendingUpdate, "SENDING_UPDATE"},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.expected {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.expected)
		}
	}
}

func TestValidTransitions(t *testing.T) {
	// Every state should have at least one valid transition
	allStates := []RespawnState{
		StateStopped, StateWatching, StateConfirmingIdle, StateAiChecking,
		StateSendingUpdate, StateWaitingUpdate, StateSendingClear, StateWaitingClear,
		StateSendingInit, StateWaitingInit, StateMonitoringInit,
		StateSendingKickstart, StateWaitingKickstart,
	}
	for _, state := range allStates {
		transitions, ok := validTransitions[state]
		if !ok {
			t.Errorf("state %s has no transitions defined", state)
		}
		if len(transitions) == 0 {
			t.Errorf("state %s has empty transition list", state)
		}
	}

	// StateStopped can only go to StateWatching
	if len(validTransitions[StateStopped]) != 1 || validTransitions[StateStopped][0] != StateWatching {
		t.Error("StateStopped should only transition to StateWatching")
	}

	// Every state except StateStopped should be able to go to StateStopped
	for _, state := range allStates {
		if state == StateStopped {
			continue
		}
		found := false
		for _, target := range validTransitions[state] {
			if target == StateStopped {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("state %s should be able to transition to StateStopped", state)
		}
	}
}

func TestDetectionLayer_String(t *testing.T) {
	if LayerStopHook.String() != "stop_hook" {
		t.Errorf("expected 'stop_hook', got %q", LayerStopHook.String())
	}
	if LayerAiCheck.String() != "ai_check" {
		t.Errorf("expected 'ai_check', got %q", LayerAiCheck.String())
	}
}

func TestDefaultRespawnConfig(t *testing.T) {
	cfg := DefaultRespawnConfig()
	if cfg.Enabled {
		t.Error("default should be disabled")
	}
	if cfg.ConfidenceThreshold != 70 {
		t.Errorf("expected threshold 70, got %d", cfg.ConfidenceThreshold)
	}
	total := cfg.ConfidenceWeightCompletion + cfg.ConfidenceWeightSilence +
		cfg.ConfidenceWeightTokens + cfg.ConfidenceWeightNoWorking
	if total != 100 {
		t.Errorf("weights should sum to 100, got %d", total)
	}
}
