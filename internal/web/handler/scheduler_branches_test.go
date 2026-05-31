package handler

// Branch fills for the scheduler CRUD handlers: the nil-scheduler 503 guards,
// the ErrNotFound 404 paths on update/delete/run-now, and the create
// validation arms (invalid cron, invalid action).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/scheduler"
)

func TestGetSchedulerJob_NilScheduler503(t *testing.T) {
	ctx := &Context{} // Scheduler nil
	req := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/x", nil), "id", "x")
	rec := httptest.NewRecorder()
	ctx.GetSchedulerJob(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil scheduler, got %d", rec.Code)
	}
}

func TestExecuteOneShotJob_BadConfig(t *testing.T) {
	ctx := newSchedulerCtx(t)
	_, err := executeOneShotJob(context.Background(), ctx, &scheduler.Job{Config: json.RawMessage("{bad")})
	if err == nil {
		t.Fatal("expected error for invalid one_shot config")
	}
}

func TestSchedulerHandlers_NilScheduler503(t *testing.T) {
	ctx := &Context{} // Scheduler + SchedulerRunner nil
	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
	}{
		{"create", ctx.CreateSchedulerJob, "POST"},
		{"update", ctx.UpdateSchedulerJob, "PUT"},
		{"delete", ctx.DeleteSchedulerJob, "DELETE"},
		{"run-now", ctx.RunSchedulerJobNow, "POST"},
		{"history", ctx.GetSchedulerJobHistory, "GET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := withURLParam(httptest.NewRequest(c.method, "/api/scheduler/jobs/x", nil), "id", "x")
			rec := httptest.NewRecorder()
			c.handler(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s: expected 503 with nil scheduler, got %d", c.name, rec.Code)
			}
		})
	}
}

func TestRunSchedulerJobNow_NotFound(t *testing.T) {
	ctx := newSchedulerCtx(t)
	req := withURLParam(httptest.NewRequest("POST", "/api/scheduler/jobs/ghost/run-now", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.RunSchedulerJobNow(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 running unknown job, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSchedulerJob_InvalidCronAndAction(t *testing.T) {
	ctx := newSchedulerCtx(t)
	id := createJob(t, ctx, "j", "0 0 * * *", scheduler.ActionWebhook)

	// Invalid cron on update -> 400.
	reqCron := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id,
		jsonBody(t, map[string]interface{}{"cron": "not-a-cron"})), "id", id)
	reqCron.Header.Set("Content-Type", "application/json")
	recCron := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(recCron, reqCron)
	if recCron.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid cron on update, got %d", recCron.Code)
	}

	// Invalid action on update -> 400.
	reqAction := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id,
		jsonBody(t, map[string]interface{}{"action": "not-an-action"})), "id", id)
	reqAction.Header.Set("Content-Type", "application/json")
	recAction := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(recAction, reqAction)
	if recAction.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid action on update, got %d", recAction.Code)
	}
}
