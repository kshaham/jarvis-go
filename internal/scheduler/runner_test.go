package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestStore returns a Store backed by a temp file unique to the test.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "sched.json"))
}

// spyExecutor records every invocation and lets a test block on completion.
type spyExecutor struct {
	mu      sync.Mutex
	calls   []string // job IDs in call order
	output  string
	err     error
	delay   time.Duration
	panicOn bool
	done    chan struct{} // closed-once-buffered signal per call
	calledN int32
}

func newSpy() *spyExecutor {
	return &spyExecutor{done: make(chan struct{}, 64)}
}

func (s *spyExecutor) fn(ctx context.Context, job *Job) (string, error) {
	atomic.AddInt32(&s.calledN, 1)
	s.mu.Lock()
	s.calls = append(s.calls, job.ID)
	delay := s.delay
	out := s.output
	err := s.err
	doPanic := s.panicOn
	s.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			// signal completion even on cancel so waiters don't hang
			s.signal()
			return "", ctx.Err()
		}
	}
	if doPanic {
		// signal before panic so the deferred recover path is still observed via history
		s.signal()
		panic("boom")
	}
	s.signal()
	return out, err
}

func (s *spyExecutor) signal() {
	select {
	case s.done <- struct{}{}:
	default:
	}
}

func (s *spyExecutor) callCount() int { return int(atomic.LoadInt32(&s.calledN)) }

func (s *spyExecutor) lastCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

// waitHistory polls the runner's History until it has at least n records or
// times out. Deterministic enough for tests (the executor goroutine appends
// history right before it returns) without relying on a fixed sleep.
func waitHistory(t *testing.T, r *Runner, id string, n int) []RunRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recs := r.History(id, 0)
		if len(recs) >= n {
			return recs
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d history records (have %d)", n, len(r.History(id, 0)))
	return nil
}

func TestNewRunner_Initializes(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	r := NewRunner(store, spy.fn)
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
	if r.store != store {
		t.Fatal("store not wired")
	}
	if r.cron == nil {
		t.Fatal("cron not constructed")
	}
	if r.entryByID == nil || r.history == nil {
		t.Fatal("maps not initialized")
	}
	// History on an unknown job is empty, not nil-panic.
	if got := r.History("nope", 10); len(got) != 0 {
		t.Fatalf("expected empty history, got %v", got)
	}
}

func TestRunNow_NotFound(t *testing.T) {
	r := NewRunner(newTestStore(t), newSpy().fn)
	if err := r.RunNow("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRunNow_Success_RecordsHistoryAndStatus(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	spy.output = "hello-output"
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "ok-job", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})

	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	recs := waitHistory(t, r, job.ID, 1)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Status != "ok" {
		t.Fatalf("expected status ok, got %q (err=%q)", rec.Status, rec.Error)
	}
	if rec.Output != "hello-output" {
		t.Fatalf("expected output propagated, got %q", rec.Output)
	}
	if rec.StartedAt.IsZero() || rec.EndedAt.IsZero() {
		t.Fatal("expected timestamps set")
	}
	if spy.callCount() != 1 {
		t.Fatalf("expected executor called once, got %d", spy.callCount())
	}
	// Persisted run status reflects success.
	persisted := store.Get(job.ID)
	if persisted.LastStatus != "ok" {
		t.Fatalf("expected persisted LastStatus=ok, got %q", persisted.LastStatus)
	}
	if persisted.LastRunAt == nil {
		t.Fatal("expected LastRunAt persisted")
	}
	if persisted.LastError != "" {
		t.Fatalf("expected no LastError, got %q", persisted.LastError)
	}
}

func TestRunNow_ExecutorError_RecordsError(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	spy.err = errors.New("kaboom")
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "err-job", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Status != "error" {
		t.Fatalf("expected status error, got %q", recs[0].Status)
	}
	if recs[0].Error != "kaboom" {
		t.Fatalf("expected error message, got %q", recs[0].Error)
	}
	persisted := store.Get(job.ID)
	if persisted.LastStatus != "error" || persisted.LastError != "kaboom" {
		t.Fatalf("persisted status/error wrong: %q / %q", persisted.LastStatus, persisted.LastError)
	}
}

