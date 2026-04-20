package respawn

import (
	"regexp"
	"sync"
	"time"
)

// CompletionTimePattern matches Claude's "Worked for Xm Ys" completion messages.
// Requires "Worked for" prefix to avoid false positives from bare time durations.
var CompletionTimePattern = regexp.MustCompile(`(?i)\bWorked\s+for\s+\d+[hms](\s*\d+[hms])*`)

// OutputSilenceDetector monitors terminal output and fires after a configurable
// silence threshold. Each call to RecordOutput resets the timer.
type OutputSilenceDetector struct {
	mu           sync.Mutex
	lastOutputAt time.Time
	threshold    time.Duration
	timer        *time.Timer
	onSilence    func(elapsed time.Duration)
	stopped      bool
}

// NewOutputSilenceDetector creates a detector that calls onSilence when no
// output has been received for the given threshold duration.
func NewOutputSilenceDetector(threshold time.Duration, onSilence func(elapsed time.Duration)) *OutputSilenceDetector {
	return &OutputSilenceDetector{
		threshold: threshold,
		onSilence: onSilence,
	}
}

// RecordOutput records that terminal output was received, resetting the silence timer.
func (d *OutputSilenceDetector) RecordOutput() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	d.lastOutputAt = time.Now()

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.threshold, func() {
		d.mu.Lock()
		elapsed := time.Since(d.lastOutputAt)
		stopped := d.stopped
		d.mu.Unlock()

		if !stopped && elapsed >= d.threshold {
			d.onSilence(elapsed)
		}
	})
}

// LastOutputAt returns when output was last received.
func (d *OutputSilenceDetector) LastOutputAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastOutputAt
}

// Stop cancels the silence timer.
func (d *OutputSilenceDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// Reset clears the detector state and restarts monitoring.
func (d *OutputSilenceDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = false
	d.lastOutputAt = time.Time{}
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// TokenStabilityDetector compares token counts periodically and fires when
// the count has been stable (unchanged) for a configurable number of checks.
type TokenStabilityDetector struct {
	mu             sync.Mutex
	lastTokenCount int
	stableChecks   int
	threshold      int // number of consecutive stable checks to trigger
	interval       time.Duration
	ticker         *time.Ticker
	onStable       func()
	stopped        bool
	done           chan struct{}
	stopOnce       sync.Once
}

// NewTokenStabilityDetector creates a detector that calls onStable when the
// token count has been unchanged for threshold consecutive checks at the given interval.
func NewTokenStabilityDetector(interval time.Duration, threshold int, onStable func()) *TokenStabilityDetector {
	return &TokenStabilityDetector{
		threshold:      threshold,
		interval:       interval,
		onStable:       onStable,
		lastTokenCount: -1,
		done:           make(chan struct{}),
	}
}

// UpdateTokenCount records the current token count. If different from the
// previous value, the stability counter resets.
func (d *TokenStabilityDetector) UpdateTokenCount(count int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if count != d.lastTokenCount {
		d.lastTokenCount = count
		d.stableChecks = 0
	}
}

// Start begins periodic stability checks.
func (d *TokenStabilityDetector) Start() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.ticker = time.NewTicker(d.interval)
	d.mu.Unlock()

	go func() {
		for {
			select {
			case <-d.done:
				return
			case <-d.ticker.C:
				d.mu.Lock()
				if d.stopped {
					d.mu.Unlock()
					return
				}
				d.stableChecks++
				isStable := d.stableChecks >= d.threshold
				d.mu.Unlock()

				if isStable {
					d.onStable()
				}
			}
		}
	}()
}

// Stop halts the periodic checks.
//
// Previously used a non-blocking send on d.done, which silently
// dropped when the goroutine was mid-tick-handler. The goroutine
// would then loop back to select with a stopped ticker and no
// pending done signal, leaking forever. Closing the channel
// (guarded by sync.Once for idempotent double-Stop) reliably
// wakes the receiver on the next select iteration.
func (d *TokenStabilityDetector) Stop() {
	d.mu.Lock()
	d.stopped = true
	if d.ticker != nil {
		d.ticker.Stop()
	}
	d.mu.Unlock()
	d.stopOnce.Do(func() {
		close(d.done)
	})
}

// Reset clears stability state.
func (d *TokenStabilityDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stableChecks = 0
	d.lastTokenCount = -1
}

// CompletionMessageDetector scans terminal output for the "Worked for Xm Ys"
// pattern and fires when detected.
type CompletionMessageDetector struct {
	onDetected func(text string)
}

// NewCompletionMessageDetector creates a detector that calls onDetected when
// a completion message is found in terminal output.
func NewCompletionMessageDetector(onDetected func(text string)) *CompletionMessageDetector {
	return &CompletionMessageDetector{
		onDetected: onDetected,
	}
}

// Scan checks a chunk of terminal output for completion message patterns.
func (d *CompletionMessageDetector) Scan(data string) {
	match := CompletionTimePattern.FindString(data)
	if match != "" {
		d.onDetected(match)
	}
}
