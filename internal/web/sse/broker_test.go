package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	b := NewBroker(100, 10)
	if b.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", b.ClientCount())
	}
}

func TestBroker_Broadcast(t *testing.T) {
	b := NewBroker(100, 10)
	// Broadcast with no clients should not panic
	b.Broadcast("test:event", map[string]string{"key": "value"})
}

func TestParseSubscriptions(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"session:*", 1},
		{"session:*,office:*", 2},
		{"  session:*  ,  office:*  ", 2},
	}
	for _, tc := range tests {
		result := parseSubscriptions(tc.input)
		if tc.expected == 0 && result != nil {
			t.Errorf("parseSubscriptions(%q) = %v, want nil", tc.input, result)
		}
		if tc.expected > 0 && len(result) != tc.expected {
			t.Errorf("parseSubscriptions(%q) len=%d, want %d", tc.input, len(result), tc.expected)
		}
	}
}

func TestIsCriticalEvent(t *testing.T) {
	if !IsCriticalEvent("session:created") {
		t.Error("session:created should be critical")
	}
	if IsCriticalEvent("session:output") {
		t.Error("session:output should not be critical")
	}
}

func TestBroker_Subscribe_MaxClients(t *testing.T) {
	b := NewBroker(1, 10)

	// Create a fake SSE client
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events", nil)

	// Subscribe in background (it blocks)
	done := make(chan struct{})
	go func() {
		b.Subscribe(recorder, req)
		close(done)
	}()

	// Give it a moment to register
	time.Sleep(50 * time.Millisecond)

	// Second client should be rejected
	recorder2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/events", nil)

	// This will return immediately with 503
	b.Subscribe(recorder2, req2)
	if recorder2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", recorder2.Code)
	}
}

// TestReplay_DeliversMissedEvents verifies that a client reconnecting with
// Last-Event-ID receives the events it missed.
func TestReplay_DeliversMissedEvents(t *testing.T) {
	b := NewBroker(10, 5)
	// Broadcast 5 events while no client is connected.
	for i := 0; i < 5; i++ {
		b.Broadcast("test:event", map[string]int{"n": i})
	}

	// Collect events > ID 2.
	b.mu.RLock()
	replay := b.collectReplayEvents(2)
	b.mu.RUnlock()

	if len(replay) != 3 {
		t.Errorf("expected 3 replayed events (IDs 3,4,5), got %d", len(replay))
	}
	for i, msg := range replay {
		expectedID := uint64(3 + i)
		if msg.ID != expectedID {
			t.Errorf("replay[%d] expected ID %d, got %d", i, expectedID, msg.ID)
		}
	}
}

// TestReplay_IgnoresOlderIDs verifies that requesting ID 0 replays all events.
func TestReplay_IgnoresOlderIDs(t *testing.T) {
	b := NewBroker(10, 5)
	for i := 0; i < 3; i++ {
		b.Broadcast("test:event", map[string]int{"n": i})
	}

	b.mu.RLock()
	replay := b.collectReplayEvents(0)
	b.mu.RUnlock()

	if len(replay) != 3 {
		t.Errorf("expected 3 events when lastSeenID=0, got %d", len(replay))
	}
}

// TestReplay_WrapsAroundBuffer verifies the circular buffer wraps correctly
// when more than replayBufferSize events are broadcast.
func TestReplay_WrapsAroundBuffer(t *testing.T) {
	b := NewBroker(10, 5)
	// Broadcast 250 events (more than the 200 buffer size).
	for i := 0; i < 250; i++ {
		b.Broadcast("test:event", map[string]int{"n": i})
	}

	b.mu.RLock()
	// Request everything after ID 240 -- should get 10 events (241-250).
	replay := b.collectReplayEvents(240)
	b.mu.RUnlock()

	if len(replay) != 10 {
		t.Errorf("expected 10 events after ID 240, got %d", len(replay))
	}
	if replay[0].ID != 241 || replay[9].ID != 250 {
		t.Errorf("expected IDs 241..250, got %d..%d", replay[0].ID, replay[9].ID)
	}
}

