package handler

// Round-3 branch fills for the scheduler handlers + executor helpers:
//   - UpdateSchedulerJob where the post-update Reschedule fails (an enabled job
//     updated to an empty cron -> schedule() errors) -> 500 (scheduler.go:121-124)
//   - executeWebhookJob: custom headers loop + a transport error (unreachable
//     host) + a NewRequest build error (invalid method)
//   - BuildSchedulerExecutor webhook dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/scheduler"
)

// TestUpdateSchedulerJob_RescheduleFails drives the Reschedule-error 500 arm:
// an existing enabled job updated to an empty cron passes the (skipped) cron
// validation but fails inside SchedulerRunner.Reschedule -> schedule().
func TestUpdateSchedulerJob_RescheduleFails(t *testing.T) {
	t.Parallel()
	ctx := newSchedulerCtx(t)
	id := createJob(t, ctx, "j", "0 0 * * *", scheduler.ActionWebhook)

	// enabled=true + cron="" -> handler skips cron validation (j.Cron == "") but
	// the store keeps the job enabled, so Reschedule -> schedule() sees an empty
	// cron and returns an "invalid cron" error -> 500.
	req := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id,
		jsonBody(t, map[string]interface{}{"name": "j", "cron": "", "action": scheduler.ActionWebhook, "enabled": true})),
		"id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when reschedule fails on empty cron, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestExecuteWebhookJob_HeadersAndTransportError drives the headers loop plus
// the client.Do transport-error arm (an unroutable host).
func TestExecuteWebhookJob_HeadersAndTransportError(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(map[string]interface{}{
		"url":     "http://127.0.0.1:1/never-listens", // port 1: connection refused
		"method":  "POST",
		"headers": map[string]string{"X-Custom": "abc"},
		"body":    "ping",
	})
	out, err := executeWebhookJob(context.Background(), &scheduler.Job{
		Action: scheduler.ActionWebhook, Config: cfg,
	})
	if err == nil {
		t.Fatalf("expected a transport error hitting an unroutable host, got out=%q", out)
	}
}

// TestExecuteWebhookJob_NewRequestError drives the http.NewRequestWithContext
// build-error arm via an invalid HTTP method token.
func TestExecuteWebhookJob_NewRequestError(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(map[string]interface{}{
		"url":    "http://example.com",
		"method": "BAD METHOD WITH SPACES", // invalid method -> NewRequest errors
		"body":   "x",
	})
	_, err := executeWebhookJob(context.Background(), &scheduler.Job{
		Action: scheduler.ActionWebhook, Config: cfg,
	})
	if err == nil {
		t.Fatal("expected a build-request error for an invalid HTTP method")
	}
}

// TestExecuteWebhookJob_HappyWithHeaders confirms a 200 response path with
// headers set returns a nil error and a summary (covers the success tail).
func TestExecuteWebhookJob_HappyWithHeaders(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg, _ := json.Marshal(map[string]interface{}{
		"url":     srv.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Custom": "abc"},
		"body":    "ping",
	})
	out, err := executeWebhookJob(context.Background(), &scheduler.Job{
		Action: scheduler.ActionWebhook, Config: cfg,
	})
	if err != nil {
		t.Fatalf("expected success, got err=%v", err)
	}
	if out == "" {
		t.Error("expected a non-empty summary")
	}
	if gotHeader != "abc" {
		t.Errorf("custom header not forwarded, got %q", gotHeader)
	}
}

// TestBuildSchedulerExecutor_DispatchesWebhook confirms the ActionWebhook arm of
// the executor switch routes to executeWebhookJob.
func TestBuildSchedulerExecutor_DispatchesWebhook(t *testing.T) {
	t.Parallel()
	ctx := newSchedulerCtx(t)
	exec := BuildSchedulerExecutor(ctx)
	// Missing url -> executeWebhookJob's "url is required" error proves dispatch.
	cfg, _ := json.Marshal(map[string]interface{}{"method": "POST"})
	_, err := exec(context.Background(), &scheduler.Job{Action: scheduler.ActionWebhook, Config: cfg})
	if err == nil {
		t.Fatal("expected webhook dispatch to return the url-required error")
	}
}
