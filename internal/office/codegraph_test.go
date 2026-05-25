package office

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// resetCodegraphCacheForTest clears the global LookPath cache + per-dir
// init map so tests can re-exercise both without bleeding state.
// Call BEFORE installing a binary stub so the LookPath cache is
// invalidated and re-runs against the new stub path.
func resetCodegraphCacheForTest() {
	codegraphLookupOnce = sync.Once{}
	codegraphAvailable = false
	codegraphInitMap = sync.Map{}
}

func installCodegraphStub(t *testing.T, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codegraph")
	body := "#!/bin/sh\necho codegraph stub: $@\nexit " + map[int]string{
		0: "0", 1: "1", 7: "7",
	}[exitCode] + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// setupCodegraphStub wires the stub binary in correct order: cache
// reset BOTH before (so any prior test's LookPath-false isn't
// remembered for our stub) AND in the cleanup (so a future test
// starts clean). P7 fix for the audit's cache-reset ordering nit.
func setupCodegraphStub(t *testing.T, exitCode int) {
	t.Helper()
	prev := codegraphBinaryName
	resetCodegraphCacheForTest()
	codegraphBinaryName = installCodegraphStub(t, exitCode)
	t.Cleanup(func() {
		codegraphBinaryName = prev
		resetCodegraphCacheForTest()
	})
}

func TestEnsureCodegraphIndex_RejectsEmptyDir(t *testing.T) {
	setupCodegraphStub(t, 0)
	if err := EnsureCodegraphIndex(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty workingDir")
	}
}

func TestEnsureCodegraphIndex_RejectsRelativeDir(t *testing.T) {
	setupCodegraphStub(t, 0)
	if err := EnsureCodegraphIndex(context.Background(), "relative/path"); err == nil {
		t.Fatal("expected error for relative workingDir")
	}
}

func TestEnsureCodegraphIndex_RejectsNonExistentDir(t *testing.T) {
	setupCodegraphStub(t, 0)
	bogus := filepath.Join(t.TempDir(), "does-not-exist")
	if err := EnsureCodegraphIndex(context.Background(), bogus); err == nil {
		t.Fatal("expected error for non-existent workingDir")
	}
}

func TestEnsureCodegraphIndex_RejectsFileNotDir(t *testing.T) {
	setupCodegraphStub(t, 0)
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnsureCodegraphIndex(context.Background(), filePath); err == nil {
		t.Fatal("expected error when workingDir is a file")
	}
}

func TestEnsureCodegraphIndex_NoBinaryReturnsError(t *testing.T) {
	prev := codegraphBinaryName
	resetCodegraphCacheForTest()
	codegraphBinaryName = "this-binary-definitely-does-not-exist-xyz123"
	t.Cleanup(func() {
		codegraphBinaryName = prev
		resetCodegraphCacheForTest()
	})
	if err := EnsureCodegraphIndex(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected error when codegraph binary missing")
	}
}

func TestEnsureCodegraphIndex_HappyPathFiresGoroutine(t *testing.T) {
	setupCodegraphStub(t, 0)
	dir := t.TempDir()
	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Fatalf("EnsureCodegraphIndex: %v", err)
	}

	// Wait for the background goroutine to complete (signals via
	// state.done) before letting cleanup tear down the stub binary.
	stateAny, ok := codegraphInitMap.Load(dir)
	if !ok {
		t.Fatal("codegraphInitMap missing entry for dir after EnsureCodegraphIndex returned")
	}
	state := stateAny.(*codegraphInitState)
	select {
	case <-state.done:
		// Success path leaves lastErr == nil.
		if state.lastErr != nil {
			t.Errorf("init reported error: %v", state.lastErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("init goroutine did not finish within 3s")
	}
}

func TestEnsureCodegraphIndex_RepeatCallShortCircuits(t *testing.T) {
	setupCodegraphStub(t, 0)
	dir := t.TempDir()
	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Fatalf("first EnsureCodegraphIndex: %v", err)
	}
	// Wait for the first init to complete.
	stateAny, _ := codegraphInitMap.Load(dir)
	state := stateAny.(*codegraphInitState)
	<-state.done

	// Second call against the same dir should no-op (return nil)
	// without spawning a second goroutine.
	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Fatalf("second EnsureCodegraphIndex: %v", err)
	}
	// Same state pointer = no new goroutine installed.
	stateAny2, _ := codegraphInitMap.Load(dir)
	if stateAny2.(*codegraphInitState) != state {
		t.Error("second call replaced the state record; expected no-op")
	}
}

