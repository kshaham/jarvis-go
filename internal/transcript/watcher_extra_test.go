package transcript

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// withShortPoll temporarily shrinks the package-level pollInterval so the
// watchLoop ticks fast enough to drive deterministically inside a test, and
// restores the original value on cleanup.
func withShortPoll(t *testing.T, d time.Duration) {
	t.Helper()
	orig := pollInterval
	pollInterval = d
	t.Cleanup(func() { pollInterval = orig })
}

// stopAndWait stops the watcher and blocks until its watchLoop goroutine has
// fully returned (signalled via the done channel). This is required for any
// test that calls Start while pollInterval is shrunk: the cleanup that restores
// pollInterval must not run while the loop is still reading it, or the race
// detector will (correctly) flag the unsynchronised global access.
func stopAndWait(t *testing.T, w *Watcher) {
	t.Helper()
	w.Stop()
	select {
	case <-w.done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchLoop did not exit within timeout after Stop")
	}
}

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// waitForHook drains the Events() channel until a HookDetectedEvent with the
// wanted hook arrives or the timeout fires.
func waitForHook(t *testing.T, w *Watcher, want HookEvent, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("events channel closed before receiving %q", want)
			}
			hd, isHook := ev.(HookDetectedEvent)
			if !isHook {
				t.Fatalf("expected HookDetectedEvent, got %T", ev)
			}
			if hd.Hook == want {
				return
			}
			// Not the one we want yet; keep draining.
		case <-deadline:
			t.Fatalf("timed out waiting for hook %q", want)
		}
	}
}

// TestStartWatchLoop_FilePreexisting drives Start -> watchLoop -> readNewContent
// -> processEntry end-to-end with a short poll interval and a file that already
// exists when Start is called.
func TestStartWatchLoop_FilePreexisting(t *testing.T) {
	withShortPoll(t, 5*time.Millisecond)

	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, `{"type":"stop"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, "sess-pre")
	w.Start(path)
	defer stopAndWait(t, w)

	waitForHook(t, w, HookStop, 2*time.Second)

	w.mu.Lock()
	running := w.isRunning
	w.mu.Unlock()
	if !running {
		t.Error("expected isRunning to be true after Start")
	}
}

// TestStartWatchLoop_FileAppearsLater exercises the file-existence polling
// branch of watchLoop: the file does not exist at Start, then is created and
// appended to.
func TestStartWatchLoop_FileAppearsLater(t *testing.T) {
	withShortPoll(t, 5*time.Millisecond)

	dir := t.TempDir()
	path := filepath.Join(dir, "later.jsonl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, "sess-later")
	w.Start(path)
	defer stopAndWait(t, w)

	// Let the existence-poll loop spin a couple of times before the file
	// shows up, so we cover the "file missing -> time.After" branch.
	time.Sleep(20 * time.Millisecond)
	writeFile(t, path, `{"type":"permission_prompt"}`+"\n")

	waitForHook(t, w, HookPermissionPrompt, 2*time.Second)
}

// TestStartWatchLoop_AppendedContent verifies the ticker-driven read path picks
// up content appended after the watcher has already consumed the initial file.
func TestStartWatchLoop_AppendedContent(t *testing.T) {
	withShortPoll(t, 5*time.Millisecond)

	dir := t.TempDir()
	path := filepath.Join(dir, "append.jsonl")
	writeFile(t, path, `{"type":"idle_prompt"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, "sess-append")
	w.Start(path)
	defer stopAndWait(t, w)

	waitForHook(t, w, HookIdlePrompt, 2*time.Second)

	// Append a second entry; the ticker should read it on the next tick.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile append: %v", err)
	}
	if _, err := f.WriteString(`{"type":"some_elicitation_dialog"}` + "\n"); err != nil {
		t.Fatalf("append write: %v", err)
	}
	f.Close()

	waitForHook(t, w, HookElicitation, 2*time.Second)
}

// TestStartWatchLoop_CtxCancelWhileWaitingForFile makes sure cancelling the
// context while the loop is still polling for a non-existent file exits cleanly.
func TestStartWatchLoop_CtxCancelWhileWaitingForFile(t *testing.T) {
	withShortPoll(t, 5*time.Millisecond)

	dir := t.TempDir()
	path := filepath.Join(dir, "never.jsonl") // never created

	ctx, cancel := context.WithCancel(context.Background())

	w := NewWatcher(ctx, "sess-cancel")
	w.Start(path)

	time.Sleep(20 * time.Millisecond)
	cancel() // should break the existence-poll loop via ctx.Done()

	// stopAndWait blocks until the watchLoop goroutine has fully returned,
	// proving the ctx.Done() branch of the existence-poll loop is taken.
	stopAndWait(t, w)

	// After Stop, the channel must be closed and drainable without blocking.
	if _, ok := <-w.Events(); ok {
		t.Error("expected events channel to be closed after Stop")
	}
}

