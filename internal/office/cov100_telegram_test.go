package office

import (
	"sync"
	"testing"
	"time"
)

// syncNotifier is a thread-safe TelegramNotifier for ticker tests.
type syncNotifier struct {
	mu        sync.Mutex
	updates   []string
	connected bool
}

func (s *syncNotifier) SendOfficeUpdate(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, text)
}
func (s *syncNotifier) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}
func (s *syncNotifier) NotifyStandupComplete(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, text)
}
func (s *syncNotifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updates)
}

// TestStartPeriodicUpdates_NilNotifier covers the early return when no
// notifier (or a disconnected one) is set.
func TestStartPeriodicUpdates_NilNotifier(t *testing.T) {
	m := newTestManager()
	// No notifier set → early return, no goroutine launched.
	m.startPeriodicUpdates()
	// Disconnected notifier → also early return.
	m.SetTelegramNotifier(&syncNotifier{connected: false})
	m.startPeriodicUpdates()
}

// TestStartPeriodicUpdates_TicksAndStops drives one real ticker tick (so
// the goroutine body, panic-recovery wrapper, buildSprintSummary,
// SendOfficeUpdate, and lastTelegramUpdate write all run), then stops.
func TestStartPeriodicUpdates_TicksAndStops(t *testing.T) {
	m := newTestManager()
	m.SetTeamSpawned(true)
	n := &syncNotifier{connected: true}
	m.SetTelegramNotifier(n)
	// Force a very short interval so a tick fires fast. updateInterval is
	// set directly to bypass the 10-minute floor in SetTelegramConfig.
	m.mu.Lock()
	m.updateInterval = 20 * time.Millisecond
	m.mu.Unlock()

	m.startPeriodicUpdates()
	// Wait for at least one tick.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n.count() == 0 {
		t.Error("expected at least one telegram update to be sent")
	}
	m.StopTelegramUpdates()

	_, _, last := m.GetTelegramConfig()
	if last.IsZero() {
		t.Error("expected lastTelegramUpdate to be set after a tick")
	}
}

// TestSetTelegramConfig_EnableStartsWhenSpawned covers the
// enable && !wasEnabled && teamSpawned branch (start path).
func TestSetTelegramConfig_EnableStartsWhenSpawned(t *testing.T) {
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.SetTelegramNotifier(&syncNotifier{connected: true})
	m.SetTelegramConfig(true, 15)
	enabled, mins, _ := m.GetTelegramConfig()
	if !enabled || mins != 15 {
		t.Errorf("expected enabled=true interval=15, got %v %d", enabled, mins)
	}
	m.StopTelegramUpdates()
}

// TestSetTelegramConfig_IntervalFloor covers the <10-minute floor clamp.
func TestSetTelegramConfig_IntervalFloor(t *testing.T) {
	m := newTestManager()
	m.SetTelegramConfig(true, 1) // below the 10-minute floor
	_, mins, _ := m.GetTelegramConfig()
	if mins != 10 {
		t.Errorf("expected interval clamped to 10, got %d", mins)
	}
}

// TestSetTelegramConfig_RestartOnIntervalChange covers the
// enable && wasEnabled (restart) branch and the disable branch.
func TestSetTelegramConfig_RestartAndDisable(t *testing.T) {
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.SetTelegramNotifier(&syncNotifier{connected: true})
	m.SetTelegramConfig(true, 10) // start
	m.SetTelegramConfig(true, 20) // restart (interval changed)
	m.SetTelegramConfig(false, 0) // disable → stop
	enabled, _, _ := m.GetTelegramConfig()
	if enabled {
		t.Error("expected telegram disabled after final config")
	}
}
