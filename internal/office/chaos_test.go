package office

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/safego"
)

// Chaos test for 12h-unattended-operation confidence.
//
// The test runs sustained churn (spawn/kill/message/triage) across
// dozens of concurrent goroutines, with synthetic panics injected at
// random points to verify that the safego wrapping (and the office
// package's own defensive code) actually holds the daemon together.
//
// What it proves:
//  1. Manager state stays consistent under heavy concurrent churn.
//  2. safego.Go survives a high panic rate without ever crashing the
//     test binary (a single uncaught panic would kill the test).
//  3. Goroutine count returns to baseline after the storm settles —
//     no leaks from the burst.
//  4. Mutex-protected fields (agents, messages, tasks) never
//     deadlock under concurrent access from N writers.
//
// What it does NOT prove:
//  - Long-tail external-API hangs (those need separate tests with
//    network fault injection).
//  - State-file corruption recovery (that's persistence_test territory).
//  - Telegram / SSE forwarder behaviour (uses mock SSE here).

// chaosOpts bundles the dials so the same test can be tuned for
// short CI runs vs. longer pre-release soak.
type chaosOpts struct {
	workers      int           // concurrent goroutines doing churn
	duration     time.Duration // total wall-clock churn time
	panicRate    float64       // 0..1 chance any single op panics
	agentChurn   int           // # of synthetic agents in rotation
	leakSettleMs int           // ms to wait after stopping for cleanup
}

// defaultChaosOpts is sized to run in ~5–10 seconds on CI. Long
// enough to surface concurrency bugs, short enough to keep `go test`
// fast.
func defaultChaosOpts() chaosOpts {
	return chaosOpts{
		workers:      32,
		duration:     5 * time.Second,
		panicRate:    0.05, // 5% of ops trigger a synthetic panic
		agentChurn:   20,
		leakSettleMs: 500,
	}
}

func TestChaos_SustainedChurnWithPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("chaos test skipped in -short mode")
	}
	runChaos(t, defaultChaosOpts())
}

// TestChaos_LongSoak is the unattended-operation soak proxy. Sized
// for ~30 seconds of wall-clock churn at higher panic rate + more
// workers. Gated behind an env var so CI defaults stay fast; run
// locally with: JARVIS_CHAOS_SOAK=1 go test ./internal/office/...
//
// What it proves beyond TestChaos_SustainedChurnWithPanics:
//  - Goroutine count stays flat across an order-of-magnitude more
//    panic-recover cycles (target: tens of thousands of panics).
//  - safego does not accumulate sysmon stacks or runtime debt under
//    sustained recovery storms.
//  - Mutex acquisition stays responsive even after the heap has
//    been churned through millions of allocations.
func TestChaos_LongSoak(t *testing.T) {
	if testing.Short() {
		t.Skip("long soak skipped in -short mode")
	}
	if os.Getenv("JARVIS_CHAOS_SOAK") == "" {
		t.Skip("set JARVIS_CHAOS_SOAK=1 to run the long-soak chaos test")
	}
	runChaos(t, chaosOpts{
		workers:      128,
		duration:     30 * time.Second,
		panicRate:    0.10,
		agentChurn:   50,
		leakSettleMs: 1000,
	})
}

