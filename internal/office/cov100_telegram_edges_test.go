package office

import (
	"sync"
	"testing"
	"time"
)

// panicNotifier panics on SendOfficeUpdate so the periodic-update tick's
// panic-recovery wrapper fires. IsConnected returns true so the goroutine
// starts.
type panicNotifier struct {
	mu      sync.Mutex
	panics  int
	stopped bool
}

func (p *panicNotifier) SendOfficeUpdate(string) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.panics++
	p.mu.Unlock()
	panic("boom from SendOfficeUpdate")
}
func (p *panicNotifier) IsConnected() bool          { return true }
func (p *panicNotifier) NotifyStandupComplete(string) {}
func (p *panicNotifier) panicCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.panics
}
func (p *panicNotifier) stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
}

// TestStartPeriodicUpdates_TickPanicRecovered drives the per-tick
// panic-recovery wrapper: SendOfficeUpdate panics, but the goroutine survives
// (recover logs + continues) and the next tick panics again.
func TestStartPeriodicUpdates_TickPanicRecovered(t *testing.T) {
	m := newTestManager()
	m.SetTeamSpawned(true)
	n := &panicNotifier{}
	m.SetTelegramNotifier(n)
	m.mu.Lock()
	m.updateInterval = 15 * time.Millisecond
	m.mu.Unlock()

	m.startPeriodicUpdates()

	// Wait for at least 2 ticks: the second only happens if the first panic
	// was recovered (the goroutine didn't die).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.panicCount() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	got := n.panicCount()
	n.stop() // prevent further panics during teardown
	m.StopTelegramUpdates()

	if got < 2 {
		t.Errorf("expected the update goroutine to survive a panic and tick again, panics=%d", got)
	}
}

// TestStopTelegramUpdates_WaitTimeout drives the 5s wait-timeout branch in
// StopTelegramUpdates: a phantom outstanding updateWg entry that never
// completes forces the select onto its time.After arm.
func TestStopTelegramUpdates_WaitTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("5s wait-timeout path skipped in -short")
	}
	m := newTestManager()
	// Add a phantom waitgroup count with no matching Done so updateWg.Wait()
	// never returns and StopTelegramUpdates must fall back to its 5s timeout.
	m.updateWg.Add(1)

	start := time.Now()
	m.StopTelegramUpdates()
	elapsed := time.Since(start)

	if elapsed < 4*time.Second {
		t.Errorf("expected StopTelegramUpdates to block ~5s on the wait timeout, took %v", elapsed)
	}
	// Release the phantom count so the package's other tests aren't affected
	// (the WaitGroup is per-Manager, but be tidy).
	m.updateWg.Done()
}
