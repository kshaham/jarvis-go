package office

// P6.3 + P7 hardening · Codegraph MCP support for spawned agents.
// Three responsibilities:
//
//   1. CodegraphBinaryAvailable() — feature-flag the whole capability
//      so hosts without the `codegraph` CLI don't poison every SDK
//      spawn with a dead MCP server entry. Cached once per process.
//   2. EnsureCodegraphIndex(ctx, dir) — idempotent background init of
//      the .codegraph/ index at the team's working directory. Runs
//      `codegraph init -i` (init + initial index in one shot — safe
//      to repeat; existing index becomes a no-op). Per-dir guard so
//      two simultaneous spawns into the same workingDir share one
//      init goroutine instead of stepping on each other.
//   3. SetRootContext(ctx) — daemon bootstrap seam so the background
//      goroutines honour process-level cancellation instead of
//      orphaning subprocesses on shutdown.
//
// Must not block spawn. Failures log and degrade gracefully: agents
// keep running, they just see "index not initialized" if they call an
// mcp__codegraph__* tool before the goroutine finishes.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// codegraphBinaryName is the executable jarvis shells out to. Variable
// so tests can swap to a stub. Same pattern as ultrareviewBinary in
// the sibling office files.
var codegraphBinaryName = "codegraph"

// codegraphInitTimeout caps the init+index call. A 286-file project
// takes ~3s on a warm cache; the timeout is generous to cover cold-
// disk re-index of larger workspaces without ever blocking a spawn's
// goroutine for more than a couple minutes.
var codegraphInitTimeout = 5 * time.Minute

// codegraphRetryAfterFailure is the minimum interval between init
// retries for a workingDir whose previous init failed. Prevents a
// failed binary from being re-spawned on every team restart in a
// thrash loop.
var codegraphRetryAfterFailure = 60 * time.Second

// codegraphLookupOnce + codegraphAvailable cache the binary check.
// LookPath hits the filesystem; doing it on every spawn is silly when
// the answer is process-stable.
var (
	codegraphLookupOnce sync.Once
	codegraphAvailable  bool
)

// rootCtx is the long-lived daemon context. Set once at bootstrap via
// SetRootContext; falls back to context.Background() if unset (lib /
// test use). All background goroutines derive their timeout context
// from this so a server shutdown cancels in-flight inits cleanly
// instead of orphaning the codegraph subprocess.
var (
	rootCtxMu sync.RWMutex
	rootCtx   context.Context = context.Background()
)

// SetRootContext installs the daemon-lifetime context. Call once from
// the server bootstrap (cmd/jarvis main or server.go) with the same
// context the HTTP server uses. Subsequent calls overwrite — useful
// for tests that want a cancellable parent without restarting the
// process.
func SetRootContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	rootCtxMu.Lock()
	rootCtx = ctx
	rootCtxMu.Unlock()
}

// currentRootCtx returns the live daemon context. Snapshotted on the
// caller's goroutine so background workers don't race a concurrent
// SetRootContext swap.
func currentRootCtx() context.Context {
	rootCtxMu.RLock()
	defer rootCtxMu.RUnlock()
	return rootCtx
}

// codegraphInitState is the per-workingDir lifecycle record. Replaces
// the earlier sync.Once-based approach which had a race: deleting the
// once on failure let a second caller install a new once + spawn a
// parallel init while the first goroutine was still running.
//
// Lifecycle: starting → done (success | error). Subsequent callers
// see the done channel closed and read lastErr to decide whether to
// retry (only after codegraphRetryAfterFailure elapses post-failure).
type codegraphInitState struct {
	done     chan struct{}
	once     sync.Once // ensures the goroutine body runs exactly once for this state
	lastErr  error
	finishAt time.Time // when the init goroutine returned
}

// codegraphInitMap holds one *codegraphInitState per workingDir.
// Bounded by the number of distinct team working directories, which
// is small in practice.
var codegraphInitMap sync.Map // map[string]*codegraphInitState

// CodegraphBinaryAvailable reports whether the `codegraph` CLI is on
// PATH. Result cached after the first call.
func CodegraphBinaryAvailable() bool {
	codegraphLookupOnce.Do(func() {
		if _, err := exec.LookPath(codegraphBinaryName); err == nil {
			codegraphAvailable = true
			slog.Info("codegraph CLI detected; agents will get mcp__codegraph__* tools",
				"binary", codegraphBinaryName)
		} else {
			slog.Info("codegraph CLI not found on PATH; agents will not see mcp__codegraph__* tools",
				"binary", codegraphBinaryName, "err", err)
		}
	})
	return codegraphAvailable
}