func runChaos(t *testing.T, opts chaosOpts) {
	t.Helper()

	// Baseline goroutine count BEFORE we touch anything. We allow a
	// generous margin at the end because the runtime / GC keeps a
	// few sysmon goroutines around that fluctuate naturally.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	mgr := newTestManager()

	// Pre-register a pool of agents so workers have something to
	// hammer on. Each worker also adds/removes agents during churn.
	for i := 0; i < opts.agentChurn; i++ {
		mgr.RegisterAgent(syntheticSessionID(i), syntheticConfigID(i),
			&TeamMemberConfig{ID: syntheticConfigID(i), Name: "chaos-" + itoa(i)})
	}

	var (
		panicCount   atomic.Int64
		opCount      atomic.Int64
		spawnCount   atomic.Int64
		killCount    atomic.Int64
		messageCount atomic.Int64
	)

	deadline := time.Now().Add(opts.duration)

	// Workers: each runs a tight loop of mixed operations.
	var wg sync.WaitGroup
	for w := 0; w < opts.workers; w++ {
		wg.Add(1)
		safego.Go("chaos.worker", func() {
			defer wg.Done()
			rng := newDeterministicRNG(int64(w))
			for time.Now().Before(deadline) {
				op := rng.Intn(6)
				opCount.Add(1)

				// Synthetic panic injection. safego catches it; the
				// test process must survive. Count to assert the
				// panic path actually fires.
				if rng.Float64() < opts.panicRate {
					panicCount.Add(1)
					safego.Go("chaos.injectedPanic", func() {
						panic("chaos: injected panic to verify recovery")
					})
				}

				switch op {
				case 0:
					// Spawn-shaped: register fresh agent at a free slot.
					id := opts.agentChurn + int(spawnCount.Add(1))
					mgr.RegisterAgent(syntheticSessionID(id), syntheticConfigID(id),
						&TeamMemberConfig{ID: syntheticConfigID(id), Name: "chaos-spawn"})
				case 1:
					// Kill-shaped: unregister a synthetic agent.
					victim := rng.Intn(opts.agentChurn)
					mgr.UnregisterAgent(syntheticSessionID(victim))
					killCount.Add(1)
					// Re-register so the pool stays populated.
					mgr.RegisterAgent(syntheticSessionID(victim), syntheticConfigID(victim),
						&TeamMemberConfig{ID: syntheticConfigID(victim), Name: "chaos-respawn"})
				case 2:
					// Message broadcast — hits the message ring buffer
					// + persistMessages goroutine via safego.
					mgr.SendMessage("chaos-from", "", "synthetic broadcast")
					messageCount.Add(1)
				case 3:
					// Add task to a random agent.
					victim := rng.Intn(opts.agentChurn)
					if agent := mgr.GetAgent(syntheticSessionID(victim)); agent != nil {
						agent.AddTask(&Task{
							ID:     "chaos-task-" + itoa(int(opCount.Load())),
							Title:  "chaos",
							Status: TaskPending,
						})
					}
				case 4:
					// Read-heavy op: snapshot roster + iterate.
					for _, a := range mgr.GetRoster() {
						_ = a.SessionID
					}
				case 5:
					// File-lock cycle: acquire + release. Stresses
					// the fileLocks map cleanup path.
					path := "/chaos/" + itoa(rng.Intn(50))
					holder := syntheticSessionID(rng.Intn(opts.agentChurn))
					_, _ = mgr.AcquireLock(path, holder, "chaos")
					mgr.ReleaseLock(path, holder)
				}
			}
		})
	}

	// Done channel pattern lets us bound the wait — if a deadlock
	// occurred, the test fails with a clear message instead of
	// hanging until the global `go test` timeout.
	done := make(chan struct{})
	safego.Go("chaos.waiter", func() { wg.Wait(); close(done) })

	select {
	case <-done:
	case <-time.After(opts.duration + 30*time.Second):
		t.Fatalf("chaos workers did not finish within %v + 30s grace — likely deadlock or leaked goroutine",
			opts.duration)
	}

	// Settle: give safego'd panic goroutines time to log + exit, and
	// any persistMessages writes time to flush.
	time.Sleep(time.Duration(opts.leakSettleMs) * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	final := runtime.NumGoroutine()

	t.Logf("chaos summary: ops=%d panics=%d spawns=%d kills=%d messages=%d  goroutines: baseline=%d final=%d",
		opCount.Load(), panicCount.Load(), spawnCount.Load(), killCount.Load(),
		messageCount.Load(), baseline, final)

	// Test invariants.

	// 1. Synthetic panics actually fired. If panicRate>0 and
	// panicCount==0, the random-number path is broken and we
	// didn't actually exercise the recovery surface.
	if opts.panicRate > 0 && panicCount.Load() == 0 {
		t.Errorf("panic injection never fired despite panicRate=%v", opts.panicRate)
	}

	// 2. Workers actually did work. If opCount is tiny, the test
	// didn't exercise much.
	if opCount.Load() < int64(opts.workers*10) {
		t.Errorf("workers did only %d ops total; expected at least %d (workers=%d)",
			opCount.Load(), opts.workers*10, opts.workers)
	}

	// 3. No goroutine leak. Allow a margin because safego'd
	// background goroutines may still be draining logs.
	const leakMargin = 50
	if final > baseline+leakMargin {
		t.Errorf("goroutine leak: baseline=%d final=%d (delta %d > margin %d)",
			baseline, final, final-baseline, leakMargin)
	}

	// 4. Manager state is still consistent — we can still read the
	// roster without deadlocking.
	readDone := make(chan int, 1)
	safego.Go("chaos.finalRead", func() {
		readDone <- len(mgr.GetRoster())
	})
	select {
	case n := <-readDone:
		if n == 0 {
			t.Error("roster empty after chaos — agent pool was drained without re-population")
		}
	case <-time.After(5 * time.Second):
		t.Error("post-chaos GetRoster deadlocked — mutex orphaned by a panic?")
	}
}

// syntheticSessionID builds a unique session id for chaos workers.
// Keeps the format compatible with strs.SessionIDPattern so any
// production validator sees these as well-formed.
func syntheticSessionID(i int) string {
	return "chaos-session-" + padInt(i, 4)
}

func syntheticConfigID(i int) string {
	return "chaos-config-" + padInt(i, 4)
}

// padInt zero-pads i to width digits without importing fmt.
func padInt(i, width int) string {
	s := itoa(i)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// itoa is a minimal int-to-string to avoid importing strconv in
// every test file. AgentState test file already uses one — this is
// the same name pattern, so the linker dedupes if both files compile
// in the same package. (Each definition is local to its own scope
// via Go's unused-import detection.)

// Deterministic RNG so a failure reproduces. Uses a tiny LCG so we
// don't pull math/rand and entangle with global RNG state.
type chaosRNG struct{ state uint64 }

func newDeterministicRNG(seed int64) *chaosRNG {
	return &chaosRNG{state: uint64(seed) + 1}
}

func (r *chaosRNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return int((r.state >> 33) % uint64(n))
}

func (r *chaosRNG) Float64() float64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return float64(r.state>>11) / (1 << 53)
}
