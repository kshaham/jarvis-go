package telegram

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestProcessOne_RetryableRequeues drives a 429 response which is retryable;
// processOne -> maybeRetry should schedule a re-enqueue after a backoff. We use
// a short-circuit: maybeRetry sleeps math.Pow(2,0)=1s before requeueing, so we
// assert the message lands back on the queue within a generous window.
func TestProcessOne_RetryableRequeues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`)
	}))
	defer ts.Close()

	mq := NewMessageQueue("tok", &http.Client{Timeout: 5 * time.Second}, 100)
	withAPIBase(t, ts.URL, func() {
		mq.processOne(queuedMessage{ChatID: "c", Text: "x", MaxRetries: 3})

		// maybeRetry re-enqueues after ~1s (2^(retries-1) with retries=1).
		deadline := time.Now().Add(3 * time.Second)
		var requeued bool
		for time.Now().Before(deadline) {
			mq.mu.Lock()
			n := len(mq.queue)
			mq.mu.Unlock()
			if n > 0 {
				requeued = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !requeued {
			t.Fatal("expected retryable 429 to be re-enqueued")
		}
		mq.mu.Lock()
		retries := mq.queue[0].Retries
		mq.mu.Unlock()
		if retries != 1 {
			t.Errorf("requeued message retries = %d, want 1", retries)
		}
	})
	// Drain the requeued entry so it doesn't leak.
	mq.mu.Lock()
	mq.queue = mq.queue[:0]
	mq.mu.Unlock()
}

// TestMaybeRetry_GivesUpAtMaxRetries verifies the giving-up branch: a message
// already at its retry cap is not re-enqueued.
func TestMaybeRetry_GivesUpAtMaxRetries(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	// Retries == MaxRetries -> give up, no requeue.
	mq.maybeRetry(queuedMessage{ChatID: "c", Text: "x", Retries: 3, MaxRetries: 3}, errTest)
	mq.mu.Lock()
	depth := len(mq.queue)
	mq.mu.Unlock()
	if depth != 0 {
		t.Errorf("expected no requeue at max retries, queue depth = %d", depth)
	}
}

// TestMaybeRetry_StopCancelsRetry verifies the retry goroutine drops the
// message when the queue is stopped before the backoff elapses.
func TestMaybeRetry_StopCancelsRetry(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	mq.maybeRetry(queuedMessage{ChatID: "c", Text: "x", Retries: 0, MaxRetries: 3}, errTest)
	// Immediately close stopCh; the retry goroutine should take the stop branch
	// rather than requeueing.
	close(mq.stopCh)
	time.Sleep(50 * time.Millisecond)
	mq.mu.Lock()
	depth := len(mq.queue)
	mq.mu.Unlock()
	if depth != 0 {
		t.Errorf("stopped retry should not requeue, queue depth = %d", depth)
	}
}

// TestProcessOne_NetworkErrorRetries drives a transport error (server closed)
// which is a non-API error -> always retried.
func TestProcessOne_NetworkErrorRetries(t *testing.T) {
	// Point at a closed server to force a connection error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // close so connections fail

	mq := NewMessageQueue("tok", &http.Client{Timeout: 1 * time.Second}, 100)
	withAPIBase(t, url, func() {
		mq.processOne(queuedMessage{ChatID: "c", Text: "x", MaxRetries: 3})
		deadline := time.Now().Add(3 * time.Second)
		var requeued bool
		for time.Now().Before(deadline) {
			mq.mu.Lock()
			n := len(mq.queue)
			mq.mu.Unlock()
			if n > 0 {
				requeued = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !requeued {
			t.Fatal("expected network error to trigger a retry requeue")
		}
	})
	mq.mu.Lock()
	mq.queue = mq.queue[:0]
	mq.mu.Unlock()
}

// TestDrainOnShutdown_SendsRemaining verifies Stop() drains queued messages
// through sendTelegram on shutdown.
func TestDrainOnShutdown_SendsRemaining(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer ts.Close()

	mq := NewMessageQueue("tok", &http.Client{Timeout: 5 * time.Second}, 1) // rate 1/min so processLoop won't drain it first
	withAPIBase(t, ts.URL, func() {
		// Exhaust the limiter so the live loop can't send; the drain path must.
		for mq.limiter.tryConsume() {
		}
		mq.Enqueue("c1", "msg1", nil)
		mq.Enqueue("c2", "msg2", nil)
		mq.Start()
		mq.Stop() // triggers drainOnShutdown
	})
	if atomic.LoadInt32(&hits) < 1 {
		t.Errorf("expected drain to send at least one message, got %d hits", hits)
	}
}

// TestEnqueue_BacklogCapDropsOldest verifies the maxQueueDepth guard drops the
// oldest message when the backlog is full.
func TestEnqueue_BacklogCapDropsOldest(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	// Fill to capacity.
	for i := 0; i < maxQueueDepth; i++ {
		mq.Enqueue("c", "old", nil)
	}
	mq.mu.Lock()
	before := len(mq.queue)
	mq.mu.Unlock()
	if before != maxQueueDepth {
		t.Fatalf("expected queue at cap %d, got %d", maxQueueDepth, before)
	}
	// One more should drop the oldest and append the new one (depth unchanged).
	mq.Enqueue("c", "newest", nil)
	mq.mu.Lock()
	after := len(mq.queue)
	last := mq.queue[len(mq.queue)-1].Text
	mq.mu.Unlock()
	if after != maxQueueDepth {
		t.Errorf("depth after overflow = %d, want %d", after, maxQueueDepth)
	}
	if last != "newest" {
		t.Errorf("newest message not appended, last = %q", last)
	}
}

var errTest = &telegramAPIError{StatusCode: 500, ErrorCode: 500, Description: "boom"}

// concurrent enqueue under -race
func TestEnqueue_ConcurrentSafe(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mq.Enqueue("c", "x", nil)
			}
		}()
	}
	wg.Wait()
	mq.mu.Lock()
	depth := len(mq.queue)
	mq.mu.Unlock()
	if depth == 0 {
		t.Error("expected messages after concurrent enqueue")
	}
}
