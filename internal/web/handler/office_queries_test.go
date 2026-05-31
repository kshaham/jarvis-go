package handler

// Coverage for the pure state-reading + simple CRUD office.go handlers
// that only depend on OfficeManager (built by newTimelineContext). Each
// asserts the 200 status and the documented JSON envelope, plus the
// validation 400s on the POST/PUT mutators.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// getJSON fires a GET handler and returns the decoded map (asserting 200).
func getJSON(t *testing.T, h func(http.ResponseWriter, *http.Request), path string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s decode: %v", path, err)
	}
	return resp
}

func TestOfficeStatus_Shape(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeStatus, "/api/office/status")
	// GetOfficeState always reports the spawned flag.
	if _, ok := resp["teamSpawned"]; !ok {
		t.Error("expected teamSpawned key in office state")
	}
}

func TestOfficeHealth_Healthy(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeHealth, "/api/office/health")
	if resp["status"] != "healthy" {
		t.Errorf("status=%v, want healthy", resp["status"])
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("expected timestamp")
	}
}

func TestOfficeGetWhiteboard_EmptyThenNote(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	resp := getJSON(t, ctx.OfficeGetWhiteboard, "/api/office/whiteboard")
	notes, _ := resp["notes"].(map[string]interface{})
	if len(notes) != 0 {
		t.Errorf("expected empty whiteboard, got %d notes", len(notes))
	}

	// Post a note, then re-read.
	rec := postJSON(t, ctx.OfficePostWhiteboard, "/api/office/whiteboard", map[string]interface{}{
		"sessionId": "s-vex",
		"text":      "ship it",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("post note: %d", rec.Code)
	}
	resp2 := getJSON(t, ctx.OfficeGetWhiteboard, "/api/office/whiteboard")
	notes2, _ := resp2["notes"].(map[string]interface{})
	if len(notes2) != 1 {
		t.Errorf("expected 1 note after post, got %d", len(notes2))
	}
}

func TestOfficePostWhiteboard_MissingFields(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficePostWhiteboard, "/api/office/whiteboard", map[string]interface{}{
		"sessionId": "s-vex",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing text, got %d", rec.Code)
	}
}

func TestOfficePostWhiteboard_TooLong(t *testing.T) {
	ctx := newTimelineContext(t)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	rec := postJSON(t, ctx.OfficePostWhiteboard, "/api/office/whiteboard", map[string]interface{}{
		"sessionId": "s-vex",
		"text":      string(long),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-length note, got %d", rec.Code)
	}
}

func TestOfficeGetTaskQueues_WithAgent(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t-1", Title: "x", Status: office.TaskPending})

	resp := getJSON(t, ctx.OfficeGetTaskQueues, "/api/office/task-queues")
	queues, _ := resp["queues"].([]interface{})
	if len(queues) != 1 {
		t.Fatalf("expected 1 queue, got %d", len(queues))
	}
}

func TestOfficeGetTaskQueueBySession_FoundAndNotFound(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// Found.
	req := withURLParam(httptest.NewRequest("GET", "/api/office/task-queues/s-vex", nil), "sessionId", "s-vex")
	rec := httptest.NewRecorder()
	ctx.OfficeGetTaskQueueBySession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Not found.
	req2 := withURLParam(httptest.NewRequest("GET", "/api/office/task-queues/ghost", nil), "sessionId", "ghost")
	rec2 := httptest.NewRecorder()
	ctx.OfficeGetTaskQueueBySession(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestOfficeGetBugTasks_AddThenList(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetBugTasks, "/api/office/bugs")
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) != 0 {
		t.Errorf("expected no bug tasks initially, got %d", len(tasks))
	}

	rec := postJSON(t, ctx.OfficePostBugTask, "/api/office/bugs", map[string]interface{}{
		"reportedBy":  "vex",
		"assignedTo":  "bolt",
		"file":        "auth.go",
		"description": "nil deref",
		"priority":    "high",
		"category":    "crash",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("post bug: %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	task, _ := created["task"].(map[string]interface{})
	if task["status"] != "open" {
		t.Errorf("new bug status=%v, want open", task["status"])
	}

	resp2 := getJSON(t, ctx.OfficeGetBugTasks, "/api/office/bugs")
	tasks2, _ := resp2["tasks"].([]interface{})
	if len(tasks2) != 1 {
		t.Errorf("expected 1 bug task, got %d", len(tasks2))
	}
}

func TestOfficePostBugTask_MissingFields(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficePostBugTask, "/api/office/bugs", map[string]interface{}{
		"reportedBy": "vex",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for incomplete bug, got %d", rec.Code)
	}
}

func TestOfficePutBugTask_Resolve(t *testing.T) {
	ctx := newTimelineContext(t)
	// Seed via POST so there's an ID to update.
	postJSON(t, ctx.OfficePostBugTask, "/api/office/bugs", map[string]interface{}{
		"reportedBy": "vex", "assignedTo": "bolt", "file": "a.go",
		"description": "x", "priority": "low", "category": "bug",
	})
	id := ctx.OfficeManager.GetBugTasks()[0].ID

	req := withURLParam(httptest.NewRequest("PUT", "/api/office/bugs/"+id, jsonBody(t, map[string]interface{}{"status": "resolved"})), "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutBugTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeGetFileLocks_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetFileLocks, "/api/office/locks")
	locks, _ := resp["locks"].([]interface{})
	if len(locks) != 0 {
		t.Errorf("expected no locks, got %d", len(locks))
	}
}

func TestOfficeGetMessages_AfterSend(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	rec := postJSON(t, ctx.OfficePostMessage, "/api/office/messages", map[string]interface{}{
		"fromSessionId": "s-vex",
		"toSessionId":   "s-bolt",
		"content":       "hi",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("post message: %d", rec.Code)
	}
	resp := getJSON(t, ctx.OfficeGetMessages, "/api/office/messages")
	msgs, _ := resp["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestOfficePostMessage_Validation(t *testing.T) {
	ctx := newTimelineContext(t)
	// Missing content.
	rec := postJSON(t, ctx.OfficePostMessage, "/api/office/messages", map[string]interface{}{
		"fromSessionId": "s-vex", "toSessionId": "s-bolt",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing content, got %d", rec.Code)
	}
}

func TestOfficeGetMetrics_Shape(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetMetrics, "/api/office/metrics")
	if _, ok := resp["metrics"]; !ok {
		t.Error("expected metrics key")
	}
}

func TestOfficeGetScores_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetScores, "/api/office/scores")
	if _, ok := resp["scores"]; !ok {
		t.Error("expected scores key")
	}
}

func TestOfficeGetAuditLog_Shape(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetAuditLog, "/api/office/audit")
	if _, ok := resp["entries"]; !ok {
		t.Error("expected entries key")
	}
}

func TestOfficeGetMeetings_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetMeetings, "/api/office/meetings")
	if _, ok := resp["meetings"]; !ok {
		t.Error("expected meetings key")
	}
}

func TestOfficeGetSettings_Shape(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetSettings, "/api/office/settings")
	if _, ok := resp["teamSpawned"]; !ok {
		t.Error("expected teamSpawned in office settings")
	}
}

func TestOfficeGetAutoStandup_Defaults(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetAutoStandup, "/api/office/auto-standup")
	if _, ok := resp["enabled"]; !ok {
		t.Error("expected enabled key")
	}
	if _, ok := resp["intervalMs"]; !ok {
		t.Error("expected intervalMs key")
	}
}

func TestOfficePutAutoStandup_EnablesAndReports(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficePutAutoStandup, "/api/office/auto-standup", map[string]interface{}{
		"enabled":  true,
		"interval": 120000,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp["enabled"])
	}
	if got, _ := resp["intervalMs"].(float64); int(got) != 120000 {
		t.Errorf("intervalMs=%v, want 120000", resp["intervalMs"])
	}
	if !ctx.OfficeManager.IsAutoStandupEnabled() {
		t.Error("manager should report auto-standup enabled")
	}
}

