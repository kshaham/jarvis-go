package main

import (
	"net/http"
	"strings"
	"testing"
)

// officeSessionID reads JARVIS_SESSION_ID; every office handler errors without it.
func TestOfficeSessionID_RequiredEnv(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "")
	if _, err := officeSessionID(); err == nil {
		t.Fatal("expected error when JARVIS_SESSION_ID unset")
	}
	setEnv(t, "JARVIS_SESSION_ID", "sid-42")
	id, err := officeSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sid-42" {
		t.Fatalf("expected sid-42, got %s", id)
	}
}

// allOfficeHandlersErrorWithoutSession verifies each office handler short-circuits
// when no session context is present.
func TestOfficeHandlers_MissingSessionContext(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "")
	handlers := map[string]ToolHandler{
		"office_task_list":         handleOfficeTaskList,
		"office_task_self_add":     handleOfficeTaskSelfAdd,
		"office_task_ack":          handleOfficeTaskAck,
		"office_task_start":        handleOfficeTaskStart,
		"office_task_complete":     handleOfficeTaskComplete,
		"office_task_progress":     handleOfficeTaskProgress,
		"office_task_blocked":      handleOfficeTaskBlocked,
		"office_task_set_criteria": handleOfficeTaskSetCriteria,
		"office_standup_respond":   handleOfficeStandupRespond,
		"office_reflect":           handleOfficeReflect,
		"office_team_roster":       handleOfficeTeamRoster,
		"office_send_message":      handleOfficeSendMessage,
		"office_memory_read":       handleOfficeMemoryRead,
		"office_memory_write":      handleOfficeMemoryWrite,
		"office_memory_append":     handleOfficeMemoryAppend,
	}
	for name, fn := range handlers {
		res := fn(map[string]interface{}{})
		if !res.IsError || !strings.Contains(resultText(res), "JARVIS_SESSION_ID not set") {
			t.Errorf("%s: expected session-context error, got %+v", name, res)
		}
	}
}

func TestHandleOfficeTaskList(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	res := handleOfficeTaskList(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/tasks" || !strings.Contains(rec.Query, "sessionId=sid") {
		t.Fatalf("unexpected request path=%s query=%s", rec.Path, rec.Query)
	}
}

func TestHandleOfficeTaskSelfAdd_Validation(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskSelfAdd(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "title is required") {
		t.Fatal("expected title required")
	}
	if res := handleOfficeTaskSelfAdd(map[string]interface{}{"title": "t"}); !res.IsError ||
		!strings.Contains(resultText(res), "description is required") {
		t.Fatal("expected description required")
	}
}

func TestHandleOfficeTaskSelfAdd_WithCriteria(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"id":"t1"}`))
	res := handleOfficeTaskSelfAdd(map[string]interface{}{
		"title":              "Build feature",
		"description":        "Do the thing",
		"priority":           "high",
		"acceptanceCriteria": []interface{}{"builds", "tests pass"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/task/self-add" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"sessionId":"sid"`, `"title":"Build feature"`, `"priority":"high"`,
		`"acceptanceCriteria":["builds","tests pass"]`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleOfficeTaskSelfAdd_DefaultPriority(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleOfficeTaskSelfAdd(map[string]interface{}{"title": "t", "description": "d"})
	if !strings.Contains(cap.get().Body, `"priority":"medium"`) {
		t.Fatalf("expected default priority medium, got %s", cap.get().Body)
	}
}

func TestHandleOfficeTaskSelfAdd_NonStringCriteria(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	// A server is needed only if it reaches the request; the validation should
	// fail before that, but set one up defensively.
	withServer(t, recordingHandler(&captureRequest{}, 200, `{}`))
	res := handleOfficeTaskSelfAdd(map[string]interface{}{
		"title": "t", "description": "d",
		"acceptanceCriteria": []interface{}{"ok", 123},
	})
	if !res.IsError || !strings.Contains(resultText(res), "acceptanceCriteria[1] must be a string") {
		t.Fatalf("expected non-string criteria error, got %+v", res)
	}
}

func TestHandleOfficeTaskSelfAdd_EmptyCriteriaOmitted(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	// Empty slice -> no acceptanceCriteria field.
	handleOfficeTaskSelfAdd(map[string]interface{}{
		"title": "t", "description": "d", "acceptanceCriteria": []interface{}{},
	})
	if strings.Contains(cap.get().Body, "acceptanceCriteria") {
		t.Fatalf("empty criteria should be omitted, got %s", cap.get().Body)
	}
}

