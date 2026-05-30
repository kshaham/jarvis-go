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
	w             http.ResponseWriter
	flusher       http.Flusher
	ch            chan EventMessage
	done          chan struct{}
	ctx           context.Context
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
	if c.closed.Load() {
		return
	}
	defer func() { recover() }() // catch send on closed channel
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
			// Include id: line so browsers track Last-Event-ID automatically
			// on reconnect. Events broadcast before ID assignment (init)
			// have ID=0 and omit the id line.
			var err error
			if msg.ID > 0 {
				_, err = fmt.Fprintf(c.w, "id: %d\nevent: %s\ndata: %s\n\n", msg.ID, msg.Event, msg.Data)
			} else {
				_, err = fmt.Fprintf(c.w, "event: %s\ndata: %s\n\n", msg.Event, msg.Data)
			}
			if err != nil {
				return
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
