package telegram

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestHandleOutput_LargeBufferImmediateFlush drives the >streamFlushSize branch
// where HandleOutput flushes immediately rather than scheduling a timer.
func TestHandleOutput_LargeBufferImmediateFlush(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)

	big := strings.Repeat("a", streamFlushSize+100)
	s.HandleOutput("chat1", big)

	// Immediate flush should have enqueued a <pre> message and emptied the buffer.
	mq.mu.Lock()
	qLen := len(mq.queue)
	var text string
	if qLen > 0 {
		text = mq.queue[len(mq.queue)-1].Text
	}
	mq.mu.Unlock()
	if qLen == 0 {
		t.Fatal("expected immediate flush to enqueue a message")
	}
	if !strings.HasPrefix(text, "<pre>") {
		t.Errorf("flushed output should be wrapped in <pre>, got %q", text[:20])
	}

	s.mu.Lock()
	buf := s.buffers["chat1"]
	s.mu.Unlock()
	if buf != nil && buf.data.Len() != 0 {
		t.Errorf("buffer should be empty after immediate flush, len=%d", buf.data.Len())
	}
}

// TestHandleOutput_DelayedFlush drives the timer-scheduled flush path for a
// small buffer.
func TestHandleOutput_DelayedFlush(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)

	s.HandleOutput("chat1", "small output")

	// Wait past streamFlushDelay for the AfterFunc to fire.
	deadline := time.Now().Add(streamFlushDelay + time.Second)
	var flushed bool
	for time.Now().Before(deadline) {
		mq.mu.Lock()
		n := len(mq.queue)
		mq.mu.Unlock()
		if n > 0 {
			flushed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !flushed {
		t.Error("expected delayed timer flush to enqueue output")
	}
}

// TestFlushBuffer_TruncatesOversize drives the >streamMaxMessage truncation
// branch in flushBufferMatching.
func TestFlushBuffer_TruncatesOversize(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)

	// Build a buffer larger than streamMaxMessage without triggering the
	// immediate flush threshold collapse (HandleOutput trims/strips, so write
	// directly into the buffer).
	s.mu.Lock()
	buf := &outputBuffer{chatID: "chat1"}
	buf.data.WriteString(strings.Repeat("z", streamMaxMessage+500))
	s.buffers["chat1"] = buf
	s.mu.Unlock()

	s.FlushChat("chat1")

	mq.mu.Lock()
	var text string
	if len(mq.queue) > 0 {
		text = mq.queue[len(mq.queue)-1].Text
	}
	mq.mu.Unlock()
	if !strings.Contains(text, "truncated") {
		t.Errorf("oversize flush should include truncation note, got len=%d", len(text))
	}
}

// TestFlushBufferMatching_IdentityGuard verifies the timer-path identity guard:
// if the live buffer was replaced, a stale-captured flush is a no-op.
func TestFlushBufferMatching_IdentityGuard(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)

	stale := &outputBuffer{chatID: "chat1"}
	stale.data.WriteString("stale data")

	live := &outputBuffer{chatID: "chat1"}
	live.data.WriteString("live data")

	s.mu.Lock()
	s.buffers["chat1"] = live
	s.mu.Unlock()

	// Flush with the STALE expected pointer -> should not flush (identity mismatch).
	s.flushBufferMatching("chat1", stale)

	mq.mu.Lock()
	depth := len(mq.queue)
	mq.mu.Unlock()
	if depth != 0 {
		t.Errorf("stale-identity flush should be a no-op, enqueued %d", depth)
	}

	// The live buffer should be untouched.
	s.mu.Lock()
	liveLen := s.buffers["chat1"].data.Len()
	s.mu.Unlock()
	if liveLen == 0 {
		t.Error("live buffer should not have been flushed by stale guard")
	}
}

// TestHandleOutput_EmptyAfterStrip verifies output that strips to empty is
// dropped without creating a buffer.
func TestHandleOutput_EmptyAfterStrip(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)
	s.HandleOutput("chat1", "\x1b[2K\x1b[?25h") // only control sequences
	s.mu.Lock()
	_, exists := s.buffers["chat1"]
	s.mu.Unlock()
	if exists {
		t.Error("control-only output should not create a buffer")
	}
}

// TestRemoveChat_FlushesThenDeletes verifies RemoveChat flushes pending data
// then removes the buffer.
func TestRemoveChat_FlushesThenDeletes(t *testing.T) {
	mq := NewMessageQueue("tok", &http.Client{}, 100)
	s := NewOutputStreamer(mq)
	s.HandleOutput("chat1", "pending output here")

	s.RemoveChat("chat1")

	s.mu.Lock()
	_, exists := s.buffers["chat1"]
	s.mu.Unlock()
	if exists {
		t.Error("RemoveChat should delete the buffer")
	}
	// The pending data should have been flushed.
	mq.mu.Lock()
	depth := len(mq.queue)
	mq.mu.Unlock()
	if depth == 0 {
		t.Error("RemoveChat should flush pending data before deleting")
	}
}
