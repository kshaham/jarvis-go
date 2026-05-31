package handler

// Coverage for the in-memory scheduled-task CRUD handlers in misc.go.
// These read/write ctx.ScheduledTasks under ScheduledTasksMu, so the suite
// also doubles as a concurrency smoke-test under -race.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func newScheduledCtx() *Context {
	return &Context{
		ScheduledTasks: make(map[string]interface{}),
	}
}

func TestListScheduledTasks_Empty(t *testing.T) {
	ctx := newScheduledCtx()
	resp := getJSON(t, ctx.ListScheduledTasks, "/api/scheduled")
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestCreateScheduledTask_AssignsID(t *testing.T) {
	ctx := newScheduledCtx()
	rec := postJSON(t, ctx.CreateScheduledTask, "/api/scheduled", map[string]interface{}{
		"name": "nightly", "cron": "0 0 * * *",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	task, _ := resp["task"].(map[string]interface{})
	if id, _ := task["id"].(string); id == "" {
		t.Error("expected a generated id")
	}
	if len(ctx.ScheduledTasks) != 1 {
		t.Errorf("expected 1 stored task, got %d", len(ctx.ScheduledTasks))
	}
}

func TestCreateScheduledTask_InvalidJSON(t *testing.T) {
	ctx := newScheduledCtx()
	req := httptest.NewRequest("POST", "/api/scheduled", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateScheduledTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetScheduledTask_FoundAndNotFound(t *testing.T) {
	ctx := newScheduledCtx()
	rec := postJSON(t, ctx.CreateScheduledTask, "/api/scheduled", map[string]interface{}{"name": "x"})
	var created map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["task"].(map[string]interface{})["id"].(string)

	reqGet := withURLParam(httptest.NewRequest("GET", "/api/scheduled/"+id, nil), "id", id)
	recGet := httptest.NewRecorder()
	ctx.GetScheduledTask(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recGet.Code)
	}

	reqMiss := withURLParam(httptest.NewRequest("GET", "/api/scheduled/ghost", nil), "id", "ghost")
	recMiss := httptest.NewRecorder()
	ctx.GetScheduledTask(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recMiss.Code)
	}
}

func TestUpdateScheduledTask_MergesAndNotFound(t *testing.T) {
	ctx := newScheduledCtx()
	rec := postJSON(t, ctx.CreateScheduledTask, "/api/scheduled", map[string]interface{}{"name": "x", "cron": "old"})
	var created map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["task"].(map[string]interface{})["id"].(string)

	// Update only cron; name should be preserved by the merge.
	reqUpd := withURLParam(httptest.NewRequest("PUT", "/api/scheduled/"+id, jsonBody(t, map[string]interface{}{"cron": "new"})), "id", id)
	reqUpd.Header.Set("Content-Type", "application/json")
	recUpd := httptest.NewRecorder()
	ctx.UpdateScheduledTask(recUpd, reqUpd)
	if recUpd.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recUpd.Code)
	}
	var updated map[string]interface{}
	_ = json.Unmarshal(recUpd.Body.Bytes(), &updated)
	task, _ := updated["task"].(map[string]interface{})
	if task["cron"] != "new" {
		t.Errorf("cron=%v, want new", task["cron"])
	}
	if task["name"] != "x" {
		t.Errorf("name should be preserved by merge, got %v", task["name"])
	}

	// Not found.
	reqMiss := withURLParam(httptest.NewRequest("PUT", "/api/scheduled/ghost", jsonBody(t, map[string]interface{}{"cron": "x"})), "id", "ghost")
	reqMiss.Header.Set("Content-Type", "application/json")
	recMiss := httptest.NewRecorder()
	ctx.UpdateScheduledTask(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recMiss.Code)
	}
}

func TestDeleteScheduledTask_HappyAndNotFound(t *testing.T) {
	ctx := newScheduledCtx()
	rec := postJSON(t, ctx.CreateScheduledTask, "/api/scheduled", map[string]interface{}{"name": "x"})
	var created map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["task"].(map[string]interface{})["id"].(string)

	reqDel := withURLParam(httptest.NewRequest("DELETE", "/api/scheduled/"+id, nil), "id", id)
	recDel := httptest.NewRecorder()
	ctx.DeleteScheduledTask(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recDel.Code)
	}

	reqMiss := withURLParam(httptest.NewRequest("DELETE", "/api/scheduled/"+id, nil), "id", id)
	recMiss := httptest.NewRecorder()
	ctx.DeleteScheduledTask(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", recMiss.Code)
	}
}

// TestScheduledTasks_ConcurrentCreateList exercises the mutex under -race:
// many goroutines create tasks while others list, so a missing lock would
// trip the race detector or the "concurrent map" runtime panic.
func TestScheduledTasks_ConcurrentCreateList(t *testing.T) {
	ctx := newScheduledCtx()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			postJSON(t, ctx.CreateScheduledTask, "/api/scheduled", map[string]interface{}{"name": "c"})
		}()
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/scheduled", nil)
			rec := httptest.NewRecorder()
			ctx.ListScheduledTasks(rec, req)
		}()
	}
	wg.Wait()
	if len(ctx.ScheduledTasks) != 20 {
		t.Errorf("expected 20 tasks after concurrent creates, got %d", len(ctx.ScheduledTasks))
	}
}
