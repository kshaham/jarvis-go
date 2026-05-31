package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestPollLoop_ProcessesUpdateThenStops drives one successful getUpdates cycle:
// the server returns a single /status command update, pollLoop dispatches it
// (which enqueues a response), and a cancelled context exits the loop cleanly.
//
// We observe progress via the mutex-protected message queue rather than reading
// h.offset directly, since h.offset is owned by the poll goroutine and reading
// it from the test goroutine would be a (production-faithful) data race.
func TestPollLoop_ProcessesUpdateThenStops(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":42,"message":{"message_id":1,"chat":{"id":12345},"text":"/status"}}]}`)
			return
		}
		// Subsequent polls return empty so the loop spins until cancellation.
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
	}))
	defer ts.Close()

	h, _ := newTestHandler(t)
	h.bot.Configure("tok", "12345")

	ctx, cancel := context.WithCancel(context.Background())

	withAPIBase(t, ts.URL, func() {
		done := make(chan struct{})
		go func() {
			h.pollLoop(ctx)
			close(done)
		}()

		// Wait until the /status update has been dispatched (response enqueued).
		deadline := time.Now().Add(3 * time.Second)
		dispatched := false
		for time.Now().Before(deadline) {
			if queueLen(h) > 0 {
				dispatched = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("pollLoop did not exit after context cancel")
		}
		if !dispatched {
			t.Error("expected /status response enqueued from polled update")
		}
	})
}

// TestPollLoop_ErrorBacksOffThenStops drives the error path: getUpdates fails
// with a 500, the loop logs and backs off, then the cancelled context exits the
// backoff sleep cleanly. We observe the first poll happened via the server hit
// count (atomic), not via h.consecutiveErrors.
func TestPollLoop_ErrorBacksOffThenStops(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"server error"}`)
	}))
	defer ts.Close()

	h, _ := newTestHandler(t)
	h.bot.Configure("tok", "12345")

	ctx, cancel := context.WithCancel(context.Background())
	withAPIBase(t, ts.URL, func() {
		done := make(chan struct{})
		go func() {
			h.pollLoop(ctx)
			close(done)
		}()

		// Wait for the first failing poll to land.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if atomic.LoadInt32(&hits) >= 1 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel() // exits during the ~1s backoff sleep
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("pollLoop did not exit after cancel during backoff")
		}
		if atomic.LoadInt32(&hits) < 1 {
			t.Error("expected at least one getUpdates attempt")
		}
	})
}
