package handler

// Coverage for the agent-auth task-lifecycle handlers in office.go:
// start / ack / progress / self-add / triage / criteria, plus the
// agent file-lock endpoints. All reachable with a seeded OfficeManager.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// seedAgentWithTask registers one worker agent holding a single in-progress
// task and returns the context for handler calls.
func seedAgentWithTask(t *testing.T, taskID string, status office.TaskStatus) *Context {
	t.Helper()
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: taskID, Title: "build thing", Status: status})
	return ctx
}

func TestOfficeAgentTaskStart_UpdatesStatus(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskPending)
	rec := postJSON(t, ctx.OfficeAgentTaskStart, "/api/office/agent/task/start", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "t-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := ctx.OfficeManager.GetAgent("s-vex").GetTasks()[0].Status; got != office.TaskInProgress {
		t.Errorf("status=%v, want in_progress", got)
	}
}

func TestOfficeAgentTaskStart_WrappingUpConflict(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskPending)
	ctx.OfficeManager.SetWrappingUp(true)
	rec := postJSON(t, ctx.OfficeAgentTaskStart, "/api/office/agent/task/start", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "t-1",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 while wrapping up, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskStart_UnknownAgentNoop(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentTaskStart, "/api/office/agent/task/start", map[string]interface{}{
		"sessionId": "s-nobody", "taskId": "t-1",
	})
	// Unknown agents are tolerated: handler no-ops and returns 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskAck_UpdatesStatus(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskPending)
	rec := postJSON(t, ctx.OfficeAgentTaskAck, "/api/office/agent/task/ack", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "t-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := ctx.OfficeManager.GetAgent("s-vex").GetTasks()[0].Status; got != office.TaskAcknowledged {
		t.Errorf("status=%v, want acknowledged", got)
	}
}

func TestOfficeAgentTaskProgress_SetsProgress(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskInProgress)
	rec := postJSON(t, ctx.OfficeAgentTaskProgress, "/api/office/agent/task/progress", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "t-1", "progress": "halfway",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// SetTaskProgress records the note in ProgressLog and mirrors it into
	// the task Description.
	task := ctx.OfficeManager.GetAgent("s-vex").GetTasks()[0]
	if len(task.ProgressLog) == 0 || task.ProgressLog[len(task.ProgressLog)-1].Note != "halfway" {
		t.Errorf("expected last progress log note 'halfway', got %+v", task.ProgressLog)
	}
}

