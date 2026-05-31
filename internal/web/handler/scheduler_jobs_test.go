package handler

// Coverage for the cron-job scheduler HTTP handlers (scheduler.go) plus the
// executor dispatch helpers. Built against a real scheduler.Store + Runner
// backed by a temp file, so create/update/delete/run-now/history run
// hermetically. The webhook executor is exercised against an httptest server.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/scheduler"
)

func newSchedulerCtx(t *testing.T) *Context {
	t.Helper()
	store := scheduler.NewStore(t.TempDir() + "/jobs.json")
	runner := scheduler.NewRunner(store, func(ctx context.Context, job *scheduler.Job) (string, error) {
		return "ran " + job.Name, nil
	})
	// Stop() drains in-flight run-now goroutines so their async history /
	// store writes finish before the test's TempDir is removed (avoids the
	// "rename: invalid argument" cleanup race).
	t.Cleanup(runner.Stop)
	return &Context{Scheduler: store, SchedulerRunner: runner}
}

// createJob is a small helper that POSTs a valid job and returns its ID.
func createJob(t *testing.T, ctx *Context, name, cron, action string) string {
	t.Helper()
	rec := postJSON(t, ctx.CreateSchedulerJob, "/api/scheduler/jobs", map[string]interface{}{
		"name": name, "cron": cron, "action": action, "enabled": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var j scheduler.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("decode created job: %v", err)
	}
	if j.ID == "" {
		t.Fatal("created job missing ID")
	}
	return j.ID
}

func TestListSchedulerJobs_NilScheduler503(t *testing.T) {
	ctx := &Context{}
	req := httptest.NewRequest("GET", "/api/scheduler/jobs", nil)
	rec := httptest.NewRecorder()
	ctx.ListSchedulerJobs(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when scheduler nil, got %d", rec.Code)
	}
}

func TestSchedulerJobs_FullCRUDCycle(t *testing.T) {
	ctx := newSchedulerCtx(t)

	// Empty list.
	resp := getJSON(t, ctx.ListSchedulerJobs, "/api/scheduler/jobs")
	jobs, _ := resp["jobs"].([]interface{})
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs initially, got %d", len(jobs))
	}

	id := createJob(t, ctx, "nightly", "0 0 * * *", scheduler.ActionWebhook)

	// List now has 1.
	resp2 := getJSON(t, ctx.ListSchedulerJobs, "/api/scheduler/jobs")
	jobs2, _ := resp2["jobs"].([]interface{})
	if len(jobs2) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs2))
	}

	// Get by ID.
	reqGet := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/"+id, nil), "id", id)
	recGet := httptest.NewRecorder()
	ctx.GetSchedulerJob(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get job: expected 200, got %d", recGet.Code)
	}

	// Update the cron.
	reqUpd := withURLParam(
		httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id, jsonBody(t, map[string]interface{}{"cron": "30 9 * * *", "name": "morning", "action": scheduler.ActionWebhook, "enabled": true})),
		"id", id,
	)
	reqUpd.Header.Set("Content-Type", "application/json")
	recUpd := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(recUpd, reqUpd)
	if recUpd.Code != http.StatusOK {
		t.Fatalf("update job: expected 200, got %d: %s", recUpd.Code, recUpd.Body.String())
	}

	// Delete.
	reqDel := withURLParam(httptest.NewRequest("DELETE", "/api/scheduler/jobs/"+id, nil), "id", id)
	recDel := httptest.NewRecorder()
	ctx.DeleteSchedulerJob(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete job: expected 200, got %d", recDel.Code)
	}

	// Get after delete -> 404.
	reqGet2 := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/"+id, nil), "id", id)
	recGet2 := httptest.NewRecorder()
	ctx.GetSchedulerJob(recGet2, reqGet2)
	if recGet2.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", recGet2.Code)
	}
}

