package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"github.com/claudeman/jarvis-go/internal/scheduler"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// webhookTimeout bounds outbound HTTP calls for webhook-type jobs. 30s is
// generous for a healthcheck / status post without letting a stuck remote
// tie up the executor worker indefinitely.
const webhookTimeout = 30 * time.Second

// ---- HTTP handlers ----

// ListSchedulerJobs returns every persisted job. GET /api/scheduler/jobs
func (ctx *Context) ListSchedulerJobs(w http.ResponseWriter, r *http.Request) {
	if ctx.Scheduler == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": ctx.Scheduler.List(),
	})
}

// GetSchedulerJob returns a single job. GET /api/scheduler/jobs/{id}
func (ctx *Context) GetSchedulerJob(w http.ResponseWriter, r *http.Request) {
	if ctx.Scheduler == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	j := ctx.Scheduler.Get(chi.URLParam(r, "id"))
	if j == nil {
		request.WriteError(w, http.StatusNotFound, "job not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, j)
}

// CreateSchedulerJob validates the cron expression and persists a new job.
// POST /api/scheduler/jobs
func (ctx *Context) CreateSchedulerJob(w http.ResponseWriter, r *http.Request) {
	if ctx.Scheduler == nil || ctx.SchedulerRunner == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	var j scheduler.Job
	if !request.DecodeAndValidate(w, r, &j) {
		return
	}
	if j.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if j.Cron == "" {
		request.WriteError(w, http.StatusBadRequest, "cron is required")
		return
	}
	if _, err := cron.ParseStandard(j.Cron); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid cron expression: "+err.Error())
		return
	}
	if !validAction(j.Action) {
		request.WriteError(w, http.StatusBadRequest, "action must be one of spawn_team, one_shot, webhook")
		return
	}
	created, err := ctx.Scheduler.Create(j)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ctx.SchedulerRunner.Reschedule(created.ID); err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusCreated, created)
}

