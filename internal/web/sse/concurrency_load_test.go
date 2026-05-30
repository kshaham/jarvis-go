package sse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// syncFlushWriter is a minimal, goroutine-safe http.ResponseWriter +
// http.Flusher used purely as the SSE sink in these concurrency tests.
//
// It exists so the TEST HARNESS itself contributes no data race: the SSE
// Client's writePump goroutine writes to w concurrently with the test
// goroutine that reads headers/status, and httptest.ResponseRecorder is
// NOT safe for that (its Body buffer + Code field are touched without
// synchronization). All state here is guarded by mu, so any DATA RACE the
// -race detector reports under these tests comes from the production sse
// package, not from this writer.
type syncFlushWriter struct {
	mu      sync.Mutex
	header  http.Header
	written int64 // bytes written, read atomically
	code    int
}

func newSyncFlushWriter() *syncFlushWriter {
	return &syncFlushWriter{header: make(http.Header), code: http.StatusOK}
}

func (s *syncFlushWriter) Header() http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header
}

func (s *syncFlushWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	atomic.AddInt64(&s.written, int64(len(p)))
	return len(p), nil
}

func (s *syncFlushWriter) WriteHeader(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = code
}

// Flush satisfies http.Flusher so Subscribe accepts this writer and the
// Client's writePump exercises its real flush path.
func (s *syncFlushWriter) Flush() {}

func (s *syncFlushWriter) bytesWritten() int64 {
	return atomic.LoadInt64(&s.written)
}

// TestConcurrencyLoad_BrokerUnderConcurrentBroadcastSubscribeClose hammers the
// broker with many goroutines broadcasting (which mutate the replay ring buffer
// and the subscription index under b.mu, then fan out non-blocking c.Send calls
// outside the lock) WHILE other goroutines connect via Subscribe (mutating the
// clients map / index / replay reads) and CloseAllClients runs concurrently
// (snapshotting clients and closing each channel).
//
// This is the regression guard for the fixed data-race classes:
//   - clients map / subscription index concurrent read+write
//   - replay buffer concurrent write (Broadcast) + read (Subscribe replay)
//   - send-on-closed-channel between Send and Close/CloseAllClients
//   - getInitData read (Subscribe) vs SetInitDataFunc write
//
// Run: go test -race -run TestConcurrencyLoad_BrokerUnderConcurrentBroadcastSubscribeClose -count=1 ./internal/web/sse/
func TestConcurrencyLoad_BrokerUnderConcurrentBroadcastSubscribeClose(t *testing.T) {
	const (
		numBroadcasters = 24
		numSubscribers  = 24
		numClosers      = 6
		iterations      = 300
	)

	b := NewBroker(10000, 10000)

	// Set an init-data func so the SetInitDataFunc <-> Subscribe(getInitData)
	// path is exercised under load too.
	b.SetInitDataFunc(func() interface{} {
		return map[string]interface{}{"type": "init", "n": 1}
	})

	// A post-broadcast hook touches only its argument (per the contract), so it
	// must stay race-free as Broadcast copies the hook slice under the lock and
	// invokes it outside the lock.
	var hookFires atomic.Int64
	b.AddPostBroadcastHook(func(msg EventMessage) {
		hookFires.Add(1)
	})

	// start gates every worker so they all hit the broker in the same window,
	// maximizing the chance of interleaving the racy operations.
	start := make(chan struct{})
	var wg sync.WaitGroup

	// Mix of event families so the subscription index (exact + prefix +
	// wildcard) and the replay buffer all see churn.
	events := []string{
		"session:created", // critical -> can disconnect a full-buffer client
		"session:output",  // non-critical
		"office:standup",
		"respawn:started", // critical
		"misc",            // bucketed as "other"
	}

	// Broadcasters: each cycles Broadcast and BroadcastPreSerialized.
	for i := 0; i < numBroadcasters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				ev := events[(id+j)%len(events)]
				if j%2 == 0 {
					b.Broadcast(ev, map[string]int{"id": id, "j": j})
				} else {
					b.BroadcastPreSerialized(EventMessage{
						Event: ev,
						Data:  []byte(fmt.Sprintf(`{"id":%d,"j":%d}`, id, j)),
					})
				}
			}
		}(i)
	}

	// Subscribers: connect via the real Subscribe path. Each Subscribe blocks on
	// client.Done() until the client's channel is closed -- which happens either
	// because CloseAllClients closes it, or because a critical event arrives on a
	// full buffer. To guarantee every Subscribe returns (so the WaitGroup
	// completes and the test can't hang), each subscriber cancels its own request
	// context after kicking off the handler; ctx.Done() also unblocks writePump.
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < iterations/10; j++ {
				w := newSyncFlushWriter()
				ctx, cancel := context.WithCancel(context.Background())

				// Vary subscriptions: wildcard (nil), prefix, exact, and a
				// Last-Event-ID reconnect that forces a replay-buffer read while
				// broadcasters write it.
				var rawurl string
				switch j % 4 {
				case 0:
					rawurl = "/api/events"
				case 1:
					rawurl = "/api/events?subscribe=session:*,office:*"
				case 2:
					rawurl = "/api/events?subscribe=session:created"
				default:
					rawurl = "/api/events?subscribe=session:*&lastEventId=1"
				}
				req := httptest.NewRequest("GET", rawurl, nil).WithContext(ctx)

				subDone := make(chan struct{})
				go func() {
					defer close(subDone)
					b.Subscribe(w, req)
				}()

				// Cancel the context to guarantee the handler unblocks even if no
				// closer reaches this particular client. This still races
				// Subscribe's clients-map insert against CloseAllClients's
				// snapshot + the broadcasters' index reads -- exactly the window
				// under test.
				cancel()
				<-subDone
				_ = w.bytesWritten()
			}
		}(i)
	}

	// Closers: repeatedly CloseAllClients, racing the clients-map snapshot
	// against Subscribe inserts/deletes and each c.Close() against in-flight
	// Send calls from broadcasters.
	for i := 0; i < numClosers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				b.CloseAllClients()
				_ = b.ClientCount() // concurrent read of the clients map
			}
		}()
	}

	close(start)
	wg.Wait()

	// After all workers drain, every Subscribe goroutine has returned, so the
	// broker should have no lingering clients. This both confirms CloseAllClients
	// + ctx-cancel reliably unblock Subscribe and gives the race detector a final
	// clean read of the clients map.
	if got := b.ClientCount(); got != 0 {
		t.Errorf("expected 0 clients after all subscribers returned, got %d", got)
	}
	if hookFires.Load() == 0 {
		t.Error("expected post-broadcast hook to fire at least once under load")
	}
}

