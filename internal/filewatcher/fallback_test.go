package filewatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// newTestWatcher builds a Watcher with a tiny poll interval so fallback
// polling fires quickly under test. Caller is responsible for Close.
func newTestWatcher(t *testing.T, maxWatches int) *Watcher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	w, err := New(ctx, maxWatches)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Shrink the poll interval so pollLoop reacts within the test window.
	w.mu.Lock()
	w.pollInterval = 5 * time.Millisecond
	w.mu.Unlock()
	return w
}

// ---------------------------------------------------------------------------
// isWatchLimitError
// ---------------------------------------------------------------------------

func TestIsWatchLimitError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"no space left", errors.New("write: no space left on device"), true},
		{"too many open files", errors.New("open: too many open files"), true},
		{"ENOSPC token", errors.New("inotify_add_watch: ENOSPC"), true},
		{"unrelated error", errors.New("permission denied"), false},
		{"empty message", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWatchLimitError(tc.err); got != tc.want {
				t.Errorf("isWatchLimitError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// errorString.Error / ErrMaxWatchesReached
// ---------------------------------------------------------------------------

func TestErrorStringError(t *testing.T) {
	if got := ErrMaxWatchesReached.Error(); got != "max watches reached" {
		t.Errorf("ErrMaxWatchesReached.Error() = %q, want %q", got, "max watches reached")
	}
	var custom error = errorString("boom")
	if custom.Error() != "boom" {
		t.Errorf("errorString.Error() = %q, want %q", custom.Error(), "boom")
	}
}

// ---------------------------------------------------------------------------
// mapOp
// ---------------------------------------------------------------------------

func TestMapOp(t *testing.T) {
	cases := []struct {
		name string
		in   fsnotify.Op
		want Op
	}{
		{"create", fsnotify.Create, Create},
		{"write", fsnotify.Write, Write},
		{"remove", fsnotify.Remove, Remove},
		{"rename", fsnotify.Rename, Rename},
		{"chmod falls through to write", fsnotify.Chmod, Write},
		{"zero op defaults to write", fsnotify.Op(0), Write},
		// Create has priority when multiple bits are set.
		{"create+write prefers create", fsnotify.Create | fsnotify.Write, Create},
		// Write has priority over remove/rename when create is absent.
		{"write+remove prefers write", fsnotify.Write | fsnotify.Remove, Write},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapOp(tc.in); got != tc.want {
				t.Errorf("mapOp(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// currentPollInterval
// ---------------------------------------------------------------------------

func TestCurrentPollInterval(t *testing.T) {
	w := &Watcher{pollInterval: 100 * time.Millisecond}
	for i := 0; i < 50; i++ {
		got := w.currentPollInterval()
		if got < w.pollInterval {
			t.Fatalf("currentPollInterval() = %v, must be >= base %v", got, w.pollInterval)
		}
		if got >= w.pollInterval+pollJitter {
			t.Fatalf("currentPollInterval() = %v, must be < base+jitter %v", got, w.pollInterval+pollJitter)
		}
	}
}

// ---------------------------------------------------------------------------
// switchToFallback (idempotency + flag)
// ---------------------------------------------------------------------------

func TestSwitchToFallbackIdempotent(t *testing.T) {
	w := newTestWatcher(t, 100)

	w.mu.Lock()
	if w.fallbackMode {
		w.mu.Unlock()
		t.Fatal("watcher should not start in fallback mode")
	}
	w.switchToFallback()
	if !w.fallbackMode {
		w.mu.Unlock()
		t.Fatal("switchToFallback did not set fallbackMode")
	}
	// Second call must be a no-op (no second pollLoop spawned, no panic).
	w.switchToFallback()
	stillFallback := w.fallbackMode
	w.mu.Unlock()

	if !stillFallback {
		t.Fatal("fallbackMode unexpectedly cleared")
	}
}

// ---------------------------------------------------------------------------
// pollLoop end-to-end: drives Write detection via polling.
// ---------------------------------------------------------------------------

func TestPollLoopDetectsWrite(t *testing.T) {
	w := newTestWatcher(t, 100)

	dir := t.TempDir()
	target := filepath.Join(dir, "poll.txt")
	if err := os.WriteFile(target, []byte("initial"), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	var writes atomic.Int32
	var gotPath atomic.Value
	gotPath.Store("")

	// Enter fallback mode FIRST so Watch does not touch fsnotify and the
	// only event source is pollLoop.
	w.mu.Lock()
	w.switchToFallback()
	w.mu.Unlock()

	if err := w.Watch(target, func(e Event) {
		if e.Op == Write {
			writes.Add(1)
			gotPath.Store(e.Path)
		}
	}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// First poll sees the file for the first time (no prior stat) -> Write.
	// Then mutate it to force a size/modtime change -> another Write.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(target, []byte("changed content longer"), 0644); err != nil {
		t.Fatalf("write change: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if writes.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if writes.Load() < 1 {
		t.Fatalf("expected pollLoop to dispatch at least one Write, got %d", writes.Load())
	}
	if p := gotPath.Load().(string); p != target {
		t.Errorf("dispatched path = %q, want %q", p, target)
	}
}

// pollLoop must skip paths whose os.Stat fails (e.g. removed file) without
// dispatching or panicking.
func TestPollLoopSkipsMissingFile(t *testing.T) {
	w := newTestWatcher(t, 100)

	dir := t.TempDir()
	target := filepath.Join(dir, "ghost.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w.mu.Lock()
	w.switchToFallback()
	w.mu.Unlock()

	var events atomic.Int32
	if err := w.Watch(target, func(e Event) { events.Add(1) }); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Let the first poll register the file (one Write expected), then delete
	// it so subsequent polls hit the os.Stat error branch and skip it.
	time.Sleep(30 * time.Millisecond)
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Record the count, wait several more poll cycles, ensure it does not grow
	// (the missing file is skipped, not re-dispatched).
	time.Sleep(40 * time.Millisecond)
	before := events.Load()
	time.Sleep(60 * time.Millisecond)
	after := events.Load()

	if after != before {
		t.Errorf("missing file should be skipped: events grew from %d to %d", before, after)
	}
}

// pollLoop must exit promptly when the context is cancelled.
func TestPollLoopStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w, err := New(ctx, 100)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w.mu.Lock()
	w.pollInterval = 5 * time.Millisecond
	w.switchToFallback()
	w.mu.Unlock()

	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(target, []byte("x"), 0644)
	if err := w.Watch(target, func(e Event) {}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	cancel() // pollLoop + eventLoop should observe ctx.Done and return.
	if err := w.Close(); err != nil {
		t.Fatalf("Close after cancel: %v", err)
	}
	// Give goroutines a moment to unwind; the -race detector + leak-free exit
	// is the real assertion here.
	time.Sleep(20 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Watch in fallback mode: must NOT touch fsnotify, still counts + registers.
// ---------------------------------------------------------------------------

func TestWatchInFallbackModeSkipsFsnotify(t *testing.T) {
	w := newTestWatcher(t, 100)

	w.mu.Lock()
	w.switchToFallback()
	w.mu.Unlock()

	dir := t.TempDir()
	// A path that does NOT exist on disk. In normal mode fsWatcher.Add would
	// error; in fallback mode Watch must skip Add and succeed.
	target := filepath.Join(dir, "does-not-exist-yet.txt")

	if err := w.Watch(target, func(e Event) {}); err != nil {
		t.Fatalf("Watch in fallback mode should not error on nonexistent path: %v", err)
	}
	if w.WatchCount() != 1 {
		t.Errorf("WatchCount = %d, want 1", w.WatchCount())
	}

	// Unwatch in fallback mode must not call fsWatcher.Remove (guarded) and
	// should decrement the count.
	if err := w.Unwatch(target); err != nil {
		t.Fatalf("Unwatch: %v", err)
	}
	if w.WatchCount() != 0 {
		t.Errorf("WatchCount after unwatch = %d, want 0", w.WatchCount())
	}
}

// Watching the same path twice registers two callbacks but counts as one.
func TestWatchSamePathTwiceCountsOnce(t *testing.T) {
	w := newTestWatcher(t, 100)

	dir := t.TempDir()
	target := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var hits atomic.Int32
	cb := func(e Event) { hits.Add(1) }
	if err := w.Watch(target, cb); err != nil {
		t.Fatalf("Watch 1: %v", err)
	}
	if err := w.Watch(target, cb); err != nil {
		t.Fatalf("Watch 2: %v", err)
	}

	if w.WatchCount() != 1 {
		t.Errorf("WatchCount = %d, want 1 (distinct paths)", w.WatchCount())
	}

	// dispatch should invoke BOTH registered callbacks for the single path.
	w.dispatch(target, Write)
	if hits.Load() != 2 {
		t.Errorf("dispatch invoked %d callbacks, want 2", hits.Load())
	}
}

// ---------------------------------------------------------------------------
// Watch in normal mode on a path fsnotify cannot add returns the underlying
// (non-limit) error and must NOT register a callback or bump the count.
// ---------------------------------------------------------------------------

func TestWatchAddErrorIsNotRegistered(t *testing.T) {
	w := newTestWatcher(t, 100)

	// A path under a directory that does not exist: fsnotify's Add fails with a
	// non-limit error (e.g. "no such file or directory").
	bad := filepath.Join(t.TempDir(), "missing-dir", "missing.txt")

	err := w.Watch(bad, func(e Event) {})
	if err == nil {
		t.Fatal("expected Watch to return the fsnotify Add error for a nonexistent path")
	}
	if isWatchLimitError(err) {
		t.Fatalf("expected a non-limit error, got limit error: %v", err)
	}
	// Failed Add must leave the watcher untouched: no callback, no count, not in
	// fallback mode.
	if w.WatchCount() != 0 {
		t.Errorf("WatchCount = %d, want 0 after failed Add", w.WatchCount())
	}
	w.mu.RLock()
	_, registered := w.callbacks[bad]
	fb := w.fallbackMode
	w.mu.RUnlock()
	if registered {
		t.Error("callback should not be registered for a failed Add")
	}
	if fb {
		t.Error("non-limit Add error must not switch to fallback mode")
	}
}

// ---------------------------------------------------------------------------
// Unwatch on unknown path is a no-op returning nil.
// ---------------------------------------------------------------------------

func TestUnwatchUnknownPath(t *testing.T) {
	w := newTestWatcher(t, 100)
	if err := w.Unwatch("/no/such/path/registered"); err != nil {
		t.Errorf("Unwatch unknown path = %v, want nil", err)
	}
	if w.WatchCount() != 0 {
		t.Errorf("WatchCount = %d, want 0", w.WatchCount())
	}
}

// ---------------------------------------------------------------------------
// New defaults maxWatches when given <= 0.
// ---------------------------------------------------------------------------

func TestNewDefaultsMaxWatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := New(ctx, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if w.maxWatches != defaultMaxWatches {
		t.Errorf("maxWatches = %d, want default %d", w.maxWatches, defaultMaxWatches)
	}

	wNeg, err := New(ctx, -5)
	if err != nil {
		t.Fatalf("New negative: %v", err)
	}
	defer wNeg.Close()
	if wNeg.maxWatches != defaultMaxWatches {
		t.Errorf("maxWatches (neg) = %d, want default %d", wNeg.maxWatches, defaultMaxWatches)
	}
}

// ---------------------------------------------------------------------------
// eventLoop: real fsnotify Write + Remove dispatch through the Events channel
// for a watched file. (Watching the directory itself does not surface child
// create/remove events on the kqueue backend, so we watch the file directly.)
// ---------------------------------------------------------------------------

func TestEventLoopWriteAndRemove(t *testing.T) {
	w := newTestWatcher(t, 100)

	dir := t.TempDir()
	target := filepath.Join(dir, "evt.txt")
	if err := os.WriteFile(target, []byte("init"), 0644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	var mu sync.Mutex
	seen := map[Op]int{}
	if err := w.Watch(target, func(e Event) {
		mu.Lock()
		seen[e.Op]++
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Watch file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(target, []byte("a much larger replacement body"), 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		w, r := seen[Write], seen[Remove]
		mu.Unlock()
		if w >= 1 && r >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen[Write] == 0 {
		t.Error("expected at least one Write event from eventLoop")
	}
	if seen[Remove] == 0 {
		t.Error("expected a Remove event from eventLoop")
	}
}

// ---------------------------------------------------------------------------
// handleWatchError: the eventLoop error branch. Tested directly (rather than
// by writing into fsnotify's internally-owned Errors channel, which races the
// library's own reader goroutine).
// ---------------------------------------------------------------------------

func TestHandleWatchErrorTriggersFallback(t *testing.T) {
	w := newTestWatcher(t, 100)

	w.handleWatchError(errors.New("inotify_add_watch: no space left on device"))

	w.mu.RLock()
	fb := w.fallbackMode
	w.mu.RUnlock()
	if !fb {
		t.Fatal("watch-limit error did not switch to fallback mode")
	}
}

func TestHandleWatchErrorIgnoresNonLimitError(t *testing.T) {
	w := newTestWatcher(t, 100)

	w.handleWatchError(errors.New("some transient read error"))

	w.mu.RLock()
	fb := w.fallbackMode
	w.mu.RUnlock()
	if fb {
		t.Error("non-limit error should not trigger fallback mode")
	}
}

func TestHandleWatchErrorNil(t *testing.T) {
	w := newTestWatcher(t, 100)
	// nil error must be safely ignored (no fallback, no panic).
	w.handleWatchError(nil)
	w.mu.RLock()
	fb := w.fallbackMode
	w.mu.RUnlock()
	if fb {
		t.Error("nil error should not trigger fallback mode")
	}
}

// ---------------------------------------------------------------------------
// Concurrency: hammer Watch/Unwatch/WatchCount/dispatch under -race.
// ---------------------------------------------------------------------------

func TestWatcherConcurrentAccess(t *testing.T) {
	w := newTestWatcher(t, 1000)
	dir := t.TempDir()

	const workers = 8
	const iters = 50
	var wg sync.WaitGroup
	wg.Add(workers)
	for g := 0; g < workers; g++ {
		go func(id int) {
			defer wg.Done()
			path := filepath.Join(dir, "f"+string(rune('a'+id))+".txt")
			_ = os.WriteFile(path, []byte("x"), 0644)
			for i := 0; i < iters; i++ {
				_ = w.Watch(path, func(e Event) {})
				_ = w.WatchCount()
				w.dispatch(path, Write)
				_ = w.Unwatch(path)
			}
		}(g)
	}
	wg.Wait()
}

// dispatch on a path with no callbacks must be a safe no-op.
func TestDispatchNoCallbacks(t *testing.T) {
	w := newTestWatcher(t, 100)
	// Should not panic and should not block.
	w.dispatch("/unregistered/path", Create)
}
