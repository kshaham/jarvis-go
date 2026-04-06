package cleanup

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestManager_RegisterAndShutdown(t *testing.T) {
	m := New(context.Background())
	var called atomic.Bool

	m.Register("test", func() {
		called.Store(true)
	})

	if m.Count() != 1 {
		t.Errorf("expected 1 cleanup, got %d", m.Count())
	}

	m.Shutdown()

	if !called.Load() {
		t.Error("cleanup not called")
	}
}

func TestManager_ReverseOrder(t *testing.T) {
	m := New(context.Background())
	order := make([]string, 0)

	m.Register("first", func() { order = append(order, "first") })
	m.Register("second", func() { order = append(order, "second") })
	m.Register("third", func() { order = append(order, "third") })

	m.Shutdown()

	if len(order) != 3 || order[0] != "third" || order[1] != "second" || order[2] != "first" {
		t.Errorf("expected reverse order, got %v", order)
	}
}

func TestManager_PanicRecovery(t *testing.T) {
	m := New(context.Background())

	m.Register("panicker", func() {
		panic("test panic")
	})
	m.Register("after", func() {})

	// Should not panic
	m.Shutdown()
}

func TestManager_ContextCancelled(t *testing.T) {
	m := New(context.Background())
	ctx := m.Context()

	select {
	case <-ctx.Done():
		t.Error("context should not be done yet")
	default:
	}

	m.Shutdown()

	select {
	case <-ctx.Done():
		// good
	default:
		t.Error("context should be done after shutdown")
	}
}