func TestGetSchedulerJob_NotFound(t *testing.T) {
	ctx := newSchedulerCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/ghost", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetSchedulerJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateSchedulerJob_Validation(t *testing.T) {
	ctx := newSchedulerCtx(t)

	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing name", map[string]interface{}{"cron": "0 0 * * *", "action": scheduler.ActionWebhook}},
		{"missing cron", map[string]interface{}{"name": "x", "action": scheduler.ActionWebhook}},
		{"bad cron", map[string]interface{}{"name": "x", "cron": "not-a-cron", "action": scheduler.ActionWebhook}},
		{"bad action", map[string]interface{}{"name": "x", "cron": "0 0 * * *", "action": "nuke"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, ctx.CreateSchedulerJob, "/api/scheduler/jobs", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateSchedulerJob_NotFound(t *testing.T) {
	ctx := newSchedulerCtx(t)
	req := withURLParam(
		httptest.NewRequest("PUT", "/api/scheduler/jobs/ghost", jsonBody(t, map[string]interface{}{"name": "x"})),
		"id", "ghost",
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating unknown job, got %d", rec.Code)
	}
}

func TestUpdateSchedulerJob_BadCronAndAction(t *testing.T) {
	ctx := newSchedulerCtx(t)
	id := createJob(t, ctx, "j", "0 0 * * *", scheduler.ActionWebhook)

	// Bad cron.
	reqC := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id, jsonBody(t, map[string]interface{}{"cron": "xx"})), "id", id)
	reqC.Header.Set("Content-Type", "application/json")
	recC := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(recC, reqC)
	if recC.Code != http.StatusBadRequest {
		t.Fatalf("bad cron: expected 400, got %d", recC.Code)
	}

	// Bad action.
	reqA := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/"+id, jsonBody(t, map[string]interface{}{"action": "explode"})), "id", id)
	reqA.Header.Set("Content-Type", "application/json")
	recA := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(recA, reqA)
	if recA.Code != http.StatusBadRequest {
		t.Fatalf("bad action: expected 400, got %d", recA.Code)
	}
}

func TestDeleteSchedulerJob_NotFound(t *testing.T) {
	ctx := newSchedulerCtx(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/scheduler/jobs/ghost", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.DeleteSchedulerJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRunSchedulerJobNow_HappyAndNotFound(t *testing.T) {
	ctx := newSchedulerCtx(t)
	id := createJob(t, ctx, "manual", "0 0 * * *", scheduler.ActionWebhook)

	reqRun := withURLParam(httptest.NewRequest("POST", "/api/scheduler/jobs/"+id+"/run-now", nil), "id", id)
	recRun := httptest.NewRecorder()
	ctx.RunSchedulerJobNow(recRun, reqRun)
	if recRun.Code != http.StatusOK {
		t.Fatalf("run-now: expected 200, got %d: %s", recRun.Code, recRun.Body.String())
	}
	resp := decodeMap(t, recRun)
	if resp["triggered"] != true {
		t.Errorf("triggered=%v, want true", resp["triggered"])
	}

	reqMiss := withURLParam(httptest.NewRequest("POST", "/api/scheduler/jobs/ghost/run-now", nil), "id", "ghost")
	recMiss := httptest.NewRecorder()
	ctx.RunSchedulerJobNow(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("run-now ghost: expected 404, got %d", recMiss.Code)
	}
}

func TestRunSchedulerJobNow_NilRunner503(t *testing.T) {
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("POST", "/api/scheduler/jobs/x/run-now", nil), "id", "x")
	rec := httptest.NewRecorder()
	ctx.RunSchedulerJobNow(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestGetSchedulerJobHistory_RecordsRun(t *testing.T) {
	ctx := newSchedulerCtx(t)
	id := createJob(t, ctx, "hist", "0 0 * * *", scheduler.ActionWebhook)

	// Fire it once so a record exists. RunNow dispatches the executor on a
	// goroutine, so Stop() drains it to guarantee the history write landed
	// before we read it back through the handler.
	if err := ctx.SchedulerRunner.RunNow(id); err != nil {
		t.Fatalf("run now: %v", err)
	}
	ctx.SchedulerRunner.Stop()

	req := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/"+id+"/history?limit=5", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSchedulerJobHistory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history: expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	hist, _ := resp["history"].([]interface{})
	if len(hist) == 0 {
		t.Error("expected at least one history record after run-now")
	}
}

func TestGetSchedulerJobHistory_NilRunner503(t *testing.T) {
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("GET", "/api/scheduler/jobs/x/history", nil), "id", "x")
	rec := httptest.NewRecorder()
	ctx.GetSchedulerJobHistory(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// --- validAction (pure) ---

func TestValidAction(t *testing.T) {
	for _, a := range []string{scheduler.ActionSpawnTeam, scheduler.ActionOneShot, scheduler.ActionWebhook} {
		if !validAction(a) {
			t.Errorf("validAction(%q) should be true", a)
		}
	}
	for _, a := range []string{"", "nope", "spawn"} {
		if validAction(a) {
			t.Errorf("validAction(%q) should be false", a)
		}
	}
}

// --- Executor dispatch ---

func TestBuildSchedulerExecutor_UnknownAction(t *testing.T) {
	ctx := newSchedulerCtx(t)
	exec := BuildSchedulerExecutor(ctx)
	_, err := exec(context.Background(), &scheduler.Job{Action: "mystery"})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown-action error, got %v", err)
	}
}

func TestExecuteWebhookJob_SuccessAnd4xx(t *testing.T) {
	// 200 server.
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer okSrv.Close()

	cfg, _ := json.Marshal(map[string]interface{}{"url": okSrv.URL})
	out, err := executeWebhookJob(context.Background(), &scheduler.Job{Action: scheduler.ActionWebhook, Config: cfg})
	if err != nil {
		t.Fatalf("webhook 200: unexpected err %v", err)
	}
	if !strings.Contains(out, "HTTP 200") {
		t.Errorf("summary should mention HTTP 200, got %q", out)
	}

	// 500 server -> error returned.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer errSrv.Close()
	cfg2, _ := json.Marshal(map[string]interface{}{"url": errSrv.URL})
	_, err2 := executeWebhookJob(context.Background(), &scheduler.Job{Config: cfg2})
	if err2 == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestExecuteWebhookJob_MissingURLAndBadConfig(t *testing.T) {
	// Missing URL.
	cfg, _ := json.Marshal(map[string]interface{}{})
	if _, err := executeWebhookJob(context.Background(), &scheduler.Job{Config: cfg}); err == nil {
		t.Error("expected error for missing url")
	}
	// Malformed config JSON.
	if _, err := executeWebhookJob(context.Background(), &scheduler.Job{Config: json.RawMessage("{bad")}); err == nil {
		t.Error("expected error for malformed config")
	}
}

func TestExecuteOneShotJob_NotSupported(t *testing.T) {
	ctx := newSchedulerCtx(t)
	cfg, _ := json.Marshal(map[string]interface{}{"prompt": "hi", "workingDir": "/tmp"})
	_, err := executeOneShotJob(context.Background(), ctx, &scheduler.Job{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "not yet supported") {
		t.Fatalf("expected not-supported error, got %v", err)
	}
}

func TestExecuteSpawnTeamJob_MissingWorkingDir(t *testing.T) {
	ctx := newSchedulerCtx(t)
	cfg, _ := json.Marshal(map[string]interface{}{"templateId": "t1"})
	_, err := executeSpawnTeamJob(context.Background(), ctx, &scheduler.Job{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "workingDir is required") {
		t.Fatalf("expected workingDir error, got %v", err)
	}
}

func TestExecuteSpawnTeamJob_BadConfig(t *testing.T) {
	ctx := newSchedulerCtx(t)
	_, err := executeSpawnTeamJob(context.Background(), ctx, &scheduler.Job{Config: json.RawMessage("{bad")})
	if err == nil || !strings.Contains(err.Error(), "invalid spawn_team config") {
		t.Fatalf("expected invalid-config error, got %v", err)
	}
}

// --- scheduledResponseWriter ---

func TestScheduledResponseWriter_CapturesStatusAndBody(t *testing.T) {
	w := &scheduledResponseWriter{}
	w.Header().Set("X-Test", "1")
	w.WriteHeader(http.StatusTeapot)
	n, err := w.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if w.status != http.StatusTeapot {
		t.Errorf("status=%d, want 418", w.status)
	}
	if w.body.String() != "hello" {
		t.Errorf("body=%q, want hello", w.body.String())
	}
	if w.Header().Get("X-Test") != "1" {
		t.Error("header not captured")
	}
}