// UpdateSchedulerJob replaces a job's fields. PUT /api/scheduler/jobs/{id}
func (ctx *Context) UpdateSchedulerJob(w http.ResponseWriter, r *http.Request) {
	if ctx.Scheduler == nil || ctx.SchedulerRunner == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	id := chi.URLParam(r, "id")
	var j scheduler.Job
	if !request.DecodeAndValidate(w, r, &j) {
		return
	}
	if j.Cron != "" {
		if _, err := cron.ParseStandard(j.Cron); err != nil {
			request.WriteError(w, http.StatusBadRequest, "invalid cron expression: "+err.Error())
			return
		}
	}
	if j.Action != "" && !validAction(j.Action) {
		request.WriteError(w, http.StatusBadRequest, "action must be one of spawn_team, one_shot, webhook")
		return
	}
	updated, err := ctx.Scheduler.Update(id, j)
	if err != nil {
		if err == scheduler.ErrNotFound {
			request.WriteError(w, http.StatusNotFound, "job not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ctx.SchedulerRunner.Reschedule(id); err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, updated)
}

// DeleteSchedulerJob removes a job. DELETE /api/scheduler/jobs/{id}
func (ctx *Context) DeleteSchedulerJob(w http.ResponseWriter, r *http.Request) {
	if ctx.Scheduler == nil || ctx.SchedulerRunner == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	id := chi.URLParam(r, "id")
	if err := ctx.Scheduler.Delete(id); err != nil {
		if err == scheduler.ErrNotFound {
			request.WriteError(w, http.StatusNotFound, "job not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = ctx.SchedulerRunner.Reschedule(id)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// RunSchedulerJobNow fires a job out-of-band for testing.
// POST /api/scheduler/jobs/{id}/run-now
func (ctx *Context) RunSchedulerJobNow(w http.ResponseWriter, r *http.Request) {
	if ctx.SchedulerRunner == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	if err := ctx.SchedulerRunner.RunNow(chi.URLParam(r, "id")); err != nil {
		if err == scheduler.ErrNotFound {
			request.WriteError(w, http.StatusNotFound, "job not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"triggered": true})
}

// GetSchedulerJobHistory returns the N most recent run records for a job.
// GET /api/scheduler/jobs/{id}/history?limit=N
func (ctx *Context) GetSchedulerJobHistory(w http.ResponseWriter, r *http.Request) {
	if ctx.SchedulerRunner == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	records := ctx.SchedulerRunner.History(chi.URLParam(r, "id"), limit)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"history": records,
	})
}

// validAction whitelists the built-in action types.
func validAction(a string) bool {
	switch a {
	case scheduler.ActionSpawnTeam, scheduler.ActionOneShot, scheduler.ActionWebhook:
		return true
	}
	return false
}

// ---- Executor dispatch ----

// BuildSchedulerExecutor returns an ExecutorFunc closed over the handler
// Context. Wired up at server startup and passed into scheduler.NewRunner.
// The spending-freeze check is enforced for spawn_team / one_shot so
// scheduled work can't bypass cost caps.
func BuildSchedulerExecutor(ctx *Context) scheduler.ExecutorFunc {
	return func(execCtx context.Context, job *scheduler.Job) (string, error) {
		switch job.Action {
		case scheduler.ActionSpawnTeam:
			return executeSpawnTeamJob(execCtx, ctx, job)
		case scheduler.ActionOneShot:
			return executeOneShotJob(execCtx, ctx, job)
		case scheduler.ActionWebhook:
			return executeWebhookJob(execCtx, job)
		default:
			return "", fmt.Errorf("unknown action: %s", job.Action)
		}
	}
}

func executeSpawnTeamJob(execCtx context.Context, ctx *Context, job *scheduler.Job) (string, error) {
	var cfg spawnTeamRequest
	if err := json.Unmarshal(job.Config, &cfg); err != nil {
		return "", fmt.Errorf("invalid spawn_team config: %w", err)
	}
	if cfg.WorkingDir == "" {
		return "", fmt.Errorf("workingDir is required")
	}
	resolvedDir, err := ctx.ResolveWorkingDir(cfg.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("invalid workingDir: %w", err)
	}
	cfg.WorkingDir = resolvedDir
	// Reuse the existing spawn flow. The handler writes directly to w; we
	// pipe into an ignoring ResponseWriter so the output goes only into the
	// job's return value below.
	recorder := &scheduledResponseWriter{}
	ctx.spawnTeamFromRequest(recorder, cfg)
	if recorder.status >= 400 {
		return recorder.body.String(), fmt.Errorf("spawn_team failed: HTTP %d", recorder.status)
	}
	return fmt.Sprintf("spawned team: %s", cfg.TemplateID), nil
}

func executeOneShotJob(execCtx context.Context, ctx *Context, job *scheduler.Job) (string, error) {
	var cfg oneShotRequest
	if err := json.Unmarshal(job.Config, &cfg); err != nil {
		return "", fmt.Errorf("invalid one_shot config: %w", err)
	}
	// We can't call OneShotSession directly because it's an HTTP handler;
	// but the effective work (create session, send prompt, wait idle, stop)
	// is small enough to mirror here. For this round, require users to hit
	// /api/sessions/one-shot via a webhook job instead if they want ad-hoc
	// prompts on a schedule. Return an informative error.
	return "", fmt.Errorf("one_shot action not yet supported by scheduler; use a webhook job targeting /api/sessions/one-shot")
}

func executeWebhookJob(execCtx context.Context, job *scheduler.Job) (string, error) {
	var cfg struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(job.Config, &cfg); err != nil {
		return "", fmt.Errorf("invalid webhook config: %w", err)
	}
	if cfg.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	reqCtx, cancel := context.WithTimeout(execCtx, webhookTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, method, cfg.URL, bytes.NewBufferString(cfg.Body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	client := &http.Client{Timeout: webhookTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()
	// Cap captured body so a gigantic response doesn't bloat the history file.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	summary := fmt.Sprintf("%s %s -> HTTP %d", method, cfg.URL, resp.StatusCode)
	if resp.StatusCode >= 400 {
		return summary + "\n" + string(respBody), fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return summary, nil
}

// scheduledResponseWriter is a minimal http.ResponseWriter used to capture
// the output of an in-process handler call without routing through a real
// HTTP stack. Only the scheduler's spawn_team path uses this.
type scheduledResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *scheduledResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *scheduledResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *scheduledResponseWriter) WriteHeader(code int)        { w.status = code }