// TestReadNewContent_FileMissing covers the os.Open error branch (returns early).
func TestReadNewContent_FileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.jsonl")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path

	w.readNewContent() // must not panic and must not emit

	select {
	case ev := <-w.Events():
		t.Fatalf("expected no event for missing file, got %v", ev)
	default:
	}

	w.mu.Lock()
	pos := w.filePos
	w.mu.Unlock()
	if pos != 0 {
		t.Errorf("filePos should stay 0 on missing file, got %d", pos)
	}
}

// TestReadNewContent_EmptyFile covers the stat.Size()==pos early return where
// both are zero (no new content).
func TestReadNewContent_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	writeFile(t, path, "")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path
	w.readNewContent()

	select {
	case ev := <-w.Events():
		t.Fatalf("expected no event for empty file, got %v", ev)
	default:
	}
}

// TestReadNewContent_NoNewContent covers the size==pos branch after a prior read
// already consumed the whole file.
func TestReadNewContent_NoNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stable.jsonl")
	writeFile(t, path, `{"type":"stop"}`+"\n")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path

	w.readNewContent() // consumes the stop entry
	waitForHook(t, w, HookStop, time.Second)

	posAfterFirst := func() int64 {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.filePos
	}()
	if posAfterFirst == 0 {
		t.Fatal("expected filePos to advance after first read")
	}

	// Second read with no new bytes should be a no-op (size == pos).
	w.readNewContent()
	select {
	case ev := <-w.Events():
		t.Fatalf("expected no second event, got %v", ev)
	default:
	}

	w.mu.Lock()
	posAfterSecond := w.filePos
	w.mu.Unlock()
	if posAfterSecond != posAfterFirst {
		t.Errorf("filePos changed on no-op read: %d -> %d", posAfterFirst, posAfterSecond)
	}
}

// TestReadNewContent_SeekToPos covers the pos>0 seek branch: read once to
// advance pos, append, then read again so the scanner starts mid-file.
func TestReadNewContent_SeekToPos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seek.jsonl")
	writeFile(t, path, `{"type":"permission_prompt"}`+"\n")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path

	w.readNewContent()
	waitForHook(t, w, HookPermissionPrompt, time.Second)

	// Append a new line and read again; filePos>0 so it must seek forward.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	f.WriteString(`{"type":"stop"}` + "\n")
	f.Close()

	w.readNewContent()
	waitForHook(t, w, HookStop, time.Second)
}

// TestReadNewContent_Truncation covers the rotation branch: the file shrinks
// below the recorded pos, so pos resets to 0 and the file is re-read from start.
func TestReadNewContent_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.jsonl")
	writeFile(t, path, `{"type":"idle_prompt"}`+"\n"+`{"type":"stop"}`+"\n")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path

	w.readNewContent()
	waitForHook(t, w, HookIdlePrompt, time.Second)
	waitForHook(t, w, HookStop, time.Second)

	posBefore := func() int64 {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.filePos
	}()
	if posBefore == 0 {
		t.Fatal("expected non-zero pos before truncation")
	}

	// Rotate: replace with a smaller file. Size < pos triggers reset to 0.
	writeFile(t, path, `{"type":"permission_prompt"}`+"\n")

	w.readNewContent()
	waitForHook(t, w, HookPermissionPrompt, time.Second)
}

// TestReadNewContent_MalformedAndBlankLines covers the json.Unmarshal-error
// continue branch and the blank-line skip branch, while still emitting for the
// one valid hook line in the mix.
func TestReadNewContent_MalformedAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := "" +
		"   \n" + // blank/whitespace -> skipped
		"not-json-at-all\n" + // unmarshal error -> skipped
		"\n" + // empty -> skipped
		`{"type":"random_noise"}` + "\n" + // valid json, but no hook -> no event
		`{"type":"stop"}` + "\n" // valid hook -> event
	writeFile(t, path, content)

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path
	w.readNewContent()

	waitForHook(t, w, HookStop, time.Second)

	// Only one event should have been produced.
	select {
	case ev := <-w.Events():
		t.Fatalf("expected exactly one event, got extra %v", ev)
	default:
	}
}

// TestReadNewContent_CRLF verifies the position bookkeeping is correct for
// \r\n line endings (the bug the production comment guards against). After a
// CRLF read, an appended LF-terminated line must still be picked up exactly
// once with no off-by-one.
func TestReadNewContent_CRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.jsonl")
	writeFile(t, path, `{"type":"idle_prompt"}`+"\r\n")

	w := NewWatcher(context.Background(), "sess")
	w.transcriptPath = path
	w.readNewContent()
	waitForHook(t, w, HookIdlePrompt, time.Second)

	pos := func() int64 {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.filePos
	}()
	st, _ := os.Stat(path)
	if pos != st.Size() {
		t.Errorf("filePos %d should equal file size %d after CRLF read", pos, st.Size())
	}

	// Append another CRLF line; it must be read exactly once.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"stop"}` + "\r\n")
	f.Close()

	w.readNewContent()
	waitForHook(t, w, HookStop, time.Second)

	select {
	case ev := <-w.Events():
		t.Fatalf("CRLF off-by-one: got duplicate/extra event %v", ev)
	default:
	}
}

