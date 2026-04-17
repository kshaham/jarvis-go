package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// historyPerJob caps how many past runs we remember per job. Memory-bound;
// long-running jobs don't balloon over time.
const historyPerJob = 20

// Runner owns a cron schedule and dispatches jobs via the configured
// Executor. All public methods are safe for concurrent use.
type Runner struct {
	store    *Store
	cron     *cron.Cron
	executor ExecutorFunc

	mu        sync.Mutex
	entryByID map[string]cron.EntryID
	history   map[string][]RunRecord

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRunner constructs the runner but does not start the cron scheduler.
// Call Start() to begin firing jobs.
func NewRunner(store *Store, exec ExecutorFunc) *Runner {
	return &Runner{
		store:     store,
		cron:      cron.New(cron.WithSeconds()),
		executor:  exec,
		entryByID: make(map[string]cron.EntryID),
		history:   make(map[string][]RunRecord),
	}
}

// Start begins scheduling. Reschedules any persisted jobs and fires any that
// were due while the server was offline (once per missed job, not full
// history replay).
func (r *Runner) Start(ctx context.Context) {
	r.ctx, r.cancel = context.WithCancel(ctx)
	for _, j := range r.store.List() {
		if err := r.schedule(j); err != nil {
			slog.Warn("scheduler: could not schedule job", "id", j.ID, "name", j.Name, "error", err)
		}
		// Fire any job whose NextRunAt is in the past.
		if j.Enabled && j.NextRunAt != nil && j.NextRunAt.Before(time.Now()) {
			slog.Warn("scheduler: catching up missed run", "name", j.Name, "missedAt", j.NextRunAt)
			r.runJobByID(j.ID)
		}
	}
	r.cron.Start()
}

// Stop halts the cron scheduler and waits up to 10s for in-flight jobs.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	stopCtx := r.cron.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(10 * time.Second):
		slog.Warn("scheduler: jobs did not finish within 10s")
	}
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// Reschedule removes and re-adds a job. Called by handlers after Create /
// Update / Delete so the in-process cron matches the persisted state.
func (r *Runner) Reschedule(id string) error {
	r.unschedule(id)
	j := r.store.Get(id)
	if j == nil {
		return nil // deletion
	}
	return r.schedule(j)
}

// RunNow fires a job out-of-band. Useful for manual testing.
func (r *Runner) RunNow(id string) error {
	if r.store.Get(id) == nil {
		return ErrNotFound
	}
	r.runJobByID(id)
	return nil
}

// History returns the most recent run records for a job, newest-first.
func (r *Runner) History(id string, limit int) []RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := r.history[id]
	if limit <= 0 || limit > len(records) {
		limit = len(records)
	}
	out := make([]RunRecord, limit)
	// records are appended oldest-first; reverse.
	for i := 0; i < limit; i++ {
		out[i] = records[len(records)-1-i]
	}
	return out
}

// ---- internals ----

func (r *Runner) schedule(j *Job) error {
	if !j.Enabled {
		return nil
	}
	schedule, err := cron.ParseStandard(j.Cron)
	if err != nil {
		// Fall back to the 6-field format (with seconds) if standard fails.
		schedule, err = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(j.Cron)
		if err != nil {
			return fmt.Errorf("invalid cron %q: %w", j.Cron, err)
		}
	}
	entryID, err := r.cron.AddFunc(j.Cron, func() { r.runJobByID(j.ID) })
	if err != nil {
		// robfig cron returns err for non-6-field expressions when WithSeconds
		// is set; fall back to AddJob with the parsed schedule.
		entryID = r.cron.Schedule(schedule, cron.FuncJob(func() { r.runJobByID(j.ID) }))
	}
	r.mu.Lock()
	r.entryByID[j.ID] = entryID
	r.mu.Unlock()
	r.store.SetNextRun(j.ID, schedule.Next(time.Now()))
	return nil
}

func (r *Runner) unschedule(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if eid, ok := r.entryByID[id]; ok {
		r.cron.Remove(eid)
		delete(r.entryByID, id)
	}
}

// runJobByID loads the job, runs the executor, and records the outcome.
// The whole body is wrapped so a panicking executor doesn't kill the
// scheduler goroutine.
func (r *Runner) runJobByID(id string) {
	j := r.store.Get(id)
	if j == nil || !j.Enabled {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("scheduler: executor panicked", "job", j.Name, "panic", rec)
				r.appendHistory(id, RunRecord{
					StartedAt: time.Now(), EndedAt: time.Now(),
					Status: "error", Error: fmt.Sprintf("panic: %v", rec),
				})
			}
		}()

		started := time.Now()
		execCtx, cancel := context.WithTimeout(r.ctx, 15*time.Minute)
		defer cancel()

		output, err := r.runExecutor(execCtx, j)
		ended := time.Now()
		status := "ok"
		errStr := ""
		if err != nil {
			status = "error"
			errStr = err.Error()
		}
		r.store.UpdateRunStatus(id, started, status, errStr)
		r.appendHistory(id, RunRecord{
			StartedAt: started, EndedAt: ended,
			Status: status, Error: errStr, Output: output,
		})
		// Refresh NextRunAt for display.
		r.mu.Lock()
		eid, ok := r.entryByID[id]
		r.mu.Unlock()
		if ok {
			next := r.cron.Entry(eid).Next
			if !next.IsZero() {
				r.store.SetNextRun(id, next)
			}
		}
	}()
}

func (r *Runner) runExecutor(ctx context.Context, j *Job) (string, error) {
	if r.executor == nil {
		return "", errors.New("no executor configured")
	}
	return r.executor(ctx, j)
}

func (r *Runner) appendHistory(id string, rec RunRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.history[id]
	list = append(list, rec)
	if len(list) > historyPerJob {
		list = list[len(list)-historyPerJob:]
	}
	r.history[id] = list
}