func TestHandleOfficeTaskSelfAdd_CriteriaWrongType(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	// acceptanceCriteria not a slice -> silently ignored (the type assertion fails).
	handleOfficeTaskSelfAdd(map[string]interface{}{
		"title": "t", "description": "d", "acceptanceCriteria": "notaslice",
	})
	if strings.Contains(cap.get().Body, "acceptanceCriteria") {
		t.Fatalf("non-slice criteria should be ignored, got %s", cap.get().Body)
	}
}

func TestHandleOfficeTaskAck(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskAck(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "taskId is required") {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeTaskAck(map[string]interface{}{"taskId": "t1"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/task-ack" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"sessionId":"sid"`) || !strings.Contains(rec.Body, `"taskId":"t1"`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

func TestHandleOfficeTaskStart(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskStart(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleOfficeTaskStart(map[string]interface{}{"taskId": "t1"})
	if cap.get().Path != "/api/office/agent/task-start" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleOfficeTaskComplete(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskComplete(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleOfficeTaskComplete(map[string]interface{}{"taskId": "t1"})
	if cap.get().Path != "/api/office/agent/task-complete" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleOfficeTaskProgress(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskProgress(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeTaskProgress(map[string]interface{}{"taskId": "t1", "note": "making progress"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/task-progress" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	// The handler maps "note" to the server's "progress" field.
	if !strings.Contains(rec.Body, `"progress":"making progress"`) {
		t.Fatalf("expected progress field, got %s", rec.Body)
	}
}

func TestHandleOfficeTaskBlocked(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskBlocked(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeTaskBlocked(map[string]interface{}{"taskId": "t1", "reason": "waiting on API"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/task-progress" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"isBlocked":true`, `"blockerDescription":"waiting on API"`,
		`"progress":"waiting on API"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleOfficeTaskSetCriteria(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeTaskSetCriteria(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "taskId is required") {
		t.Fatal("expected taskId required")
	}
	if res := handleOfficeTaskSetCriteria(map[string]interface{}{"taskId": "t"}); !res.IsError ||
		!strings.Contains(resultText(res), "op is required") {
		t.Fatal("expected op required")
	}
	if res := handleOfficeTaskSetCriteria(map[string]interface{}{"taskId": "t", "op": "set"}); !res.IsError ||
		!strings.Contains(resultText(res), "criteria is required") {
		t.Fatal("expected criteria required")
	}
	// criteria wrong type
	if res := handleOfficeTaskSetCriteria(map[string]interface{}{
		"taskId": "t", "op": "set", "criteria": "notarray",
	}); !res.IsError || !strings.Contains(resultText(res), "criteria must be an array") {
		t.Fatal("expected criteria array-type error")
	}
	// criteria element wrong type
	if res := handleOfficeTaskSetCriteria(map[string]interface{}{
		"taskId": "t", "op": "set", "criteria": []interface{}{"ok", 5},
	}); !res.IsError || !strings.Contains(resultText(res), "criteria[1] must be a string") {
		t.Fatal("expected criteria element error")
	}
}

func TestHandleOfficeTaskSetCriteria_Success(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeTaskSetCriteria(map[string]interface{}{
		"taskId": "t1", "op": "add", "criteria": []interface{}{"one", "two"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/task/criteria" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"op":"add"`, `"criteria":["one","two"]`, `"taskId":"t1"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleOfficeStandupRespond(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeStandupRespond(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected content required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeStandupRespond(map[string]interface{}{"content": "did stuff"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/standup/response" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"content":"did stuff"`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

func TestHandleOfficeReflect(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeReflect(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected learning required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeReflect(map[string]interface{}{"learning": "learned X", "category": "pattern"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/reflection/response" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"category":"pattern"`) {
		t.Fatalf("expected category pattern, got %s", rec.Body)
	}
}

func TestHandleOfficeReflect_DefaultCategory(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleOfficeReflect(map[string]interface{}{"learning": "x"})
	if !strings.Contains(cap.get().Body, `"category":"insight"`) {
		t.Fatalf("expected default category insight, got %s", cap.get().Body)
	}
}

func TestHandleOfficeTeamRoster(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	res := handleOfficeTeamRoster(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/team-roster" || !strings.Contains(rec.Query, "sessionId=sid") {
		t.Fatalf("unexpected request path=%s query=%s", rec.Path, rec.Query)
	}
}

func TestHandleOfficeSendMessage(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeSendMessage(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "toSessionId is required") {
		t.Fatal("expected toSessionId required")
	}
	if res := handleOfficeSendMessage(map[string]interface{}{"toSessionId": "x"}); !res.IsError ||
		!strings.Contains(resultText(res), "content is required") {
		t.Fatal("expected content required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeSendMessage(map[string]interface{}{"toSessionId": "other", "content": "hi"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/message" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"fromSessionId":"sid"`, `"toSessionId":"other"`, `"content":"hi"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleOfficeMemoryRead(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"content":"mem"}`))
	res := handleOfficeMemoryRead(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/memory" || !strings.Contains(rec.Query, "sessionId=sid") {
		t.Fatalf("unexpected request path=%s query=%s", rec.Path, rec.Query)
	}
}

func TestHandleOfficeMemoryWrite(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeMemoryWrite(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected content required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeMemoryWrite(map[string]interface{}{"content": "new memory"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Method != http.MethodPost || rec.Path != "/api/office/agent/memory" {
		t.Fatalf("unexpected %s %s", rec.Method, rec.Path)
	}
	if !strings.Contains(rec.Body, `"content":"new memory"`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

func TestHandleOfficeMemoryAppend(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	if res := handleOfficeMemoryAppend(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected entry required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeMemoryAppend(map[string]interface{}{"entry": "log line"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/memory/append" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"entry":"log line"`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

// --- Upstream error propagation for a representative office handler ---

func TestHandleOfficeTaskList_UpstreamError(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	withServer(t, recordingHandler(&captureRequest{}, 500, "down"))
	res := handleOfficeTaskList(nil)
	if !res.IsError {
		t.Fatal("expected upstream error to surface")
	}
}

func TestHandleOfficeTaskAck_UpstreamError(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")
	withServer(t, recordingHandler(&captureRequest{}, 500, "down"))
	res := handleOfficeTaskAck(map[string]interface{}{"taskId": "t1"})
	if !res.IsError {
		t.Fatal("expected upstream error to surface")
	}
}

// --- Project invariants handlers ---

func TestHandleOfficeProjectInvariantsGet(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `["rule1","rule2"]`))
	res := handleOfficeProjectInvariantsGet(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/office/project/invariants" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleOfficeProjectInvariantsGet_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "x"))
	if res := handleOfficeProjectInvariantsGet(nil); !res.IsError {
		t.Fatal("expected error")
	}
}

func TestHandleOfficeProjectInvariantsSet_Validation(t *testing.T) {
	if res := handleOfficeProjectInvariantsSet(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "invariants is required") {
		t.Fatal("expected invariants required")
	}
	if res := handleOfficeProjectInvariantsSet(map[string]interface{}{"invariants": "notarray"}); !res.IsError ||
		!strings.Contains(resultText(res), "invariants must be an array") {
		t.Fatal("expected array-type error")
	}
	if res := handleOfficeProjectInvariantsSet(map[string]interface{}{
		"invariants": []interface{}{"ok", 99},
	}); !res.IsError || !strings.Contains(resultText(res), "invariants[1] must be a string") {
		t.Fatal("expected element-type error")
	}
}

func TestHandleOfficeProjectInvariantsSet_Success(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"ok":true}`))
	res := handleOfficeProjectInvariantsSet(map[string]interface{}{
		"invariants": []interface{}{"never force push", "always run tests"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Method != http.MethodPut || rec.Path != "/api/office/project/invariants" {
		t.Fatalf("unexpected %s %s", rec.Method, rec.Path)
	}
	if !strings.Contains(rec.Body, `"invariants":["never force push","always run tests"]`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

func TestHandleOfficeProjectInvariantsSet_EmptyArray(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleOfficeProjectInvariantsSet(map[string]interface{}{"invariants": []interface{}{}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(cap.get().Body, `"invariants":[]`) {
		t.Fatalf("expected empty invariants array, got %s", cap.get().Body)
	}
}
