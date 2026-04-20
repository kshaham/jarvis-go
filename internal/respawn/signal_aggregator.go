package respawn

import (
	"sync"
	"time"
)

// SignalAggregator collects idle detection signals and computes confidence.
type SignalAggregator struct {
	mu      sync.RWMutex
	weights map[DetectionLayer]int
	latest  map[DetectionLayer]DetectionSignal
}

// NewSignalAggregator creates an aggregator with configured weights.
func NewSignalAggregator(cfg RespawnConfig) *SignalAggregator {
	return &SignalAggregator{
		weights: map[DetectionLayer]int{
			LayerCompletionMessage: cfg.ConfidenceWeightCompletion,
			LayerOutputSilence:     cfg.ConfidenceWeightSilence,
			LayerTokenStability:    cfg.ConfidenceWeightTokens,
			LayerWorkingAbsence:    cfg.ConfidenceWeightNoWorking,
		},
		latest: make(map[DetectionLayer]DetectionSignal),
	}
}

// AddSignal records a detection signal from one layer.
func (sa *SignalAggregator) AddSignal(signal DetectionSignal) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.latest[signal.Layer] = signal
}

// Reset clears all accumulated signals (called when working detected).
func (sa *SignalAggregator) Reset() {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.latest = make(map[DetectionLayer]DetectionSignal)
}

// ComputeConfidence returns the current idle confidence (0-100).
func (sa *SignalAggregator) ComputeConfidence() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	// Hook signals are definitive
	if _, ok := sa.latest[LayerStopHook]; ok {
		return 100
	}
	if _, ok := sa.latest[LayerIdlePrompt]; ok {
		return 100
	}

	total := 0
	for layer, signal := range sa.latest {
		if w, ok := sa.weights[layer]; ok {
			total += w * signal.Confidence / 100
		}
	}

	// Extended silence bonus
	if silence, ok := sa.latest[LayerOutputSilence]; ok {
		elapsed := time.Since(silence.Timestamp)
		if elapsed > 30*time.Second {
			bonus := int(elapsed.Seconds()/30) * 5
			if bonus > 20 {
				bonus = 20
			}
			total += bonus
		}
	}

	if total > 100 {
		total = 100
	}
	return total
}

// ActiveLayers returns which detection layers have fired.
func (sa *SignalAggregator) ActiveLayers() []DetectionLayer {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	layers := make([]DetectionLayer, 0, len(sa.latest))
	for l := range sa.latest {
		layers = append(layers, l)
	}
	return layers
}

// HasSignal checks if a specific layer has fired.
func (sa *SignalAggregator) HasSignal(layer DetectionLayer) bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	_, ok := sa.latest[layer]
	return ok
}
