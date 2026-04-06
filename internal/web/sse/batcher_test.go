package sse

import (
	"sync"
	"testing"
	"time"
)

func TestBatcher_Add(t *testing.T) {
	var mu sync.Mutex
	flushed := make(map[string][]byte)

	b := NewBatcher(50*time.Millisecond, 0, func(sessionID string, data []byte) {
		mu.Lock()
		flushed[sessionID] = append(flushed[sessionID], data...)
		mu.Unlock()
	})

	b.Add("s1", []byte("hello"))
	b.Add("s1", []byte(" world"))
	b.Add("s2", []byte("other"))

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if string(flushed["s1"]) != "hello world" {
		t.Errorf("s1 flushed = %q, want 'hello world'", flushed["s1"])
	}
	if string(flushed["s2"]) != "other" {
		t.Errorf("s2 flushed = %q, want 'other'", flushed["s2"])
	}
}

func TestBatcher_FlushThreshold(t *testing.T) {
	var mu sync.Mutex
	flushed := make(map[string][]byte)

	b := NewBatcher(1*time.Second, 10, func(sessionID string, data []byte) {
		mu.Lock()
		flushed[sessionID] = append(flushed[sessionID], data...)
		mu.Unlock()
	})

	// Add data exceeding threshold (10 bytes)
	b.Add("s1", []byte("12345678901")) // 11 bytes > 10

	// Should flush immediately, not wait for 1s interval
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushed["s1"]) == 0 {
		t.Error("expected immediate flush when threshold exceeded")
	}
}

func TestBatcher_Stop(t *testing.T) {
	var mu sync.Mutex
	flushed := make(map[string][]byte)

	b := NewBatcher(1*time.Second, 0, func(sessionID string, data []byte) {
		mu.Lock()
		flushed[sessionID] = append(flushed[sessionID], data...)
		mu.Unlock()
	})

	b.Add("s1", []byte("pending"))
	b.Stop()

	mu.Lock()
	defer mu.Unlock()

	if string(flushed["s1"]) != "pending" {
		t.Errorf("expected 'pending' flushed on stop, got %q", flushed["s1"])
	}
}