func TestOfficeGetTelegramConfig_Shape(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeGetTelegramConfig, "/api/office/telegram")
	// Without a TelegramHandler, connected must be false.
	if resp["telegramConnected"] != false {
		t.Errorf("expected telegramConnected=false, got %v", resp["telegramConnected"])
	}
}

func TestOfficePutTelegramConfig_RejectsEnableWhenDisconnected(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficePutTelegramConfig, "/api/office/telegram", map[string]interface{}{
		"enabled":     true,
		"intervalMin": 30,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 enabling telegram while disconnected, got %d", rec.Code)
	}
}

func TestOfficePutTelegramConfig_DisableOK(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficePutTelegramConfig, "/api/office/telegram", map[string]interface{}{
		"enabled":     false,
		"intervalMin": 5, // below floor; handler clamps to 10
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 disabling telegram, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got, _ := resp["intervalMin"].(float64); int(got) != 10 {
		t.Errorf("intervalMin should clamp to 10, got %v", resp["intervalMin"])
	}
}

func TestOfficeAgentTeamRoster_Lists(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Role: "scout", Tier: "worker"})
	resp := getJSON(t, ctx.OfficeAgentTeamRoster, "/api/office/agent/roster")
	roster, _ := resp["roster"].([]interface{})
	if len(roster) != 1 {
		t.Fatalf("expected 1 roster entry, got %d", len(roster))
	}
	entry := roster[0].(map[string]interface{})
	if entry["name"] != "Vex" || entry["role"] != "scout" {
		t.Errorf("unexpected roster entry: %+v", entry)
	}
}

func TestOfficeAgentMessage_Validation(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentMessage, "/api/office/agent/message", map[string]interface{}{
		"fromSessionId": "s-vex",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d", rec.Code)
	}
}

func TestOfficeAgentGetTasks_UnknownReturnsEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/tasks?sessionId=s-nobody", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty) for unknown agent, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) != 0 {
		t.Errorf("expected empty tasks for unknown agent, got %d", len(tasks))
	}
}

func TestOfficeAgentListQuestions_MissingSession(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/questions", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentListQuestions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing sessionId, got %d", rec.Code)
	}
}