func TestRunJobByID_DisabledJob_NoRun(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "disabled", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: false})
	// RunNow should succeed (job exists) but runJobByID early-returns on !Enabled.
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// Give any goroutine a beat; none should have started.
	time.Sleep(30 * time.Millisecond)
	if spy.callCount() != 0 {
		t.Fatalf("disabled job should not execute, got %d calls", spy.callCount())
	}
	if len(r.History(job.ID, 0)) != 0 {
		t.Fatal("disabled job should record no history")
	}
}

func TestRunExecutor_NilExecutor_Error(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, nil) // no executor configured
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "no-exec", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Status != "error" {
		t.Fatalf("expected error status with nil executor, got %q", recs[0].Status)
	}
	if recs[0].Error != "no executor configured" {
		t.Fatalf("expected 'no executor configured', got %q", recs[0].Error)
	}
}

func TestRunJobByID_PanicRecovered(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	spy.panicOn = true
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "panicky", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Status != "error" {
		t.Fatalf("expected error after panic, got %q", recs[0].Status)
	}
	if recs[0].Error == "" || recs[0].Error[:6] != "panic:" {
		t.Fatalf("expected panic-prefixed error, got %q", recs[0].Error)
	}
}

func TestRunNow_BeforeStart_UsesBackgroundContext(t *testing.T) {
	// Runner.ctx is nil before Start(); runJobByID must fall back to Background.
	store := newTestStore(t)
	spy := newSpy()
	spy.output = "pre-start"
	r := NewRunner(store, spy.fn)

	job, _ := store.Create(Job{Name: "pre", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Status != "ok" || recs[0].Output != "pre-start" {
		t.Fatalf("expected ok run before Start, got %+v", recs[0])
	}
	// Cleanly drain the goroutine.
	r.Stop()
}

func TestHistory_OrderingAndLimit(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "multi", Cron: "0 0 * * *", Action: ActionWebhook, Enabled: true})

	// Fire 3 sequential runs, each waiting for completion so ordering is stable.
	for i := 0; i < 3; i++ {
		spy.mu.Lock()
		spy.output = string(rune('A' + i))
		spy.mu.Unlock()
		if err := r.RunNow(job.ID); err != nil {
			t.Fatalf("RunNow %d: %v", i, err)
		}
		waitHistory(t, r, job.ID, i+1)
	}

	all := r.History(job.ID, 0) // limit<=0 => all
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}
	// Newest-first: last appended output should be first.
	if all[0].Output != "C" {
		t.Fatalf("expected newest-first ordering, got %q first", all[0].Output)
	}
	if all[2].Output != "A" {
		t.Fatalf("expected oldest last, got %q", all[2].Output)
	}

	// Limit larger than available is clamped.
	if got := r.History(job.ID, 100); len(got) != 3 {
		t.Fatalf("oversized limit should clamp to 3, got %d", len(got))
	}
	// Limit smaller returns newest N.
	limited := r.History(job.ID, 1)
	if len(limited) != 1 || limited[0].Output != "C" {
		t.Fatalf("limit=1 should return newest only, got %+v", limited)
	}
}

func TestAppendHistory_BoundedToCap(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	// Directly drive appendHistory past the cap; deterministic, no goroutines.
	for i := 0; i < historyPerJob+5; i++ {
		r.appendHistory("job-x", RunRecord{Status: "ok", Output: string(rune('a' + (i % 26)))})
	}
	recs := r.History("job-x", 0)
	if len(recs) != historyPerJob {
		t.Fatalf("expected history capped at %d, got %d", historyPerJob, len(recs))
	}
}