// TestProcessEntry_ChannelFull covers the select default branch in processEntry:
// when the buffered events channel is full, the event is dropped rather than
// blocking.
func TestProcessEntry_ChannelFull(t *testing.T) {
	w := NewWatcher(context.Background(), "sess-full")

	// Fill the buffered channel (capacity 32) so the next send hits default.
	for i := 0; i < cap(w.events); i++ {
		w.events <- HookDetectedEvent{Hook: HookStop, SessionID: "filler"}
	}

	// This must not block; it falls through the default case and drops.
	done := make(chan struct{})
	go func() {
		w.processEntry(TranscriptEntry{Type: "permission_prompt"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("processEntry blocked when channel was full")
	}

	// Drain one and confirm it is a filler, proving the new event was dropped.
	ev := <-w.events
	hd, ok := ev.(HookDetectedEvent)
	if !ok || hd.SessionID != "filler" {
		t.Errorf("expected filler event at head, got %v", ev)
	}
}

// TestProcessEntry_NonHookType covers the early return when the entry type maps
// to no hook event.
func TestProcessEntry_NonHookType(t *testing.T) {
	w := NewWatcher(context.Background(), "sess")
	w.processEntry(TranscriptEntry{Type: "assistant_message"})

	select {
	case ev := <-w.events:
		t.Fatalf("expected no event for non-hook type, got %v", ev)
	default:
	}
}

// TestProcessEntry_SetsSessionID asserts the emitted event carries the watcher's
// session id.
func TestProcessEntry_SetsSessionID(t *testing.T) {
	w := NewWatcher(context.Background(), "abc-123")
	w.processEntry(TranscriptEntry{Type: "stop"})

	select {
	case ev := <-w.events:
		hd, ok := ev.(HookDetectedEvent)
		if !ok {
			t.Fatalf("expected HookDetectedEvent, got %T", ev)
		}
		if hd.SessionID != "abc-123" {
			t.Errorf("SessionID = %q, want %q", hd.SessionID, "abc-123")
		}
		if hd.Hook != HookStop {
			t.Errorf("Hook = %q, want %q", hd.Hook, HookStop)
		}
	default:
		t.Fatal("expected an event for stop entry")
	}
}

// TestDetectHookEvent_Substrings covers the Contains-based matches plus the
// exact "stop" match and a non-matching default, including substring positions.
func TestDetectHookEvent_Substrings(t *testing.T) {
	cases := []struct {
		in   string
		want HookEvent
	}{
		{"tool_permission_prompt_request", HookPermissionPrompt},
		{"mcp_elicitation_dialog", HookElicitation},
		{"user_idle_prompt_v2", HookIdlePrompt},
		{"stop", HookStop},
		{"stopping", ""}, // not an exact "stop" match
		{"", ""},
		{"assistant", ""},
	}
	for _, c := range cases {
		if got := detectHookEvent(c.in); got != c.want {
			t.Errorf("detectHookEvent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestHookDetectedEvent_Type covers the transcriptEventType marker method and
// confirms HookDetectedEvent satisfies the TranscriptEvent interface.
func TestHookDetectedEvent_Type(t *testing.T) {
	var ev TranscriptEvent = HookDetectedEvent{Hook: HookStop, SessionID: "s"}
	if got := ev.transcriptEventType(); got != "hook_detected" {
		t.Errorf("transcriptEventType() = %q, want %q", got, "hook_detected")
	}
}

// TestStop_Idempotent verifies Stop can be called multiple times without
// panicking on a double channel close (closeOnce guard) and is concurrency-safe.
func TestStop_Idempotent(t *testing.T) {
	w := NewWatcher(context.Background(), "sess")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Stop()
		}()
	}
	wg.Wait()

	// Channel must be closed exactly once and readable as closed.
	_, ok := <-w.Events()
	if ok {
		t.Error("expected events channel to be closed after Stop")
	}

	// isRunning must be false.
	w.mu.Lock()
	running := w.isRunning
	w.mu.Unlock()
	if running {
		t.Error("expected isRunning false after Stop")
	}
}

// TestStart_SetsState verifies Start records the path and running flag under the
// mutex before launching the loop.
func TestStart_SetsState(t *testing.T) {
	withShortPoll(t, 5*time.Millisecond)

	dir := t.TempDir()
	path := filepath.Join(dir, "state.jsonl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, "sess-state")
	w.Start(path)
	defer stopAndWait(t, w)

	w.mu.Lock()
	gotPath := w.transcriptPath
	running := w.isRunning
	w.mu.Unlock()

	if gotPath != path {
		t.Errorf("transcriptPath = %q, want %q", gotPath, path)
	}
	if !running {
		t.Error("expected isRunning true after Start")
	}
}
