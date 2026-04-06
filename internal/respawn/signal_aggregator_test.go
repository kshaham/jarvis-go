package respawn

import (
	"testing"
	"time"
)

func TestSignalAggregator_Empty(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	if c := sa.ComputeConfidence(); c != 0 {
		t.Errorf("empty aggregator confidence = %d, want 0", c)
	}
}

func TestSignalAggregator_StopHook(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	sa.AddSignal(DetectionSignal{Layer: LayerStopHook, Confidence: 100, Timestamp: time.Now()})
	if c := sa.ComputeConfidence(); c != 100 {
		t.Errorf("stop hook confidence = %d, want 100", c)
	}
}

func TestSignalAggregator_IdlePrompt(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	sa.AddSignal(DetectionSignal{Layer: LayerIdlePrompt, Confidence: 100, Timestamp: time.Now()})
	if c := sa.ComputeConfidence(); c != 100 {
		t.Errorf("idle prompt confidence = %d, want 100", c)
	}
}

func TestSignalAggregator_WeightedLayers(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	// Completion message: weight 40, confidence 100 -> 40
	sa.AddSignal(DetectionSignal{Layer: LayerCompletionMessage, Confidence: 100, Timestamp: time.Now()})
	// Output silence: weight 25, confidence 100 -> 25
	sa.AddSignal(DetectionSignal{Layer: LayerOutputSilence, Confidence: 100, Timestamp: time.Now()})

	c := sa.ComputeConfidence()
	// 40 + 25 = 65
	if c != 65 {
		t.Errorf("weighted confidence = %d, want 65", c)
	}
}

func TestSignalAggregator_AllLayers(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	sa.AddSignal(DetectionSignal{Layer: LayerCompletionMessage, Confidence: 100, Timestamp: time.Now()})
	sa.AddSignal(DetectionSignal{Layer: LayerOutputSilence, Confidence: 100, Timestamp: time.Now()})
	sa.AddSignal(DetectionSignal{Layer: LayerTokenStability, Confidence: 100, Timestamp: time.Now()})
	sa.AddSignal(DetectionSignal{Layer: LayerWorkingAbsence, Confidence: 100, Timestamp: time.Now()})

	c := sa.ComputeConfidence()
	// 40 + 25 + 20 + 15 = 100
	if c != 100 {
		t.Errorf("all layers confidence = %d, want 100", c)
	}
}

func TestSignalAggregator_Reset(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	sa.AddSignal(DetectionSignal{Layer: LayerStopHook, Confidence: 100, Timestamp: time.Now()})
	sa.Reset()
	if c := sa.ComputeConfidence(); c != 0 {
		t.Errorf("after reset confidence = %d, want 0", c)
	}
}

func TestSignalAggregator_HasSignal(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	if sa.HasSignal(LayerStopHook) {
		t.Error("should not have stop hook signal")
	}
	sa.AddSignal(DetectionSignal{Layer: LayerStopHook, Confidence: 100, Timestamp: time.Now()})
	if !sa.HasSignal(LayerStopHook) {
		t.Error("should have stop hook signal")
	}
}

func TestSignalAggregator_ActiveLayers(t *testing.T) {
	sa := NewSignalAggregator(DefaultRespawnConfig())
	sa.AddSignal(DetectionSignal{Layer: LayerCompletionMessage, Confidence: 100, Timestamp: time.Now()})
	sa.AddSignal(DetectionSignal{Layer: LayerOutputSilence, Confidence: 100, Timestamp: time.Now()})

	layers := sa.ActiveLayers()
	if len(layers) != 2 {
		t.Errorf("expected 2 active layers, got %d", len(layers))
	}
}