func TestSchedule_DisabledJob_NoEntry(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	job := &Job{ID: "d1", Name: "disabled", Cron: "* * * * *", Enabled: false}
	if err := r.schedule(job); err != nil {
		t.Fatalf("schedule disabled should be no-op nil, got %v", err)
	}
	r.mu.Lock()
	_, ok := r.entryByID[job.ID]
	r.mu.Unlock()
	if ok {
		t.Fatal("disabled job should not get a cron entry")
	}
}

func TestSchedule_InvalidCron_Error(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	job := &Job{ID: "bad", Name: "bad-cron", Cron: "not a cron", Enabled: true}
	err := r.schedule(job)
	if err == nil {
		t.Fatal("expected error for invalid cron")
	}
}

func TestSchedule_StandardCron_SetsNextRun(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	r.cron.Start()
	defer r.cron.Stop()
	job, _ := store.Create(Job{Name: "std", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})
	if err := r.schedule(job); err != nil {
		t.Fatalf("schedule standard cron: %v", err)
	}
	r.mu.Lock()
	_, ok := r.entryByID[job.ID]
	r.mu.Unlock()
	if !ok {
		t.Fatal("expected entry registered")
	}
	persisted := store.Get(job.ID)
	if persisted.NextRunAt == nil {
		t.Fatal("expected NextRunAt set after schedule")
	}
	if !persisted.NextRunAt.After(time.Now()) {
		t.Fatalf("expected NextRunAt in the future, got %v", persisted.NextRunAt)
	}
}

func TestSchedule_SecondsCron_FallbackParser(t *testing.T) {
	// A 6-field (seconds) cron exercises the fallback parse + AddFunc path.
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	r.cron.Start()
	defer r.cron.Stop()
	job, _ := store.Create(Job{Name: "sec", Cron: "*/5 * * * * *", Action: ActionWebhook, Enabled: true})
	if err := r.schedule(job); err != nil {
		t.Fatalf("schedule seconds cron: %v", err)
	}
	r.mu.Lock()
	_, ok := r.entryByID[job.ID]
	r.mu.Unlock()
	if !ok {
		t.Fatal("expected entry registered for seconds cron")
	}
}