// EnsureCodegraphIndex runs `codegraph init -i <dir>` in the
// background once per workingDir. Safe to call from a hot spawn path —
// the actual exec runs in a goroutine and the function returns
// immediately. The returned error only describes synchronous
// validation failures (empty / relative / non-existent dir, binary
// missing); shell-out failures are logged and observable via the
// per-dir state's lastErr.
//
// Concurrency: simultaneous callers for the same workingDir share
// one *codegraphInitState; the first caller's goroutine runs the
// exec, subsequent callers are no-ops. After a failed init, retries
// are throttled by codegraphRetryAfterFailure.
//
// Cancellation: the goroutine's context derives from the daemon's
// root context (see SetRootContext). A daemon shutdown cancels the
// subprocess cleanly instead of orphaning it. The ctx argument is
// retained in the signature for future scoped cancellation but is
// NOT used today — using the request ctx would kill the init when
// the spawn HTTP request returned (i.e. immediately).
func EnsureCodegraphIndex(_ context.Context, workingDir string) error {
	if workingDir == "" {
		return errors.New("codegraph: workingDir is required")
	}
	if !filepath.IsAbs(workingDir) {
		return fmt.Errorf("codegraph: workingDir must be absolute: %q", workingDir)
	}
	info, statErr := os.Stat(workingDir)
	if statErr != nil {
		return fmt.Errorf("codegraph: workingDir not accessible: %w", statErr)
	}
	if !info.IsDir() {
		return fmt.Errorf("codegraph: workingDir is not a directory: %q", workingDir)
	}
	if !CodegraphBinaryAvailable() {
		return errors.New("codegraph: binary not on PATH")
	}

	// Fast-path: a successful (or recent failed) init exists for this
	// dir. Successful = no-op; recent failure = throttle the retry.
	if existingAny, ok := codegraphInitMap.Load(workingDir); ok {
		existing := existingAny.(*codegraphInitState)
		select {
		case <-existing.done:
			// Init finished. Decide whether to retry based on outcome
			// + age.
			if existing.lastErr == nil {
				return nil // already indexed
			}
			if time.Since(existing.finishAt) < codegraphRetryAfterFailure {
				return fmt.Errorf("codegraph: last init failed at %s, throttling retry for %s",
					existing.finishAt.Format(time.RFC3339), codegraphRetryAfterFailure)
			}
			// Fall through to a fresh attempt below; LoadOrStore will
			// install a new state record.
		default:
			// Init still running — second caller is a no-op.
			return nil
		}
	}

	// Install a new state. LoadOrStore ensures only one goroutine wins
	// the race for an empty slot; losers reuse the winner's state.
	fresh := &codegraphInitState{done: make(chan struct{})}
	actualAny, loaded := codegraphInitMap.LoadOrStore(workingDir, fresh)
	actual := actualAny.(*codegraphInitState)
	if loaded && actual != fresh {
		// Someone else won the race after we did the fast-path check.
		// Their goroutine will do the work; we no-op.
		return nil
	}

	// Snapshot the binary + timeout on the caller goroutine so the
	// background closure doesn't race a concurrent test-time swap.
	binary := codegraphBinaryName
	timeout := codegraphInitTimeout
	parentCtx := currentRootCtx()

	// safego: panic in the once.Do closure would leave actual.done
	// unclosed forever (any subsequent EnsureCodegraphIndex caller
	// would deadlock waiting on it). Recovery also keeps the daemon
	// alive across an exec subprocess panic.
	safego.Go("office.codegraph.init", func() {
		actual.once.Do(func() {
			defer close(actual.done)
			actual.finishAt = time.Time{} // marker: still running

			callCtx, cancel := context.WithTimeout(parentCtx, timeout)
			defer cancel()

			start := time.Now()
			cmd := exec.CommandContext(callCtx, binary, "init", "-i", workingDir)
			out, err := cmd.CombinedOutput()
			took := time.Since(start)
			actual.finishAt = time.Now()
			if err != nil {
				actual.lastErr = err
				slog.Warn("codegraph init failed",
					"dir", workingDir, "took", took, "err", err,
					"output", truncateCodegraphLog(string(out)))
				return
			}
			actual.lastErr = nil
			slog.Info("codegraph index ready",
				"dir", workingDir, "took", took)
		})
	})
	return nil
}

func truncateCodegraphLog(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
