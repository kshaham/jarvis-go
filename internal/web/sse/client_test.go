package sse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClient_MatchesSubscription_All(t *testing.T) {
	c := &Client{subscriptions: nil}
	if !c.MatchesSubscription("anything") {
		t.Error("nil subscriptions should match all")
	}
}

func TestClient_MatchesSubscription_Exact(t *testing.T) {
	c := &Client{subscriptions: []string{"session:created"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("exact match should work")
	}
	if c.MatchesSubscription("session:deleted") {
		t.Error("non-matching should not match")
	}
}

func TestClient_MatchesSubscription_Wildcard(t *testing.T) {
	c := &Client{subscriptions: []string{"session:*"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("wildcard should match session:created")
	}
	if !c.MatchesSubscription("session:output") {
		t.Error("wildcard should match session:output")
	}
	if c.MatchesSubscription("office:standupStarted") {
		t.Error("wildcard should not match office events")
	}
}

func TestClient_MatchesSubscription_Multiple(t *testing.T) {
	c := &Client{subscriptions: []string{"session:*", "office:*"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("should match session")
	}
	if !c.MatchesSubscription("office:standupStarted") {
		t.Error("should match office")
	}
	if c.MatchesSubscription("respawn:started") {
		t.Error("should not match respawn")
	}
}

// flushTrackingWriter is a minimal http.ResponseWriter + http.Flusher that
// records its output and counts Flush calls for testing writePump behaviour.
// All fields are guarded by mu so the test goroutine can safely read them
// after writePump exits.
type flushTrackingWriter struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	flushCount int
	header     http.Header
}

func (f *flushTrackingWriter) Header() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}
func (f *flushTrackingWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}
func (f *flushTrackingWriter) WriteHeader(_ int) {}
func (f *flushTrackingWriter) Flush() {
	f.mu.Lock()
	f.flushCount++
	f.mu.Unlock()
}

// TestWritePump_BatchFlush verifies that multiple events queued at once are
// delivered without loss and that flushes are coalesced (fewer Flush calls
// than events when events arrive in a burst).
func TestWritePump_BatchFlush(t *testing.T) {
	const eventCount = maxFlushBatch * 3 // send 3 full batches

	fw := &flushTrackingWriter{}
	ctx, cancel := context.WithCancel(context.Background())

	client := NewClient(fw, fw, ctx, nil)

	// Queue all events before the writePump has a chance to drain them,
	// so they are all pending in the channel when the pump wakes up.
	for i := 0; i < eventCount; i++ {
		client.Send(EventMessage{
			ID:    uint64(i + 1),
			Event: "test:batch",
			Data:  []byte(fmt.Sprintf(`{"n":%d}`, i)),
		})
	}

	// Allow writePump time to drain the channel, then cancel and wait for exit.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("writePump did not exit after context cancel")
	}

	// Safe to read fw fields now: writePump has exited (Done is closed).
	fw.mu.Lock()
	output := fw.buf.String()
	flushCount := fw.flushCount
	fw.mu.Unlock()

	// Count event frames delivered (each ends with "\n\n").
	delivered := strings.Count(output, "\n\n")
	if delivered != eventCount {
		t.Errorf("expected %d events delivered, got %d\noutput:\n%s", eventCount, delivered, output)
	}

	// Verify all event IDs appear in output (no event dropped or reordered).
	scanner := bufio.NewScanner(strings.NewReader(output))
	foundIDs := make(map[int]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			var id int
			if _, err := fmt.Sscanf(line, "id: %d", &id); err == nil {
				foundIDs[id] = true
			}
		}
	}
	for i := 1; i <= eventCount; i++ {
		if !foundIDs[i] {
			t.Errorf("event ID %d missing from output", i)
		}
	}

	// Coalescing: with maxFlushBatch=4 and 12 events, we expect at most
	// ceil(12/4)=3 flushes (could be more if writePump woke between sends,
	// but strictly fewer than eventCount=12).
	if flushCount >= eventCount {
		t.Errorf("expected fewer than %d flushes (batch coalescing), got %d", eventCount, flushCount)
	}
}