func TestEnsureCodegraphIndex_FailureThrottlesRetry(t *testing.T) {
	prev := codegraphRetryAfterFailure
	codegraphRetryAfterFailure = 60 * time.Second // ensure we hit the throttle path
	t.Cleanup(func() { codegraphRetryAfterFailure = prev })
	setupCodegraphStub(t, 1) // non-zero exit
	dir := t.TempDir()
	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Fatalf("EnsureCodegraphIndex: %v", err)
	}
	stateAny, _ := codegraphInitMap.Load(dir)
	state := stateAny.(*codegraphInitState)
	<-state.done
	if state.lastErr == nil {
		t.Fatal("expected lastErr set after non-zero exit")
	}
	// Immediately retry — should return a throttle error.
	if err := EnsureCodegraphIndex(context.Background(), dir); err == nil {
		t.Error("expected throttle error on immediate retry after failure")
	}
}

func TestEnsureCodegraphIndex_FailureAllowsRetryAfterInterval(t *testing.T) {
	prev := codegraphRetryAfterFailure
	codegraphRetryAfterFailure = 50 * time.Millisecond
	t.Cleanup(func() { codegraphRetryAfterFailure = prev })
	setupCodegraphStub(t, 1)
	dir := t.TempDir()
	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Fatalf("first EnsureCodegraphIndex: %v", err)
	}
	stateAny, _ := codegraphInitMap.Load(dir)
	<-stateAny.(*codegraphInitState).done
	time.Sleep(80 * time.Millisecond) // exceed the retry interval

	if err := EnsureCodegraphIndex(context.Background(), dir); err != nil {
		t.Errorf("retry after interval should succeed (kicks off a new attempt): %v", err)
	}
}

func TestEnsureCodegraphIndex_ConcurrentSpawnsShareOneInit(t *testing.T) {
	// 5 goroutines call EnsureCodegraphIndex against the same dir
	// concurrently; assert only one subprocess actually started.
	// Validates the sync.Once race fix from P7.2.6: under the old
	// implementation, deleting the once on failure let racing callers
	// install fresh onces and start parallel inits. The new state-
	// record approach must serialise via LoadOrStore.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}

	// Stub that appends its PID to a tempfile so we can count actual
	// process invocations. Sleeps a moment so concurrent racers have
	// time to all observe a pending init.
	stubDir := t.TempDir()
	pidFile := filepath.Join(stubDir, "pids.log")
	stubPath := filepath.Join(stubDir, "codegraph")
	body := "#!/bin/sh\necho $$ >> '" + pidFile + "'\nsleep 0.1\nexit 0\n"
	if err := os.WriteFile(stubPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	prev := codegraphBinaryName
	resetCodegraphCacheForTest()
	codegraphBinaryName = stubPath
	t.Cleanup(func() {
		codegraphBinaryName = prev
		resetCodegraphCacheForTest()
	})

	dir := t.TempDir()
	const concurrency = 5
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_ = EnsureCodegraphIndex(context.Background(), dir)
		}()
	}
	wg.Wait()

	// Wait for the single in-flight goroutine to finish.
	stateAny, ok := codegraphInitMap.Load(dir)
	if !ok {
		t.Fatal("no state record installed after concurrent EnsureCodegraphIndex")
	}
	<-stateAny.(*codegraphInitState).done

	// Read the pid file; should contain exactly one line.
	contents, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	lines := 0
	for _, b := range contents {
		if b == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("expected exactly 1 subprocess invocation, got %d (pid file: %q)", lines, contents)
	}
}

func TestCodegraphBinaryAvailable_CachesLookup(t *testing.T) {
	setupCodegraphStub(t, 0)
	if !CodegraphBinaryAvailable() {
		t.Fatal("expected available with stub on PATH")
	}
	// Second call hits cache; result must be stable.
	if !CodegraphBinaryAvailable() {
		t.Fatal("second call must return cached true")
	}
}

func TestSetRootContext_NilFallsBackToBackground(t *testing.T) {
	SetRootContext(nil)
	ctx := currentRootCtx()
	if ctx == nil {
		t.Fatal("currentRootCtx returned nil after SetRootContext(nil)")
	}
	if ctx.Err() != nil {
		t.Errorf("background ctx should not be done; got %v", ctx.Err())
	}
}

func TestSetRootContext_PropagatesCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	SetRootContext(parent)
	t.Cleanup(func() { SetRootContext(context.Background()) })

	cancel()
	got := currentRootCtx()
	if got.Err() == nil {
		t.Error("currentRootCtx should reflect parent cancellation")
	}
}
