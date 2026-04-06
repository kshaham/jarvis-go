package cleanup

import (
	"context"
	"log/slog"
	"sync"
)

// CleanupFunc is called during shutdown.
type CleanupFunc func()

// Manager tracks resources that need cleanup on shutdown.
type Manager struct {
	mu           sync.Mutex
	cleanups     []namedCleanup
	ctx          context.Context
	cancel       context.CancelFunc
	shutdownOnce sync.Once
}

type namedCleanup struct {
	name string
	fn   CleanupFunc
}

// New creates a new cleanup manager with a derived context.
func New(parent context.Context) *Manager {
	ctx, cancel := context.WithCancel(parent)
	return &Manager{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Register adds a cleanup function.
func (m *Manager) Register(name string, fn CleanupFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanups = append(m.cleanups, namedCleanup{name: name, fn: fn})
}

// Context returns the manager's context (cancelled on Shutdown).
func (m *Manager) Context() context.Context {
	return m.ctx
}

// Shutdown cancels the context and runs all cleanup functions in reverse order.
// Safe to call multiple times; only the first call executes cleanups.
func (m *Manager) Shutdown() {
	m.shutdownOnce.Do(func() {
		m.cancel()

		m.mu.Lock()
		cleanups := make([]namedCleanup, len(m.cleanups))
		copy(cleanups, m.cleanups)
		m.mu.Unlock()

		for i := len(cleanups) - 1; i >= 0; i-- {
			c := cleanups[i]
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("cleanup panic", "name", c.name, "error", r)
					}
				}()
				c.fn()
			}()
		}
	})
}

// Count returns the number of registered cleanups.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cleanups)
}
