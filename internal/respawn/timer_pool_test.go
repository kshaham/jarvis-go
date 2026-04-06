package respawn

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestTimerPool_Start(t *testing.T) {
	tp := NewTimerPool()
	var fired atomic.Bool

	tp.Start("test", 50*time.Millisecond, "testing", func() {
		fired.Store(true)
	})

	if tp.Count() != 1 {
		t.Errorf("expected 1 timer, got %d", tp.Count())
	}

	time.Sleep(100 * time.Millisecond)

	if !fired.Load() {
		t.Error("timer should have fired")
	}
	if tp.Count() != 0 {
		t.Errorf("expected 0 timers after fire, got %d", tp.Count())
	}
}

func TestTimerPool_Cancel(t *testing.T) {
	tp := NewTimerPool()
	var fired atomic.Bool

	tp.Start("test", 100*time.Millisecond, "testing", func() {
		fired.Store(true)
	})

	tp.Cancel("test")

	time.Sleep(150 * time.Millisecond)

	if fired.Load() {
		t.Error("cancelled timer should not fire")
	}
	if tp.Count() != 0 {
		t.Errorf("expected 0 timers after cancel, got %d", tp.Count())
	}
}

func TestTimerPool_CancelAll(t *testing.T) {
	tp := NewTimerPool()

	tp.Start("t1", 1*time.Second, "a", func() {})
	tp.Start("t2", 1*time.Second, "b", func() {})
	tp.Start("t3", 1*time.Second, "c", func() {})

	if tp.Count() != 3 {
		t.Errorf("expected 3 timers, got %d", tp.Count())
	}

	tp.CancelAll()

	if tp.Count() != 0 {
		t.Errorf("expected 0 timers after cancel all, got %d", tp.Count())
	}
}

func TestTimerPool_Replace(t *testing.T) {
	tp := NewTimerPool()
	var count atomic.Int32

	tp.Start("test", 50*time.Millisecond, "first", func() {
		count.Add(1)
	})

	// Replace before first fires
	tp.Start("test", 50*time.Millisecond, "second", func() {
		count.Add(1)
	})

	if tp.Count() != 1 {
		t.Errorf("expected 1 timer (replaced), got %d", tp.Count())
	}

	time.Sleep(100 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("expected 1 fire (replaced first), got %d", count.Load())
	}
}

func TestTimerPool_ActiveTimers(t *testing.T) {
	tp := NewTimerPool()
	tp.Start("t1", 1*time.Second, "reason1", func() {})
	tp.Start("t2", 2*time.Second, "reason2", func() {})

	active := tp.ActiveTimers()
	if len(active) != 2 {
		t.Errorf("expected 2 active timers, got %d", len(active))
	}

	tp.CancelAll()
}

func TestTimerPool_CancelNonexistent(t *testing.T) {
	tp := NewTimerPool()
	// Should not panic
	tp.Cancel("nonexistent")
}