func TestOfficeAgentTaskProgress_Blocked(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskInProgress)
	rec := postJSON(t, ctx.OfficeAgentTaskProgress, "/api/office/agent/task/progress", map[string]interface{}{
		"sessionId":          "s-vex",
		"taskId":             "t-1",
		"isBlocked":          true,
		"blockerDescription": "waiting on API key",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	task := ctx.OfficeManager.GetAgent("s-vex").GetTasks()[0]
	if task.Status != office.TaskBlocked {
		t.Errorf("status=%v, want blocked", task.Status)
	}
	if len(task.ProgressLog) == 0 || task.ProgressLog[len(task.ProgressLog)-1].Note != "waiting on API key" {
		t.Errorf("expected blocker description in progress log, got %+v", task.ProgressLog)
	}
}

func TestOfficeAgentTaskSelfAdd_CreatesTask(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": "s-vex",
		"title":     "refactor auth",
		"priority":  "high",
		"labels":    []string{"Auth"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["taskId"] == nil || resp["taskId"] == "" {
		t.Error("expected a taskId in response")
	}
	tasks := ctx.OfficeManager.GetAgent("s-vex").GetTasks()
	if len(tasks) != 1 || tasks[0].Title != "refactor auth" {
		t.Errorf("self-added task missing: %+v", tasks)
	}
	// Label should be normalized to lowercase.
	if len(tasks[0].Labels) != 1 || tasks[0].Labels[0] != "auth" {
		t.Errorf("labels=%v, want [auth]", tasks[0].Labels)
	}
}

func TestOfficeAgentTaskSelfAdd_InvalidLabel(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": "s-vex",
		"title":     "x",
		"labels":    []string{"has spaces"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid label, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskSelfAdd_WrappingUpConflict(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.SetWrappingUp(true)
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": "s-vex", "title": "x",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskTriage_MovesTask(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t-1", Title: "x", Status: office.TaskPending})

	rec := postJSON(t, ctx.OfficeAgentTaskTriage, "/api/office/agent/task/triage", map[string]interface{}{
		"sessionId":   "s-vex",
		"toSessionId": "s-bolt",
		"taskId":      "t-1",
		"reason":      "better fit",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(ctx.OfficeManager.GetAgent("s-vex").GetTasks()) != 0 {
		t.Error("task should have left vex's queue")
	}
	if len(ctx.OfficeManager.GetAgent("s-bolt").GetTasks()) != 1 {
		t.Error("task should be in bolt's queue")
	}
}

func TestOfficeAgentTaskTriage_UnknownTarget(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t-1", Status: office.TaskPending})
	rec := postJSON(t, ctx.OfficeAgentTaskTriage, "/api/office/agent/task/triage", map[string]interface{}{
		"sessionId": "s-vex", "toSessionId": "s-ghost", "taskId": "t-1",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown target, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskCriteria_AddAndUnknownAgent(t *testing.T) {
	ctx := seedAgentWithTask(t, "t-1", office.TaskInProgress)
	rec := postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria", map[string]interface{}{
		"sessionId": "s-vex",
		"taskId":    "t-1",
		"op":        "add",
		"criteria":  []string{"build passes"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	crit, _ := resp["criteria"].([]interface{})
	if len(crit) != 1 {
		t.Errorf("expected 1 criterion, got %d", len(crit))
	}

	// Unknown agent -> 404.
	rec2 := postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria", map[string]interface{}{
		"sessionId": "s-nobody", "taskId": "t-1", "op": "add", "criteria": []string{"x"},
	})
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d", rec2.Code)
	}
}

func TestOfficeAgentFileLock_LockUnlock(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// Acquire.
	rec := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex", "filePath": "src/auth.go", "action": "lock",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("lock: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(ctx.OfficeManager.GetLocks()) != 1 {
		t.Errorf("expected 1 active lock, got %d", len(ctx.OfficeManager.GetLocks()))
	}

	// Release.
	rec2 := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex", "filePath": "src/auth.go", "action": "unlock",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("unlock: expected 200, got %d", rec2.Code)
	}
	if len(ctx.OfficeManager.GetLocks()) != 0 {
		t.Errorf("expected lock released, got %d", len(ctx.OfficeManager.GetLocks()))
	}
}

func TestOfficeAgentFileLock_MissingFields(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d", rec.Code)
	}
}

func TestOfficeAgentFileLock_PathTraversalRejected(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex", "filePath": "../../etc/passwd", "action": "lock",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d", rec.Code)
	}
}

func TestOfficeAgentFileLock_Conflict(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})
	// vex acquires first.
	postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex", "filePath": "shared.go", "action": "lock",
	})
	// bolt tries the same file -> 409.
	rec := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-bolt", "filePath": "shared.go", "action": "lock",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for contended lock, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentFileLock_BadAction(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentFileLock, "/api/office/agent/file-lock", map[string]interface{}{
		"sessionId": "s-vex", "filePath": "a.go", "action": "frobnicate",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown action, got %d", rec.Code)
	}
}

func TestOfficeAgentGetFileLocks_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/file-locks", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetFileLocks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentGetDevInstance_DefaultSlot(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/dev-instance", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetDevInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	// No dev server running: empty url, slot defaults to "app".
	if resp["name"] != "app" {
		t.Errorf("name=%v, want app", resp["name"])
	}
	if resp["credsSet"] != false {
		t.Errorf("credsSet=%v, want false", resp["credsSet"])
	}
}

func TestSlotFromQuery(t *testing.T) {
	t.Parallel()
	// Empty -> "app".
	req := httptest.NewRequest("GET", "/x", nil)
	if got := slotFromQuery(req); got != "app" {
		t.Errorf("slotFromQuery(empty)=%q, want app", got)
	}
	// Explicit name.
	req2 := httptest.NewRequest("GET", "/x?name=worker", nil)
	if got := slotFromQuery(req2); got != "worker" {
		t.Errorf("slotFromQuery=%q, want worker", got)
	}
}
