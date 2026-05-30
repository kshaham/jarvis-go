package handler

// Concurrent-load / -race regression guard for AudioJobManager (standup_audio.go).
//
// AudioJobManager coalesces concurrent requests for the same standup ID and
// bounds the number of simultaneous synthesis jobs with a counting semaphore
// (cap = maxConcurrentAudioJobs) plus a sync.WaitGroup for shutdown drain. The
// shared mutable state -- the running map, the sem channel, and the wg -- is
// guarded by m.mu in Start and in run's deferred cleanup. These tests hammer
// Start from many goroutines (reads/mutations) concurrently with the in-flight
// run goroutines (mutations of the same map/sem/wg) so the -race detector can
// catch any unsynchronized access, and assert the two functional invariants:
//
//   1. Coalescing: many Start(sameID) calls collapse to a single in-flight job.
//   2. Cap: no more than maxConcurrentAudioJobs jobs run concurrently, and the
//      sem + wg never deadlock or leak (Wait() returns).
//
// The fake Synthesizer blocks inside CondenseScript on a channel the test
// controls, so jobs are held "in flight" deterministically -- no time.Sleep
// ordering, no flakiness.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/tts"
)

// blockingSynth is a fake Synthesizer whose CondenseScript blocks until the
// release channel is closed. While blocked it keeps the semaphore slot and the
// WaitGroup counter held, so the test can observe how many jobs are genuinely
// in flight at once. It tracks the current and peak concurrent CondenseScript
// callers with atomics (the only cross-goroutine state it owns).
type blockingSynth struct {
	release   chan struct{} // closed by the test to let every blocked job proceed
	started   chan struct{} // buffered; one token sent per CondenseScript entry
	inFlight  int64         // current concurrent CondenseScript callers (atomic)
	peak      int64         // high-water mark of inFlight (atomic)
	condCalls int64         // total CondenseScript invocations (atomic)
}

func newBlockingSynth(startedCap int) *blockingSynth {
	return &blockingSynth{
		release: make(chan struct{}),
		started: make(chan struct{}, startedCap),
	}
}

func (s *blockingSynth) CondenseScript(ctx context.Context, transcript string, budget int) (tts.Script, error) {
	atomic.AddInt64(&s.condCalls, 1)
	cur := atomic.AddInt64(&s.inFlight, 1)
	// Lock-free monotonic max update.
	for {
		old := atomic.LoadInt64(&s.peak)
		if cur <= old || atomic.CompareAndSwapInt64(&s.peak, old, cur) {
			break
		}
	}
	// Announce arrival (non-blocking; channel is sized to the expected count).
	select {
	case s.started <- struct{}{}:
	default:
	}

	// Hold the job in flight until the test releases it, or the job context is
	// cancelled (shutdown). Either way decrement on exit.
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	atomic.AddInt64(&s.inFlight, -1)

	// Return a valid one-segment script so run() proceeds to CreateSpeech.
	return tts.Script{
		Intro: "",
		Segments: []tts.ScriptSegment{
			{SessionID: "sess-1", Text: "all green, shipping today"},
		},
	}, nil
}

func (s *blockingSynth) CreateSpeech(ctx context.Context, req tts.SpeechRequest) ([]byte, error) {
	// A few bytes is enough; run() only needs non-empty MP3 data to write a file.
	return []byte{0xFF, 0xFB, 0x90, 0x00}, nil
}

// writeStandupFixture writes a minimal valid persisted standup JSON for id into
// the standup dir so LoadPersistedStandup + BuildSpeechInputs yield non-empty
// speakable content (required for run() to reach CondenseScript).
func writeStandupFixture(t *testing.T, id string) {
	t.Helper()
	ps := office.PersistedStandup{
		ID:           id,
		Participants: []string{"sess-1"},
		Transcript: []office.StandupMessage{
			{
				SessionID:   "sess-1",
				SpeakerName: "Alice",
				Type:        "status_report",
				Content:     "Finished the migration and started on the audit.",
			},
		},
		ParticipantCount: 1,
		RespondedCount:   1,
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal standup fixture: %v", err)
	}
	path := filepath.Join(office.StandupDir(), id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write standup fixture %s: %v", path, err)
	}
}

// newTestManager builds an AudioJobManager wired to the blocking fake synth,
// a no-op concurrency-safe broadcast, and a constant voiceFor. It points HOME
// at a fresh temp dir so office.StandupDir() resolves to clean, isolated state.
func newTestManager(t *testing.T, synth *blockingSynth) *AudioJobManager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(office.StandupDir(), 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}

	broadcast := func(event string, payload interface{}) {
		// No-op, but must be safe under concurrent calls from many run()
		// goroutines. Touching the args keeps the compiler honest without
		// introducing shared mutable state.
		_ = event
		_ = payload
	}
	newSynth := func() (Synthesizer, error) { return synth, nil }
	voiceFor := func(sessionID string, index int) string { return "cedar" }

	return NewAudioJobManager(context.Background(), broadcast, newSynth, voiceFor)
}

