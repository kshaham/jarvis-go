package collections

import (
	"sync"
	"time"
)

type staleEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// StaleExpirationMap is a thread-safe map with per-entry TTL expiration.
type StaleExpirationMap[K comparable, V any] struct {
	mu       sync.Mutex
	items    map[K]*staleEntry[V]
	ttl      time.Duration
	ticker   *time.Ticker
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewStaleExpirationMap creates a new map with the given TTL and cleanup interval.
func NewStaleExpirationMap[K comparable, V any](ttl, cleanupInterval time.Duration) *StaleExpirationMap[K, V] {
	m := &StaleExpirationMap[K, V]{
		items:  make(map[K]*staleEntry[V]),
		ttl:    ttl,
		ticker: time.NewTicker(cleanupInterval),
		stopCh: make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

// Set adds or updates an entry, resetting its TTL.
func (m *StaleExpirationMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = &staleEntry[V]{value: value, expiresAt: time.Now().Add(m.ttl)}
}

// Get retrieves a value if it exists and hasn't expired.
func (m *StaleExpirationMap[K, V]) Get(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.items[key]
	if !ok || time.Now().After(e.expiresAt) {
		if ok {
			delete(m.items, key)
		}
		var zero V
		return zero, false
	}
	return e.value, true
}

// Delete removes an entry.
func (m *StaleExpirationMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
}

// Len returns the number of non-expired entries.
func (m *StaleExpirationMap[K, V]) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	count := 0
	for _, e := range m.items {
		if now.Before(e.expiresAt) {
			count++
		}
	}
	return count
}

// Stop stops the cleanup goroutine. Safe to call multiple times.
func (m *StaleExpirationMap[K, V]) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.ticker.Stop()
	})
}

func (m *StaleExpirationMap[K, V]) cleanupLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.ticker.C:
			m.mu.Lock()
			now := time.Now()
			for k, e := range m.items {
				if now.After(e.expiresAt) {
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
