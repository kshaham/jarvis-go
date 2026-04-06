package sse

import (
	"bytes"
	"sync"
	"time"
)

// Batcher accumulates data per session and flushes at regular intervals.
type Batcher struct {
	mu             sync.Mutex
	batches        map[string]*bytes.Buffer
	interval       time.Duration
	flushThreshold int
	flushFn        func(sessionID string, data []byte)
	timer          *time.Timer
	stopped        bool
}

// NewBatcher creates a batcher that flushes at the given interval.
func NewBatcher(interval time.Duration, flushThreshold int, flushFn func(sessionID string, data []byte)) *Batcher {
	return &Batcher{
		batches:        make(map[string]*bytes.Buffer),
		interval:       interval,
		flushThreshold: flushThreshold,
		flushFn:        flushFn,
	}
}

// Add accumulates data for a session. May trigger an immediate flush if threshold exceeded.
func (b *Batcher) Add(sessionID string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.stopped {
		return
	}

	buf, ok := b.batches[sessionID]
	if !ok {
		buf = &bytes.Buffer{}
		b.batches[sessionID] = buf
	}
	buf.Write(data)

	// Flush immediately if threshold exceeded
	if b.flushThreshold > 0 && buf.Len() >= b.flushThreshold {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		buf.Reset()
		go b.flushFn(sessionID, data)
	}

	// Start timer if not already running
	if b.timer == nil {
		b.timer = time.AfterFunc(b.interval, b.flushAll)
	}
}

// Stop stops the batcher and flushes remaining data.
func (b *Batcher) Stop() {
	b.mu.Lock()
	b.stopped = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()

	b.flushAll()
}

func (b *Batcher) flushAll() {
	b.mu.Lock()
	if len(b.batches) == 0 {
		b.timer = nil
		b.mu.Unlock()
		return
	}

	// Snapshot and clear
	toFlush := make(map[string][]byte, len(b.batches))
	for sessionID, buf := range b.batches {
		if buf.Len() > 0 {
			data := make([]byte, buf.Len())
			copy(data, buf.Bytes())
			toFlush[sessionID] = data
			buf.Reset()
		}
	}
	b.timer = nil
	b.mu.Unlock()

	for sessionID, data := range toFlush {
		b.flushFn(sessionID, data)
	}
}
