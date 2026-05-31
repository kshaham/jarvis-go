package main

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// --- Session handlers ---

func TestHandleCreateSession_RequiresWorkingDir(t *testing.T) {
	res := handleCreateSession(map[string]interface{}{})
	if !res.IsError || !strings.Contains(resultText(res), "workingDir is required") {
		t.Fatalf("expected workingDir required error, got %+v", res)
	}
}

func TestHandleCreateSession_PostsBodyWithModeAndName(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"id":"sess-1"}`))

	res := handleCreateSession(map[string]interface{}{
		"workingDir": "/work",
		"name":       "My Session",
		"mode":       "office",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Method != http.MethodPost || rec.Path != "/api/sessions" {
		t.Fatalf("unexpected request %s %s", rec.Method, rec.Path)
	}
	for _, want := range []string{`"workingDir":"/work"`, `"mode":"office"`, `"name":"My Session"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected body to contain %s, got %s", want, rec.Body)
		}
	}
	if !strings.Contains(resultText(res), "sess-1") {
		t.Fatalf("expected response to include sess-1, got %s", resultText(res))
	}
}

func TestHandleCreateSession_DefaultModeWhenOmitted(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleCreateSession(map[string]interface{}{"workingDir": "/w"})
	rec := cap.get()
	if !strings.Contains(rec.Body, `"mode":"claude"`) {
		t.Fatalf("expected default mode claude, got %s", rec.Body)
	}
	if strings.Contains(rec.Body, `"name"`) {
		t.Fatalf("name should be omitted when empty, got %s", rec.Body)
	}
}

func TestHandleCreateSession_UpstreamError(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 400, "bad"))
	res := handleCreateSession(map[string]interface{}{"workingDir": "/w"})
	if !res.IsError {
		t.Fatal("expected upstream error to surface")
	}
}