// TestAudioJobManagerCoalescesSameID hammers Start with the SAME id from many
// goroutines while the single job is held in flight, and asserts the requests
// coalesce to exactly one running job (running map + sem are race-clean and
// only one CondenseScript executes).
func TestAudioJobManagerCoalescesSameID(t *testing.T) {
	synth := newBlockingSynth(64)
	m := newTestManager(t, synth)

	const id = "standup-coalesce"
	writeStandupFixture(t, id)

	const goroutines = 40
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{}) // release all callers at once to widen the race window

	var okCount int64
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			// 50 attempts each => 2000 concurrent Start calls for the same id.
			for j := 0; j < 50; j++ {
				resolved, err := m.Start(id)
				if err == nil && resolved == id {
					atomic.AddInt64(&okCount, 1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	if atomic.LoadInt64(&okCount) == 0 {
		t.Fatalf("expected at least one successful Start for id %q", id)
	}

	// Wait until the single coalesced job has actually entered CondenseScript,
	// so the assertion below observes the in-flight state (not a pre-start race).
	waitForStarted(t, synth, 1)

	if got := atomic.LoadInt64(&synth.inFlight); got != 1 {
		t.Fatalf("coalescing failed: want exactly 1 in-flight job, got %d", got)
	}
	if got := atomic.LoadInt64(&synth.condCalls); got != 1 {
		t.Fatalf("coalescing failed: want exactly 1 CondenseScript call, got %d", got)
	}

	// Release the held job and confirm the WaitGroup drains (no leak/deadlock).
	close(synth.release)
	waitForDrain(t, m)

	if got := atomic.LoadInt64(&synth.inFlight); got != 0 {
		t.Fatalf("after drain want 0 in-flight, got %d", got)
	}
}

// TestAudioJobManagerRespectsCap launches many distinct ids concurrently with
// all jobs held in flight, and asserts the semaphore never lets more than
// maxConcurrentAudioJobs run at once, and that Wait() drains cleanly afterwards
// (no deadlock, no leaked slot or wg counter).
func TestAudioJobManagerRespectsCap(t *testing.T) {
	const distinctIDs = 30
	synth := newBlockingSynth(maxConcurrentAudioJobs + 1)
	m := newTestManager(t, synth)

	ids := make([]string, distinctIDs)
	for i := range ids {
		ids[i] = fmt.Sprintf("standup-cap-%02d", i)
		writeStandupFixture(t, ids[i])
	}

	const writers = 24
	var wg sync.WaitGroup
	wg.Add(writers)
	start := make(chan struct{})

	var accepted int64    // Start returned (id, nil)
	var rejected int64    // Start returned ErrTooManyAudioJobs
	for w := 0; w < writers; w++ {
		writerIdx := w
		go func() {
			defer wg.Done()
			<-start
			// Each writer sweeps every id many times. Because the slots stay
			// held, only the first maxConcurrentAudioJobs distinct ids that win
			// the semaphore are accepted; the rest get ErrTooManyAudioJobs.
			for round := 0; round < 20; round++ {
				id := ids[(writerIdx+round)%distinctIDs]
				resolved, err := m.Start(id)
				switch {
				case err == nil && resolved == id:
					atomic.AddInt64(&accepted, 1)
				case err == ErrTooManyAudioJobs:
					atomic.AddInt64(&rejected, 1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	// The semaphore must never have admitted more than the cap concurrently.
	if peak := atomic.LoadInt64(&synth.peak); peak > int64(maxConcurrentAudioJobs) {
		t.Fatalf("cap violated: peak concurrent jobs %d > cap %d", peak, maxConcurrentAudioJobs)
	}
	// We expect the cap to actually be reached under this load (sanity: the test
	// is exercising the contended path, not trivially passing).
	if peak := atomic.LoadInt64(&synth.peak); peak < 1 {
		t.Fatalf("no jobs ran; test did not exercise the semaphore path")
	}
	// At least one rejection proves ErrTooManyAudioJobs is reachable under load.
	if atomic.LoadInt64(&rejected) == 0 {
		t.Fatalf("expected at least one ErrTooManyAudioJobs rejection under load")
	}

	// Release every held job and confirm full drain (sem + wg do not leak).
	close(synth.release)
	waitForDrain(t, m)

	if got := atomic.LoadInt64(&synth.inFlight); got != 0 {
		t.Fatalf("after drain want 0 in-flight, got %d", got)
	}
	// The semaphore must be fully released: cap successive non-blocking acquires
	// must all succeed, proving no slot leaked.
	for i := 0; i < maxConcurrentAudioJobs; i++ {
		select {
		case m.sem <- struct{}{}:
		default:
			t.Fatalf("semaphore slot leaked: could not re-acquire slot %d/%d after drain", i+1, maxConcurrentAudioJobs)
		}
	}
}

// waitForStarted polls until at least n CondenseScript calls have begun. It uses
// the synth.started channel (deterministic signal), bounded by a generous
// timeout so a regression that deadlocks fails loudly instead of hanging.
func waitForStarted(t *testing.T, s *blockingSynth, n int) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	got := 0
	for got < n {
		select {
		case <-s.started:
			got++
		case <-deadline:
			t.Fatalf("timed out waiting for %d job(s) to start (got %d)", n, got)
		}
	}
}

// waitForDrain asserts m.Wait() returns within a bounded time, surfacing a
// deadlock/leak as a test failure rather than a hung process.
func waitForDrain(t *testing.T, m *AudioJobManager) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		m.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("AudioJobManager.Wait() did not return: jobs leaked or deadlocked")
	}
}
