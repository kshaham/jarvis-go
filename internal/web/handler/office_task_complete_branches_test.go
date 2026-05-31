package handler

// Round-3 branch fills for OfficeAgentTaskComplete sub-branches the existing
// ultrareview tests don't reach:
//   - AlreadyComplete idempotency short-circuit (office.go:2230-2233)
//   - the soft screenshot-warning broadcast for UI-tagged tasks (2244-2253)
//   - needs-review label with NO worktree -> warn-and-skip ultrareview (2277-2280)
//   - next-pending-task immediate re-tasking goroutine (2303-2318)

import (
	"net/http"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

// TestOfficeAgentTaskComplete_AlreadyComplete drives the idempotency arm: a
// second completion of the same task returns {alreadyComplete:true}.
func TestOfficeAgentTaskComplete_AlreadyComplete(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-a", "cfg-a", &office.TeamMemberConfig{ID: "cfg-a", Name: "Ann"})
	agent := ctx.OfficeManager.GetAgent("sess-a")
	agent.AddTask(&office.Task{ID: "t-1", Title: "ship it", Status: office.TaskInProgress})

	rec1 := postJSON(t, ctx.OfficeAgentTaskComplete, "/api/office/agent/task/complete", map[string]interface{}{
		"sessionId": "sess-a", "taskId": "t-1", "result": "first done",
	})
	if rec1.Code != http.StatusOK {
		t.Fatalf("first complete: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	rec2 := postJSON(t, ctx.OfficeAgentTaskComplete, "/api/office/agent/task/complete", map[string]interface{}{
		"sessionId": "sess-a", "taskId": "t-1", "result": "again",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second complete: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	resp := decodeMap(t, rec2)
	if ac, _ := resp["alreadyComplete"].(bool); !ac {
		t.Errorf("expected alreadyComplete=true on repeat completion, got %v", resp)
	}
}

// TestOfficeAgentTaskComplete_ScreenshotWarning drives the soft screenshot-gate
// warning: a UI-tagged title + a summary lacking verification evidence + no
// attached PNG -> office:uiTaskMissingScreenshot broadcast and a `warning` in
// the response.
func TestOfficeAgentTaskComplete_ScreenshotWarning(t *testing.T) {
	ctx := newSnapshotCtx(t)
	recorder := newEventRecorder(ctx.SSEBroker)
	ctx.OfficeManager.RegisterAgent("sess-ui", "cfg-ui", &office.TeamMemberConfig{ID: "cfg-ui", Name: "Uli"})
	agent := ctx.OfficeManager.GetAgent("sess-ui")
	agent.AddTask(&office.Task{ID: "t-ui", Title: "redesign the login screen UI", Status: office.TaskInProgress})

	rec := postJSON(t, ctx.OfficeAgentTaskComplete, "/api/office/agent/task/complete", map[string]interface{}{
		"sessionId": "sess-ui", "taskId": "t-ui", "result": "finished the work",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !recorder.waitFor("office:uiTaskMissingScreenshot", 2*time.Second) {
		t.Fatal("expected office:uiTaskMissingScreenshot broadcast for UI task without evidence")
	}
	resp := decodeMap(t, rec)
	if warn, _ := resp["warning"].(string); warn == "" {
		t.Errorf("expected a screenshot warning in the response, got %v", resp)
	}
}

// TestOfficeAgentTaskComplete_NeedsReviewNoWorktree drives the needs-review
// label arm where the agent has NO worktree path: ultrareview is skipped with a
// warn (office.go:2277-2280) and the completion still returns 200.
func TestOfficeAgentTaskComplete_NeedsReviewNoWorktree(t *testing.T) {
	ctx := newSnapshotCtx(t)
	recorder := newEventRecorder(ctx.SSEBroker)
	ctx.OfficeManager.RegisterAgent("sess-nr", "cfg-nr", &office.TeamMemberConfig{ID: "cfg-nr", Name: "Nora"})
	agent := ctx.OfficeManager.GetAgent("sess-nr")
	// No SetWorktreePath -> GetWorktreePath() == "" -> the no-worktree warn arm.
	agent.AddTask(&office.Task{ID: "t-nr", Title: "fix the thing", Status: office.TaskInProgress, Labels: []string{"needs-review"}})

	rec := postJSON(t, ctx.OfficeAgentTaskComplete, "/api/office/agent/task/complete", map[string]interface{}{
		"sessionId": "sess-nr", "taskId": "t-nr", "result": "done",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// ultrareview must NOT fire without a worktree.
	time.Sleep(250 * time.Millisecond)
	if recorder.total("office:taskReviewed") != 0 {
		t.Errorf("ultrareview should not fire without a worktree, fired %d", recorder.total("office:taskReviewed"))
	}
}

// TestOfficeAgentTaskComplete_NextPendingTaskInjection drives the next-task
// re-tasking goroutine: completing a task while another pending task is queued
// triggers PromptNextTask asynchronously.
func TestOfficeAgentTaskComplete_NextPendingTaskInjection(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-nt", "cfg-nt", &office.TeamMemberConfig{ID: "cfg-nt", Name: "Ned"})
	agent := ctx.OfficeManager.GetAgent("sess-nt")
	agent.AddTask(&office.Task{ID: "t-done", Title: "first", Status: office.TaskInProgress})
	// A queued pending task so NextPendingTask() returns non-nil after completion.
	agent.AddTask(&office.Task{ID: "t-next", Title: "second", Status: office.TaskPending})

	rec := postJSON(t, ctx.OfficeAgentTaskComplete, "/api/office/agent/task/complete", map[string]interface{}{
		"sessionId": "sess-nt", "taskId": "t-done", "result": "done",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The PromptNextTask goroutine runs async; give it a beat to execute its
	// branch (it writes via the stub session store, which no-ops cleanly).
	time.Sleep(200 * time.Millisecond)
}
