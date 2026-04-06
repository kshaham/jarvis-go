package sse

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
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

	go c.writePump()

	return c
}

// Send queues an event for delivery. Non-blocking.
func (c *Client) Send(msg EventMessage) {
	select {
	case c.ch <- msg:
	default:
		// Buffer full
		if IsCriticalEvent(msg.Event) {
			c.closeOnce.Do(func() { close(c.ch) })
		}
		// Non-critical: silently drop
	}
}

// Done returns a channel that closes when the client disconnects.
func (c *Client) Done() <-chan struct{} {
	return c.done
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
			if _, err := fmt.Fprintf(c.w, "event: %s\ndata: %s\n\n", msg.Event, msg.Data); err != nil {
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