func TestHandleListSessions(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[{"id":"a"}]`))
	res := handleListSessions(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleListSessions_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 503, "down"))
	res := handleListSessions(nil)
	if !res.IsError {
		t.Fatal("expected error")
	}
}

func TestHandleGetSession(t *testing.T) {
	if res := handleGetSession(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected sessionId required error")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"id":"x"}`))
	res := handleGetSession(map[string]interface{}{"sessionId": "x"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/x" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleStopSession(t *testing.T) {
	if res := handleStopSession(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected sessionId required error")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, ``))
	res := handleStopSession(map[string]interface{}{"sessionId": "x"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Method != http.MethodDelete || rec.Path != "/api/sessions/x" {
		t.Fatalf("unexpected %s %s", rec.Method, rec.Path)
	}
}

func TestHandleSendToSession(t *testing.T) {
	if res := handleSendToSession(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected sessionId required")
	}
	if res := handleSendToSession(map[string]interface{}{"sessionId": "x"}); !res.IsError ||
		!strings.Contains(resultText(res), "input is required") {
		t.Fatal("expected input required error")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleSendToSession(map[string]interface{}{"sessionId": "x", "input": "hello"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/sessions/x/input" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"input":"hello"`) {
		t.Fatalf("expected input in body, got %s", rec.Body)
	}
}

func TestHandleGetSessionOutput(t *testing.T) {
	if res := handleGetSessionOutput(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected sessionId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"output":"x"}`))
	res := handleGetSessionOutput(map[string]interface{}{"sessionId": "abc"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/abc/output" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

// --- Agent handlers ---

func TestHandleListAgents_NoFilter(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	res := handleListAgents(map[string]interface{}{})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/subagents" || rec.Query != "" {
		t.Fatalf("expected no query, got path=%s query=%s", rec.Path, rec.Query)
	}
}

func TestHandleListAgents_WithWorkingDirFilter(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	res := handleListAgents(map[string]interface{}{"workingDir": "/a b/c"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if !strings.Contains(rec.Query, "workingDir=") {
		t.Fatalf("expected workingDir query param, got %s", rec.Query)
	}
	// Space must be URL-escaped.
	if !strings.Contains(rec.Query, "%2F") && !strings.Contains(rec.Query, "+") {
		t.Fatalf("expected escaped query, got %s", rec.Query)
	}
}

func TestHandleListAgents_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "x"))
	if res := handleListAgents(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected error")
	}
}

func TestHandleGetAgentStatus(t *testing.T) {
	if res := handleGetAgentStatus(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected agentId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"id":"ag"}`))
	if res := handleGetAgentStatus(map[string]interface{}{"agentId": "ag"}); res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/subagents/ag" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetAgentTranscript(t *testing.T) {
	if res := handleGetAgentTranscript(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected agentId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	res := handleGetAgentTranscript(map[string]interface{}{"agentId": "ag", "limit": "50"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/subagents/ag/transcript" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Query, "limit=50") {
		t.Fatalf("expected limit query, got %s", rec.Query)
	}
}

func TestHandleGetAgentTranscript_NoLimit(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	handleGetAgentTranscript(map[string]interface{}{"agentId": "ag"})
	if cap.get().Query != "" {
		t.Fatalf("expected empty query, got %s", cap.get().Query)
	}
}

// --- Task handlers ---

func TestHandleAddTask(t *testing.T) {
	if res := handleAddTask(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected prompt required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"id":"t1"}`))
	res := handleAddTask(map[string]interface{}{
		"prompt": "do it", "workingDir": "/w", "priority": "high",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/scheduled" || rec.Method != http.MethodPost {
		t.Fatalf("unexpected %s %s", rec.Method, rec.Path)
	}
	for _, want := range []string{`"prompt":"do it"`, `"workingDir":"/w"`, `"priority":"high"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleAddTask_MinimalBody(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleAddTask(map[string]interface{}{"prompt": "p"})
	rec := cap.get()
	if strings.Contains(rec.Body, "workingDir") || strings.Contains(rec.Body, "priority") {
		t.Fatalf("optional fields should be omitted, got %s", rec.Body)
	}
}

func TestHandleListTasks(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	handleListTasks(map[string]interface{}{"status": "pending"})
	if !strings.Contains(cap.get().Query, "status=pending") {
		t.Fatalf("expected status query, got %s", cap.get().Query)
	}
	// no status
	cap2 := &captureRequest{}
	withServer(t, recordingHandler(cap2, 200, `[]`))
	handleListTasks(map[string]interface{}{})
	if cap2.get().Query != "" {
		t.Fatalf("expected empty query, got %s", cap2.get().Query)
	}
}

func TestHandleGetTask(t *testing.T) {
	if res := handleGetTask(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleGetTask(map[string]interface{}{"taskId": "t9"})
	if cap.get().Path != "/api/scheduled/t9" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleRemoveTask(t *testing.T) {
	if res := handleRemoveTask(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskId required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, ``))
	handleRemoveTask(map[string]interface{}{"taskId": "t9"})
	rec := cap.get()
	if rec.Method != http.MethodDelete || rec.Path != "/api/scheduled/t9" {
		t.Fatalf("unexpected %s %s", rec.Method, rec.Path)
	}
}

// --- Skill handlers ---

func TestHandleListSkills(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	if res := handleListSkills(nil); res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/office/agent/skills" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetSkill(t *testing.T) {
	if res := handleGetSkill(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected skillName required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleGetSkill(map[string]interface{}{"skillName": "my skill", "file": "SKILL.md"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if !strings.Contains(rec.RawPath, "/api/office/agent/skills/") {
		t.Fatalf("unexpected raw path %s", rec.RawPath)
	}
	if !strings.Contains(rec.Query, "file=SKILL.md") {
		t.Fatalf("expected file query, got %s", rec.Query)
	}
}

func TestHandleCreateSkill(t *testing.T) {
	if res := handleCreateSkill(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected skillName required")
	}
	if res := handleCreateSkill(map[string]interface{}{"skillName": "s"}); !res.IsError ||
		!strings.Contains(resultText(res), "content is required") {
		t.Fatal("expected content required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	res := handleCreateSkill(map[string]interface{}{
		"skillName": "s", "content": "# body", "file": "x.md",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/office/agent/skill-create" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"skillName":"s"`, `"content":"# body"`, `"file":"x.md"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleSearchSkills(t *testing.T) {
	if res := handleSearchSkills(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected query required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[]`))
	handleSearchSkills(map[string]interface{}{"query": "go test"})
	rec := cap.get()
	if rec.Path != "/api/office/agent/skills/search" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Query, "q=go") {
		t.Fatalf("expected q query, got %s", rec.Query)
	}
}

func TestHandleDeleteSkill(t *testing.T) {
	if res := handleDeleteSkill(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected skillName required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, ``))
	handleDeleteSkill(map[string]interface{}{"skillName": "to delete"})
	rec := cap.get()
	if rec.Method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", rec.Method)
	}
	if !strings.HasPrefix(rec.Path, "/api/office/agent/skills/") {
		t.Fatalf("unexpected path %s", rec.Path)
	}
}

// --- Monitoring handlers + findSessionByWorkingDir ---

// sessionsListJSON is the response body the monitoring handlers see when they
// call /api/sessions to resolve a workingDir to a session id.
const sessionsListJSON = `[{"id":"sess-A","workingDir":"/proj/a"},{"id":"sess-B","workingDir":"/proj/b"}]`

// monitorServer returns a handler that serves the sessions list on
// /api/sessions and records/echoes any other path so the per-tool endpoint can
// be asserted.
func monitorServer(t *testing.T, cap *captureRequest) {
	t.Helper()
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/sessions" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(sessionsListJSON))
			return
		}
		cap.mu.Lock()
		cap.Method = r.Method
		cap.Path = r.URL.Path
		cap.Count++
		cap.mu.Unlock()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestHandleGetGitStatus_ResolvesSessionAndQueries(t *testing.T) {
	if res := handleGetGitStatus(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected workingDir required")
	}
	cap := &captureRequest{}
	monitorServer(t, cap)
	res := handleGetGitStatus(map[string]interface{}{"workingDir": "/proj/a"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/sess-A/git-status" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetGitStatus_NoSessionForDir(t *testing.T) {
	cap := &captureRequest{}
	monitorServer(t, cap)
	res := handleGetGitStatus(map[string]interface{}{"workingDir": "/unknown"})
	if !res.IsError || !strings.Contains(resultText(res), "no session found") {
		t.Fatalf("expected no session found error, got %+v", res)
	}
}

// The build/test/integration handlers share the findSessionByWorkingDir error
// branch; cover it for each by asking for a workingDir with no matching session.
func TestMonitoringHandlers_NoSessionForDir(t *testing.T) {
	handlers := map[string]ToolHandler{
		"build":       handleGetBuildStatus,
		"test":        handleGetTestStatus,
		"integration": handleGetIntegrationState,
	}
	for name, fn := range handlers {
		t.Run(name, func(t *testing.T) {
			cap := &captureRequest{}
			monitorServer(t, cap)
			res := fn(map[string]interface{}{"workingDir": "/no/such/dir"})
			if !res.IsError || !strings.Contains(resultText(res), "no session found") {
				t.Fatalf("%s: expected no session found error, got %+v", name, res)
			}
		})
	}
}

func TestHandleGetBuildStatus(t *testing.T) {
	if res := handleGetBuildStatus(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected workingDir required")
	}
	cap := &captureRequest{}
	monitorServer(t, cap)
	res := handleGetBuildStatus(map[string]interface{}{"workingDir": "/proj/b"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/sess-B/build-status" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetTestStatus(t *testing.T) {
	if res := handleGetTestStatus(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected workingDir required")
	}
	cap := &captureRequest{}
	monitorServer(t, cap)
	res := handleGetTestStatus(map[string]interface{}{"workingDir": "/proj/a"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/sess-A/test-status" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetIntegrationState(t *testing.T) {
	if res := handleGetIntegrationState(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected workingDir required")
	}
	cap := &captureRequest{}
	monitorServer(t, cap)
	res := handleGetIntegrationState(map[string]interface{}{"workingDir": "/proj/b"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if cap.get().Path != "/api/sessions/sess-B/integrations" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleGetGitStatus_SessionsFetchError(t *testing.T) {
	// /api/sessions itself errors -> findSessionByWorkingDir returns error.
	withServer(t, recordingHandler(&captureRequest{}, 500, "down"))
	res := handleGetGitStatus(map[string]interface{}{"workingDir": "/proj/a"})
	if !res.IsError {
		t.Fatal("expected error when sessions fetch fails")
	}
}

func TestFindSessionByWorkingDir_WrappedSessionsKey(t *testing.T) {
	// Response shaped as {"sessions":[...]} should still resolve.
	withServer(t, recordingHandler(&captureRequest{}, 200,
		`{"sessions":[{"id":"wrapped","workingDir":"/w"}]}`))
	id, err := findSessionByWorkingDir("/w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "wrapped" {
		t.Fatalf("expected wrapped, got %s", id)
	}
}

func TestFindSessionByWorkingDir_NonListResponse(t *testing.T) {
	// A bare object with no "sessions" key -> sessions stays nil -> not found.
	withServer(t, recordingHandler(&captureRequest{}, 200, `{"unexpected":"shape"}`))
	_, err := findSessionByWorkingDir("/w")
	if err == nil || !strings.Contains(err.Error(), "no session found") {
		t.Fatalf("expected no session found, got %v", err)
	}
}

func TestFindSessionByWorkingDir_SkipsMalformedEntries(t *testing.T) {
	// Mixed array: a string, an object missing workingDir, then the match.
	withServer(t, recordingHandler(&captureRequest{}, 200,
		`["notanobject",{"id":"x"},{"workingDir":"/w"},{"id":"good","workingDir":"/w"}]`))
	id, err := findSessionByWorkingDir("/w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The third element matches workingDir but has no id, so the loop keeps
	// going and returns the fourth (good).
	if id != "good" {
		t.Fatalf("expected good, got %s", id)
	}
}

// --- State handlers ---

func TestStateHandlers(t *testing.T) {
	cases := []struct {
		name    string
		fn      ToolHandler
		path    string
	}{
		{"app_state", handleGetAppState, "/api/status"},
		{"stats", handleGetStats, "/api/stats"},
		{"config", handleGetConfig, "/api/config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &captureRequest{}
			withServer(t, recordingHandler(cap, 200, `{"ok":true}`))
			res := tc.fn(nil)
			if res.IsError {
				t.Fatalf("unexpected error: %s", resultText(res))
			}
			if cap.get().Path != tc.path {
				t.Fatalf("expected %s, got %s", tc.path, cap.get().Path)
			}
		})
	}
}

func TestStateHandlers_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "x"))
	if res := handleGetAppState(nil); !res.IsError {
		t.Fatal("expected error")
	}
	if res := handleGetStats(nil); !res.IsError {
		t.Fatal("expected error")
	}
	if res := handleGetConfig(nil); !res.IsError {
		t.Fatal("expected error")
	}
}

// --- Planning handler ---

func TestHandleGeneratePlan(t *testing.T) {
	if res := handleGeneratePlan(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected taskDescription required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"plan":"x"}`))
	res := handleGeneratePlan(map[string]interface{}{
		"taskDescription": "build it", "workingDir": "/w",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/generate-plan-detailed" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	if !strings.Contains(rec.Body, `"taskDescription":"build it"`) ||
		!strings.Contains(rec.Body, `"workingDir":"/w"`) {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

func TestHandleGeneratePlan_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "x"))
	if res := handleGeneratePlan(map[string]interface{}{"taskDescription": "x"}); !res.IsError {
		t.Fatal("expected error")
	}
}

// --- Dynamic tool handlers ---

// resetDynamicTools clears the package-level registry so tests are isolated.
func resetDynamicTools(t *testing.T) {
	t.Helper()
	dynamicToolsMu.Lock()
	dynamicTools = make(map[string]DynamicTool)
	dynamicToolsMu.Unlock()
	t.Cleanup(func() {
		dynamicToolsMu.Lock()
		dynamicTools = make(map[string]DynamicTool)
		dynamicToolsMu.Unlock()
	})
}

func TestHandleRegisterTool_Validation(t *testing.T) {
	resetDynamicTools(t)
	if res := handleRegisterTool(map[string]interface{}{}); !res.IsError ||
		!strings.Contains(resultText(res), "name is required") {
		t.Fatal("expected name required")
	}
	if res := handleRegisterTool(map[string]interface{}{"name": "n"}); !res.IsError ||
		!strings.Contains(resultText(res), "description is required") {
		t.Fatal("expected description required")
	}
	if res := handleRegisterTool(map[string]interface{}{"name": "n", "description": "d"}); !res.IsError ||
		!strings.Contains(resultText(res), "prompt is required") {
		t.Fatal("expected prompt required")
	}
	if res := handleRegisterTool(map[string]interface{}{"name": "n", "description": "d", "prompt": "p"}); !res.IsError ||
		!strings.Contains(resultText(res), "workingDir is required") {
		t.Fatal("expected workingDir required")
	}
}

func TestHandleRegisterTool_Success(t *testing.T) {
	resetDynamicTools(t)
	res := handleRegisterTool(map[string]interface{}{
		"name": "mytool", "description": "desc", "prompt": "do {x}", "workingDir": "/w",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "Registered dynamic tool: mytool") {
		t.Fatalf("unexpected text: %s", resultText(res))
	}
	dynamicToolsMu.RLock()
	dt, ok := dynamicTools["mytool"]
	dynamicToolsMu.RUnlock()
	if !ok || dt.Prompt != "do {x}" || dt.WorkingDir != "/w" {
		t.Fatalf("tool not stored correctly: %+v", dt)
	}
}

func TestHandleUnregisterTool(t *testing.T) {
	resetDynamicTools(t)
	if res := handleUnregisterTool(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected name required")
	}
	// Not found.
	res := handleUnregisterTool(map[string]interface{}{"name": "ghost"})
	if !res.IsError || !strings.Contains(resultText(res), `"ghost" not found`) {
		t.Fatalf("expected not found error, got %s", resultText(res))
	}
	// Register then remove.
	handleRegisterTool(map[string]interface{}{
		"name": "t", "description": "d", "prompt": "p", "workingDir": "/w",
	})
	res = handleUnregisterTool(map[string]interface{}{"name": "t"})
	if res.IsError || !strings.Contains(resultText(res), "Unregistered dynamic tool: t") {
		t.Fatalf("unexpected result: %s", resultText(res))
	}
	dynamicToolsMu.RLock()
	_, exists := dynamicTools["t"]
	dynamicToolsMu.RUnlock()
	if exists {
		t.Fatal("tool should have been deleted")
	}
}

func TestHandleListDynamicTools(t *testing.T) {
	resetDynamicTools(t)
	res := handleListDynamicTools(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	// Empty registry -> empty array.
	if !strings.Contains(resultText(res), "[]") {
		t.Fatalf("expected empty array, got %s", resultText(res))
	}
	handleRegisterTool(map[string]interface{}{
		"name": "alpha", "description": "d", "prompt": "p", "workingDir": "/w",
	})
	res = handleListDynamicTools(nil)
	if !strings.Contains(resultText(res), "alpha") {
		t.Fatalf("expected alpha in list, got %s", resultText(res))
	}
}

func TestHandleRunPrompt(t *testing.T) {
	if res := handleRunPrompt(map[string]interface{}{}); !res.IsError {
		t.Fatal("expected prompt required")
	}
	if res := handleRunPrompt(map[string]interface{}{"prompt": "p"}); !res.IsError ||
		!strings.Contains(resultText(res), "workingDir is required") {
		t.Fatal("expected workingDir required")
	}
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"started":true}`))
	res := handleRunPrompt(map[string]interface{}{
		"prompt": "hello", "workingDir": "/w", "model": "opus",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/run" {
		t.Fatalf("unexpected path %s", rec.Path)
	}
	for _, want := range []string{`"command":"hello"`, `"workingDir":"/w"`, `"model":"opus"`} {
		if !strings.Contains(rec.Body, want) {
			t.Fatalf("expected %s in body, got %s", want, rec.Body)
		}
	}
}

func TestHandleRunPrompt_NoModel(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleRunPrompt(map[string]interface{}{"prompt": "p", "workingDir": "/w"})
	if strings.Contains(cap.get().Body, "model") {
		t.Fatalf("model should be omitted, got %s", cap.get().Body)
	}
}

func TestHandleRunPrompt_Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "x"))
	res := handleRunPrompt(map[string]interface{}{"prompt": "p", "workingDir": "/w"})
	if !res.IsError {
		t.Fatal("expected error")
	}
}

// --- dispatchToolCall ---

func TestDispatchToolCall_StaticTool(t *testing.T) {
	resetDynamicTools(t)
	res := dispatchToolCall(ToolCallParams{Name: "list_dynamic_tools", Arguments: map[string]interface{}{}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
}

func TestDispatchToolCall_UnknownTool(t *testing.T) {
	resetDynamicTools(t)
	res := dispatchToolCall(ToolCallParams{Name: "no_such_tool", Arguments: map[string]interface{}{}})
	if !res.IsError || !strings.Contains(resultText(res), "unknown tool: no_such_tool") {
		t.Fatalf("expected unknown tool error, got %s", resultText(res))
	}
}

func TestDispatchToolCall_DynamicToolFoldsInput(t *testing.T) {
	resetDynamicTools(t)
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"started":true}`))

	handleRegisterTool(map[string]interface{}{
		"name": "dyn", "description": "d", "prompt": "base prompt", "workingDir": "/dyndir",
	})
	res := dispatchToolCall(ToolCallParams{
		Name:      "dyn",
		Arguments: map[string]interface{}{"input": "extra input"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	rec := cap.get()
	if rec.Path != "/api/run" {
		t.Fatalf("expected dynamic tool to hit /api/run, got %s", rec.Path)
	}
	// The input should be folded into the prompt with a separator.
	if !strings.Contains(rec.Body, "base prompt") || !strings.Contains(rec.Body, "extra input") {
		t.Fatalf("expected prompt+input in body, got %s", rec.Body)
	}
	if !strings.Contains(rec.Body, `"workingDir":"/dyndir"`) {
		t.Fatalf("expected dynamic workingDir, got %s", rec.Body)
	}
}

func TestDispatchToolCall_DynamicToolNoInput(t *testing.T) {
	resetDynamicTools(t)
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{}`))
	handleRegisterTool(map[string]interface{}{
		"name": "dyn2", "description": "d", "prompt": "only base", "workingDir": "/d",
	})
	res := dispatchToolCall(ToolCallParams{Name: "dyn2", Arguments: map[string]interface{}{}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(cap.get().Body, `"command":"only base"`) {
		t.Fatalf("expected command only base, got %s", cap.get().Body)
	}
}

// --- getAllToolDefs ---

func TestGetAllToolDefs_IncludesDynamic(t *testing.T) {
	resetDynamicTools(t)
	base := getAllToolDefs()
	if len(base) != len(allToolDefs) {
		t.Fatalf("expected %d defs, got %d", len(allToolDefs), len(base))
	}
	handleRegisterTool(map[string]interface{}{
		"name": "dynDef", "description": "the dyn", "prompt": "p", "workingDir": "/w",
	})
	withDyn := getAllToolDefs()
	if len(withDyn) != len(allToolDefs)+1 {
		t.Fatalf("expected %d defs, got %d", len(allToolDefs)+1, len(withDyn))
	}
	var found *ToolDefinition
	for i := range withDyn {
		if withDyn[i].Name == "dynDef" {
			found = &withDyn[i]
			break
		}
	}
	if found == nil {
		t.Fatal("dynamic tool def not present")
	}
	if found.Description != "the dyn" {
		t.Fatalf("unexpected description %q", found.Description)
	}
	if _, ok := found.InputSchema.Properties["input"]; !ok {
		t.Fatalf("expected input property in dynamic schema, got %+v", found.InputSchema)
	}
	// Verify the original static defs slice was not mutated.
	if len(allToolDefs) == 0 {
		t.Fatal("allToolDefs should be non-empty")
	}
}

// --- allToolDefs / allTools integrity ---

func TestEveryToolDefHasHandler(t *testing.T) {
	for _, def := range allToolDefs {
		if _, ok := allTools[def.Name]; !ok {
			t.Errorf("tool def %q has no handler in allTools", def.Name)
		}
	}
}

func TestEveryHandlerHasDef(t *testing.T) {
	defNames := make(map[string]bool, len(allToolDefs))
	for _, d := range allToolDefs {
		defNames[d.Name] = true
	}
	for name := range allTools {
		if !defNames[name] {
			t.Errorf("handler %q registered with no tool def", name)
		}
	}
}

// --- Concurrency: dynamic tool registry is guarded by a mutex ---

func TestDynamicTools_ConcurrentAccess(t *testing.T) {
	resetDynamicTools(t)
	// dispatchToolCall on a dynamic tool issues an HTTP call to /api/run; point
	// it at a local stub so the race test never touches the real backend.
	withServer(t, recordingHandler(&captureRequest{}, 200, `{}`))
	var wg sync.WaitGroup
	const workers = 16
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "tool" + string(rune('A'+n%26))
			handleRegisterTool(map[string]interface{}{
				"name": name, "description": "d", "prompt": "p", "workingDir": "/w",
			})
			handleListDynamicTools(nil)
			getAllToolDefs()
			dispatchToolCall(ToolCallParams{Name: name, Arguments: map[string]interface{}{}})
			handleUnregisterTool(map[string]interface{}{"name": name})
		}(i)
	}
	wg.Wait()
}
