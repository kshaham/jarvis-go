package sse

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

const (
	clientBufferSize  = 256
	healthCheckPeriod = 30 * time.Second
)

// Client represents a connected SSE client with its own write goroutine.
type Client struct {
	w       http.ResponseWriter
	flusher http.Flusher
	ch      chan EventMessage
	done    chan struct{}
	ctx     context.Context
	// sendMu serializes the non-blocking channel send in Send against the
	// close(c.ch) in Close. The closed flag + recover used to mask the
	// send-on-closed PANIC, but `c.ch <- msg` racing `close(c.ch)` is still a
	// DATA RACE the detector flags; holding sendMu across both makes them
	// mutually exclusive. The select stays non-blocking, so the hold is trivial.
	sendMu        sync.Mutex
	closeOnce     sync.Once
	closed        atomic.Bool
	subscriptions []string // nil = all events
}

// NewClient creates a new SSE client and starts its write goroutine.
func NewClient(w http.ResponseWriter, f http.Flusher, ctx context.Context, subs []string) *Client {
	c := &Client{
		w:             w,
		flusher:       f,
		ch:            make(chan EventMessage, clientBufferSize),
		done:          make(chan struct{}),
		ctx:           ctx,
		subscriptions: subs,
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// safego: one writePump per SSE client; if a panic in the write
	// loop tears down the daemon, every other client + the office
	// itself goes with it.
	safego.Go("sse.writePump", c.writePump)

	return c
}

// Send queues an event for delivery. Non-blocking.
// Uses recover to handle the TOCTOU race between the closed check and the channel send.
func (c *Client) Send(msg EventMessage) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.closed.Load() {
		return
	}
	// Under sendMu the channel can't be closed concurrently, so the send can't
	// panic; the select stays non-blocking for backpressure.
	select {
	case c.ch <- msg:
	default:
		if IsCriticalEvent(msg.Event) {
			c.closeOnce.Do(func() {
				c.closed.Store(true)
				close(c.ch)
			})
		}
	}
}

// Done returns a channel that closes when the client disconnects.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close unblocks this client's writePump (and therefore the Subscribe handler
// waiting on Done()) by closing its event channel. Used at server shutdown so
// http.Server.Shutdown doesn't wait on never-returning SSE handlers. Idempotent
// and safe against a concurrent Send (both go through closeOnce).
func (c *Client) Close() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.ch)
	})
}

// MatchesSubscription checks if this client wants the given event.
func (c *Client) MatchesSubscription(event string) bool {
	if c.subscriptions == nil {
		return true // nil = all events
	}
	for _, sub := range c.subscriptions {
		if strings.HasSuffix(sub, "*") {
			prefix := strings.TrimSuffix(sub, "*")
			if strings.HasPrefix(event, prefix) {
				return true
			}
		} else if sub == event {
			return true
		}
	}
	return false
}

// maxFlushBatch is the maximum number of queued events written before a
// single Flush() call in writePump. Keeping it small (4) bounds latency
// while coalescing flush syscalls when events arrive in bursts.
const maxFlushBatch = 4

func (c *Client) writePump() {
	defer close(c.done)

	ticker := time.NewTicker(healthCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.ch:
			if !ok {
				return
			}
			// Write the first event from the blocking select, then drain any
			// additional queued events (non-blocking) up to maxFlushBatch before
			// issuing a single Flush. This coalesces flush syscalls on burst
			// traffic while never dropping events and preserving FIFO order.
			// Fix 2c: batch flush to reduce per-event Flush() syscall overhead.
			if err := c.writeEvent(msg); err != nil {
				return
			}
			for i := 1; i < maxFlushBatch; i++ {
				select {
				case next, ok2 := <-c.ch:
					if !ok2 {
						// Channel closed mid-batch: flush what we have and exit.
						c.flusher.Flush()
						return
					}
					if err := c.writeEvent(next); err != nil {
						return
					}
				default:
					// No more events queued right now — stop draining.
					i = maxFlushBatch // break inner loop
				}
			}
			c.flusher.Flush()

		case <-ticker.C:
			if _, err := fmt.Fprint(c.w, ": keepalive\n\n"); err != nil {
				return
			}
			c.flusher.Flush()

		case <-c.ctx.Done():
			return
		}
	}
}

// writeEvent formats a single SSE event frame. Does NOT flush.
func (c *Client) writeEvent(msg EventMessage) error {
	// Include id: line so browsers track Last-Event-ID automatically on
	// reconnect. Events broadcast before ID assignment (init) have ID=0
	// and omit the id line.
	var err error
	if msg.ID > 0 {
		_, err = fmt.Fprintf(c.w, "id: %d\nevent: %s\ndata: %s\n\n", msg.ID, msg.Event, msg.Data)
	} else {
		_, err = fmt.Fprintf(c.w, "event: %s\ndata: %s\n\n", msg.Event, msg.Data)
	}
	return err
}