func TestReschedule_NewAndUpdatedAndDeleted(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	r.cron.Start()
	defer r.cron.Stop()

	job, _ := store.Create(Job{Name: "resched", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})

	// First reschedule: registers entry.
	if err := r.Reschedule(job.ID); err != nil {
		t.Fatalf("reschedule (create): %v", err)
	}
	r.mu.Lock()
	eid1, ok1 := r.entryByID[job.ID]
	r.mu.Unlock()
	if !ok1 {
		t.Fatal("expected entry after first reschedule")
	}

	// Second reschedule after an update: old entry removed, new one added.
	if _, err := store.Update(job.ID, Job{Name: "resched", Cron: "0 10 * * *", Action: ActionWebhook, Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := r.Reschedule(job.ID); err != nil {
		t.Fatalf("reschedule (update): %v", err)
	}
	r.mu.Lock()
	eid2 := r.entryByID[job.ID]
	r.mu.Unlock()
	if eid1 == eid2 {
		t.Fatal("expected a fresh entry id after reschedule")
	}

	// Delete then reschedule: returns nil and clears the entry.
	if err := store.Delete(job.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.Reschedule(job.ID); err != nil {
		t.Fatalf("reschedule (delete) should be nil, got %v", err)
	}
	r.mu.Lock()
	_, stillThere := r.entryByID[job.ID]
	r.mu.Unlock()
	if stillThere {
		t.Fatal("expected entry removed after delete+reschedule")
	}
}

func TestReschedule_InvalidCron_PropagatesError(t *testing.T) {
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	job, _ := store.Create(Job{Name: "bad", Cron: "totally-invalid", Action: ActionWebhook, Enabled: true})
	if err := r.Reschedule(job.ID); err == nil {
		t.Fatal("expected error from invalid cron on reschedule")
	}
}

func TestStart_ReschedulesPersistedAndCatchesUpMissed(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	spy.output = "caught-up"
	r := NewRunner(store, spy.fn)

	// A job whose NextRunAt is in the past should fire once on Start (catch-up).
	past := time.Now().Add(-time.Hour)
	job, _ := store.Create(Job{Name: "missed", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})
	store.SetNextRun(job.ID, past)

	// A disabled job and a future-scheduled job should NOT catch up.
	disabled, _ := store.Create(Job{Name: "off", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: false})
	store.SetNextRun(disabled.ID, past)

	future, _ := store.Create(Job{Name: "future", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})
	store.SetNextRun(future.ID, time.Now().Add(time.Hour))

	r.Start(context.Background())
	defer r.Stop()

	// Only the missed-and-enabled job should have a run recorded.
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Output != "caught-up" {
		t.Fatalf("expected catch-up run, got %+v", recs[0])
	}

	// Disabled job: no run.
	time.Sleep(30 * time.Millisecond)
	if len(r.History(disabled.ID, 0)) != 0 {
		t.Fatal("disabled missed job should not catch up")
	}
	// Future job: no run.
	if len(r.History(future.ID, 0)) != 0 {
		t.Fatal("future job should not catch up")
	}
}

func TestStart_SchedulesBadCronWithoutCrashing(t *testing.T) {
	// Start should log-and-continue when a persisted job has a bad cron.
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	store.Create(Job{Name: "good", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})
	store.Create(Job{Name: "bad", Cron: "garbage", Action: ActionWebhook, Enabled: true})
	// Should not panic.
	r.Start(context.Background())
	r.Stop()
}

func TestStop_BeforeStart_NoPanic(t *testing.T) {
	// Stop with cancel==nil must not panic.
	store := newTestStore(t)
	r := NewRunner(store, newSpy().fn)
	r.cron.Start() // cron started but ctx/cancel never set
	r.Stop()
}

func TestStop_PreventsFurtherRuns(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	job, _ := store.Create(Job{Name: "stopme", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})
	r.Stop()

	// RunNow after Stop: job exists so returns nil, but runJobByID no-ops.
	if err := r.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow after stop returned err: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if spy.callCount() != 0 {
		t.Fatalf("no runs should fire after Stop, got %d", spy.callCount())
	}
}

func TestCronFires_RunsJob(t *testing.T) {
	// Exercise the real cron firing path with a seconds-granular spec.
	store := newTestStore(t)
	spy := newSpy()
	spy.output = "fired"
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "tick", Cron: "* * * * * *", Action: ActionWebhook, Enabled: true})
	if err := r.Reschedule(job.ID); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	// Wait for at least one organic fire (within ~2s).
	recs := waitHistory(t, r, job.ID, 1)
	if recs[0].Output != "fired" {
		t.Fatalf("expected cron-fired run, got %+v", recs[0])
	}
	// NextRunAt should be refreshed to a future time after the run.
	persisted := store.Get(job.ID)
	if persisted.NextRunAt == nil {
		t.Fatal("expected NextRunAt refreshed after organic run")
	}
}

func TestConcurrentRunNow_RaceSafe(t *testing.T) {
	store := newTestStore(t)
	spy := newSpy()
	spy.output = "x"
	r := NewRunner(store, spy.fn)
	r.Start(context.Background())
	defer r.Stop()

	job, _ := store.Create(Job{Name: "concurrent", Cron: "0 9 * * *", Action: ActionWebhook, Enabled: true})

	var wg sync.WaitGroup
	const n = 20
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.RunNow(job.ID)
			_ = r.History(job.ID, 5)
		}()
	}
	wg.Wait()
	// Drain: all fired runs append history. Wait for them to settle.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if spy.callCount() >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if spy.callCount() != n {
		t.Fatalf("expected %d executions, got %d", n, spy.callCount())
	}
	// History is bounded by historyPerJob.
	if got := len(r.History(job.ID, 0)); got > historyPerJob {
		t.Fatalf("history exceeded cap: %d", got)
	}
}
