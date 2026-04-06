package collections

import (
	"container/list"
	"sync"
)

// LRUMap is a thread-safe LRU eviction map with a maximum capacity.
type LRUMap[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[K]*list.Element
	order    *list.List
}

type entry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRUMap creates an LRU map with the given capacity.
func NewLRUMap[K comparable, V any](capacity int) *LRUMap[K, V] {
	return &LRUMap[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element),
		order:    list.New(),
	}
}

// Get retrieves a value and marks it as recently used.
func (m *LRUMap[K, V]) Get(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if elem, ok := m.items[key]; ok {
		m.order.MoveToFront(elem)
		return elem.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Set adds or updates a key-value pair. Evicts oldest if at capacity.
func (m *LRUMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if elem, ok := m.items[key]; ok {
		m.order.MoveToFront(elem)
		elem.Value.(*entry[K, V]).value = value
		return
	}

	if m.order.Len() >= m.capacity {
		oldest := m.order.Back()
		if oldest != nil {
			m.order.Remove(oldest)
			delete(m.items, oldest.Value.(*entry[K, V]).key)
		}
	}

	elem := m.order.PushFront(&entry[K, V]{key: key, value: value})
	m.items[key] = elem
}

// Delete removes a key.
func (m *LRUMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if elem, ok := m.items[key]; ok {
		m.order.Remove(elem)
		delete(m.items, key)
	}
}

// Len returns the number of entries.
func (m *LRUMap[K, V]) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.order.Len()
}

// Clear removes all entries.
func (m *LRUMap[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make(map[K]*list.Element)
	m.order.Init()
}