// TestSubscriptionIndex_DirectMatch confirms exact subscription entries route
// events to the right clients via the index.
func TestSubscriptionIndex_DirectMatch(t *testing.T) {
	b := NewBroker(10, 10)
	// Two clients with exact subscription, one with wildcard, one with no match.
	c1 := &Client{subscriptions: []string{"session:created"}}
	c2 := &Client{subscriptions: []string{"session:created"}}
	c3 := &Client{subscriptions: nil} // wildcard
	c4 := &Client{subscriptions: []string{"office:standup"}}
	b.indexClient(c1)
	b.indexClient(c2)
	b.indexClient(c3)
	b.indexClient(c4)

	pp := b.matchingClients("session:created")
	defer putClientSlice(pp)
	if len(*pp) != 3 {
		t.Errorf("expected 3 matches (2 exact + wildcard), got %d", len(*pp))
	}
	// c4 must not appear.
	for _, c := range *pp {
		if c == c4 {
			t.Errorf("c4 subscribed only to office:standup; should not match session:created")
		}
	}
}

// TestSubscriptionIndex_PrefixMatch confirms wildcard subscriptions
// ("session:*") route correctly.
func TestSubscriptionIndex_PrefixMatch(t *testing.T) {
	b := NewBroker(10, 10)
	prefixClient := &Client{subscriptions: []string{"session:*"}}
	otherClient := &Client{subscriptions: []string{"office:*"}}
	b.indexClient(prefixClient)
	b.indexClient(otherClient)

	pp := b.matchingClients("session:terminal")
	defer putClientSlice(pp)
	if len(*pp) != 1 || (*pp)[0] != prefixClient {
		t.Errorf("expected only prefix client to match, got %v", *pp)
	}
}

// TestSubscriptionIndex_UnindexRemovesClient verifies disconnect cleanup.
func TestSubscriptionIndex_UnindexRemovesClient(t *testing.T) {
	b := NewBroker(10, 10)
	c := &Client{subscriptions: []string{"session:*", "office:standup"}}
	b.indexClient(c)
	b.unindexClient(c)

	pp1 := b.matchingClients("session:created")
	n1 := len(*pp1)
	putClientSlice(pp1)
	if n1 != 0 {
		t.Errorf("unindexed client should not appear in matches, got %d", n1)
	}
	pp2 := b.matchingClients("office:standup")
	n2 := len(*pp2)
	putClientSlice(pp2)
	if n2 != 0 {
		t.Errorf("unindexed client should not appear in matches, got %d", n2)
	}
}

// TestPostBroadcastHook_FiresOncePerBroadcast confirms a registered hook
// runs exactly once per broadcast with the same event it was called with.
func TestPostBroadcastHook_FiresOncePerBroadcast(t *testing.T) {
	b := NewBroker(10, 5)
	var received []string
	b.AddPostBroadcastHook(func(msg EventMessage) {
		received = append(received, msg.Event)
	})

	b.Broadcast("test:one", map[string]int{"n": 1})
	b.Broadcast("test:two", map[string]int{"n": 2})

	if len(received) != 2 {
		t.Fatalf("expected hook to fire 2 times, got %d", len(received))
	}
	if received[0] != "test:one" || received[1] != "test:two" {
		t.Errorf("unexpected event order: %v", received)
	}
}

// TestSubscriptionIndex_NoDoubleDelivery verifies that a client subscribed to
// both a prefix and an exact event (which could match the same broadcast) is
// delivered the event exactly once.
func TestSubscriptionIndex_NoDoubleDelivery(t *testing.T) {
	b := NewBroker(10, 10)
	// This client has both a prefix and an exact subscription that both match
	// "session:created". The dedup logic must prevent double-delivery.
	c := &Client{subscriptions: []string{"session:*", "session:created"}}
	b.indexClient(c)

	pp := b.matchingClients("session:created")
	defer putClientSlice(pp)
	if len(*pp) != 1 {
		t.Errorf("expected exactly 1 delivery (no double-deliver), got %d", len(*pp))
	}
}

// TestBroker_ConcurrentBroadcastSubscribe exercises concurrent Broadcast calls
// interleaved with Subscribe/unsubscribe to catch data races under -race.
// Clients disconnect via context cancellation (not CloseAllClients) so we
// avoid the known pre-existing race between Send recovery and close(ch).
func TestBroker_ConcurrentBroadcastSubscribe(t *testing.T) {
	b := NewBroker(50, 10)

	const broadcasts = 200
	const subscribers = 5

	var wg sync.WaitGroup
	var received atomic.Int64

	// Goroutines that each subscribe via a cancellable context, then disconnect.
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
			// Subscribe blocks until context expires; no data races with Broadcast.
			b.Subscribe(recorder, req)
		}()
	}

	// Concurrent broadcaster.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < broadcasts; i++ {
			b.Broadcast("test:race", map[string]int{"i": i})
			received.Add(1)
		}
	}()

	wg.Wait()
	if n := received.Load(); n != broadcasts {
		t.Errorf("expected %d broadcasts, counted %d", broadcasts, n)
	}
}