// TestConcurrencyLoad_CloseAllClientsUnblocksSubscribe verifies the specific
// shutdown invariant in isolation: a Subscribe handler that is blocked on
// client.Done() must return promptly when CloseAllClients is called, with NO
// send-on-closed-channel panic even though broadcasters are still firing into
// the client the instant it is closed.
//
// The request context here is context.Background() (never canceled), so the
// ONLY thing that can unblock Subscribe is CloseAllClients closing the client
// channel -> writePump returns -> close(done). If CloseAllClients failed to
// unblock, wg.Wait() would hang and the test would time out (a real failure).
func TestConcurrencyLoad_CloseAllClientsUnblocksSubscribe(t *testing.T) {
	const (
		numClients      = 32
		numBroadcasters = 16
		iterations      = 300
	)

	b := NewBroker(10000, 10000)

	var subWG sync.WaitGroup
	connected := make(chan struct{}, numClients)

	for i := 0; i < numClients; i++ {
		subWG.Add(1)
		go func(id int) {
			defer subWG.Done()
			w := newSyncFlushWriter()
			// Background context => only CloseAllClients can unblock this.
			req := httptest.NewRequest("GET", "/api/events", nil)
			// Signal "about to enter Subscribe"; combined with the spin-wait
			// below this gives broadcasters and the closer live clients to race
			// against without any time.Sleep ordering.
			connected <- struct{}{}
			b.Subscribe(w, req)
		}(i)
	}

	// Wait until all subscriber goroutines have at least entered their launch
	// path (deterministic: we read exactly numClients tokens).
	for i := 0; i < numClients; i++ {
		<-connected
	}

	// Spin (no sleep) until the broker actually reports the clients registered,
	// so CloseAllClients has real work and the close races live Sends.
	for b.ClientCount() < numClients {
		runtime.Gosched()
	}

	// Broadcasters keep firing into the (about-to-be-closed) clients to force the
	// Send-vs-Close race on the channel.
	var bcWG sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < numBroadcasters; i++ {
		bcWG.Add(1)
		go func(id int) {
			defer bcWG.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				// Mix critical + non-critical so the full-buffer critical-close
				// path in Send is also exercised against CloseAllClients.
				if j%2 == 0 {
					b.Broadcast("session:created", map[string]int{"j": j})
				} else {
					b.Broadcast("session:output", map[string]int{"j": j})
				}
			}
		}(i)
	}

	// Close everything concurrently with the broadcast storm.
	b.CloseAllClients()

	// All Subscribe handlers must return; if CloseAllClients did not unblock
	// them, this hangs and the test fails by timeout.
	subWG.Wait()

	close(stop)
	bcWG.Wait()

	if got := b.ClientCount(); got != 0 {
		t.Errorf("expected 0 clients after CloseAllClients + subscriber return, got %d", got)
	}
}
