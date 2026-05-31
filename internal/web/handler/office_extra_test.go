package handler

// Coverage for the office.go handlers that operate on the OfficeManager /
// office package without spawning real agent subprocesses: team lifecycle
// (pause/resume/shutdown), concierge config + validation, spawn-team request
// validation, the experiment/learning/codegraph reads, standup stop +
// user-message validation, and the agent ask/answer validation branches.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newOfficeExtCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // isolate office-memory writes
	// Some office operations (teardown experiment logging, message persistence)
	// fire async goroutines that write under ~/.jarvis. Give them a beat to
	// settle before the t.TempDir cleanup (LIFO: this runs before removal).
	t.Cleanup(func() { time.Sleep(150 * time.Millisecond) })
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager:    pty.NewSessionManager(10),
		SSEBroker:         sse.NewBroker(100, 10),
		CSRFStore:         middleware.NewCSRFStore(context.Background()),
		StateStore:        state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:     mgr,
		TeamTemplateStore: teamtemplates.NewStore(t.TempDir()),
	}
}

// --- Team lifecycle ---

func TestOfficePauseResume_TogglesState(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	recP := postJSON(t, ctx.OfficePauseTeam, "/api/office/pause", map[string]interface{}{"reason": "lunch"})
	if recP.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d", recP.Code)
	}
	respP := decodeMap(t, recP)
	if respP["paused"] != true {
		t.Errorf("paused=%v, want true", respP["paused"])
	}
	paused, _, reason := ctx.OfficeManager.PauseSnapshot()
	if !paused || reason != "lunch" {
		t.Errorf("manager state not paused: paused=%v reason=%q", paused, reason)
	}

	recR := postJSON(t, ctx.OfficeResumeTeam, "/api/office/resume", map[string]interface{}{})
	if recR.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", recR.Code)
	}
	respR := decodeMap(t, recR)
	if respR["paused"] != false {
		t.Errorf("paused=%v, want false after resume", respR["paused"])
	}
	if p, _, _ := ctx.OfficeManager.PauseSnapshot(); p {
		t.Error("manager should be unpaused after resume")
	}
}

func TestOfficeShutdownTeam_OK(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeShutdownTeam, "/api/office/shutdown", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Teardown clears the spawned flag.
	if ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should not be spawned after shutdown")
	}
}

// --- Concierge ---

func TestConciergeConfig_RequiresGeminiKey(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// No key -> 412.
	req := httptest.NewRequest("GET", "/api/concierge/config", nil)
	rec := httptest.NewRecorder()
	ctx.ConciergeConfig(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 without gemini key, got %d", rec.Code)
	}

	// With a key -> 200 and the key + defaults.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"geminiApiKey": "g-key"})
	resp := getJSON(t, ctx.ConciergeConfig, "/api/concierge/config")
	if resp["apiKey"] != "g-key" {
		t.Errorf("apiKey=%v, want g-key", resp["apiKey"])
	}
	if resp["voice"] != "Charon" {
		t.Errorf("voice default=%v, want Charon", resp["voice"])
	}
}

func TestConciergeTemplates_ReturnsList(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	resp := getJSON(t, ctx.ConciergeTemplates, "/api/concierge/templates")
	if _, ok := resp["templates"]; !ok {
		t.Error("expected templates key")
	}
}

func TestConciergeSpawnTeam_Validation(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Missing templateId.
	rec := postJSON(t, ctx.ConciergeSpawnTeam, "/api/concierge/spawn", map[string]interface{}{"mission": "m"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing templateId: expected 400, got %d", rec.Code)
	}

	// Missing mission.
	rec2 := postJSON(t, ctx.ConciergeSpawnTeam, "/api/concierge/spawn", map[string]interface{}{"templateId": "t"})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("missing mission: expected 400, got %d", rec2.Code)
	}

	// Unknown templateId.
	rec3 := postJSON(t, ctx.ConciergeSpawnTeam, "/api/concierge/spawn", map[string]interface{}{
		"templateId": "does-not-exist", "mission": "m",
	})
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("unknown templateId: expected 400, got %d", rec3.Code)
	}
}

// --- Spawn team validation ---

func TestOfficeSpawnTeam_MissingWorkingDir400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"templateId": "t",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeSpawnTeam_InvalidWorkingDir400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"templateId": "t", "workingDir": "/no/such/dir/at/all",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid workingDir, got %d", rec.Code)
	}
}

func TestOfficeSpawnTeam_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("POST", "/api/office/spawn", nil)
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	ctx.OfficeSpawnTeam(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty/bad body, got %d", rec.Code)
	}
}

func TestSpawnTeamFromRequest_NewProjectInvalidName(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := httptest.NewRecorder()
	// NewProject with an invalid (empty) project name -> 400 from PrepareNewProject.
	ctx.spawnTeamFromRequest(rec, spawnTeamRequest{NewProject: true, ProjectName: "../evil"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid project name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Reads (empty/no-team states) ---

func TestOfficeGetExperiments_EmptyOK(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	resp := getJSON(t, ctx.OfficeGetExperiments, "/api/office/experiments")
	if _, ok := resp["experiments"]; !ok {
		t.Error("expected experiments key")
	}
}

func TestOfficeGetLearnings_EmptyOK(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	resp := getJSON(t, ctx.OfficeGetLearnings, "/api/office/learnings")
	if _, ok := resp["learnings"]; !ok {
		t.Error("expected learnings key")
	}
}

func TestOfficeGetLearningsByConfig_InvalidIDAndMissing(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Invalid config ID -> 400.
	reqBad := withURLParam(httptest.NewRequest("GET", "/api/office/learnings/..", nil), "configId", "../bad")
	recBad := httptest.NewRecorder()
	ctx.OfficeGetLearningsByConfig(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid config id: expected 400, got %d", recBad.Code)
	}

	// Valid but missing config -> 200 with empty learnings array.
	reqOK := withURLParam(httptest.NewRequest("GET", "/api/office/learnings/ghost", nil), "configId", "ghost")
	recOK := httptest.NewRecorder()
	ctx.OfficeGetLearningsByConfig(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("missing config: expected 200, got %d", recOK.Code)
	}
	resp := decodeMap(t, recOK)
	learnings, _ := resp["learnings"].([]interface{})
	if len(learnings) != 0 {
		t.Errorf("expected empty learnings, got %d", len(learnings))
	}
}

func TestOfficeGetCodeGraph_NoSessionsUnavailable(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	resp := getJSON(t, ctx.OfficeGetCodeGraph, "/api/office/codegraph")
	if resp["available"] != false {
		t.Errorf("available=%v, want false with no sessions", resp["available"])
	}
}

func TestOfficePostCodeGraphBuild_NoSessionsUnavailable(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostCodeGraphBuild, "/api/office/codegraph/build", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["available"] != false {
		t.Errorf("available=%v, want false", resp["available"])
	}
}

func TestOfficePostCodeGraphImpact_NoSessions(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostCodeGraphImpact, "/api/office/codegraph/impact", map[string]interface{}{
		"files": []string{"a.go"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if _, ok := resp["files"]; !ok {
		t.Error("expected files key in impact response")
	}
}

// --- Standup ---

func TestOfficePostStandup_StopReturnsStandup(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostStandup, "/api/office/standup", map[string]interface{}{"action": "stop"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for stop, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostStandupUserMessage_Validation(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Missing content -> 400.
	rec := postJSON(t, ctx.OfficePostStandupUserMessage, "/api/office/standup/message", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing content: expected 400, got %d", rec.Code)
	}

	// Content present but no active standup -> 400.
	rec2 := postJSON(t, ctx.OfficePostStandupUserMessage, "/api/office/standup/message", map[string]interface{}{
		"content": "hi team",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("no active standup: expected 400, got %d", rec2.Code)
	}
}

func TestOfficeGetStandupByID_InvalidAndNotFound(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Invalid ID -> 400.
	reqBad := withURLParam(httptest.NewRequest("GET", "/api/office/standups/..", nil), "standupId", "../etc")
	recBad := httptest.NewRecorder()
	ctx.OfficeGetStandupByID(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid id: expected 400, got %d", recBad.Code)
	}

	// Valid but missing -> 404.
	reqMiss := withURLParam(httptest.NewRequest("GET", "/api/office/standups/ghost", nil), "standupId", "ghost")
	recMiss := httptest.NewRecorder()
	ctx.OfficeGetStandupByID(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("missing standup: expected 404, got %d", recMiss.Code)
	}
}

// --- Agent ask / answer ---

func TestOfficeAgentAsk_Validation(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Missing fields -> 400.
	rec := postJSON(t, ctx.OfficeAgentAsk, "/api/office/agent/ask", map[string]interface{}{"content": "q"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fromSessionId: expected 400, got %d", rec.Code)
	}

	// Unknown asker -> AskQuestion errors -> 400.
	rec2 := postJSON(t, ctx.OfficeAgentAsk, "/api/office/agent/ask", map[string]interface{}{
		"fromSessionId": "ghost", "content": "anyone there?", "waitSeconds": 1,
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("unknown asker: expected 400, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestOfficeAgentAskAnswer_Validation(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Missing fields -> 400.
	rec := postJSON(t, ctx.OfficeAgentAskAnswer, "/api/office/agent/ask/answer", map[string]interface{}{
		"fromSessionId": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: expected 400, got %d", rec.Code)
	}

	// Unknown question -> AnswerQuestion errors -> 400.
	rec2 := postJSON(t, ctx.OfficeAgentAskAnswer, "/api/office/agent/ask/answer", map[string]interface{}{
		"fromSessionId": "x", "questionId": "no-such-q", "answer": "42",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("unknown question: expected 400, got %d", rec2.Code)
	}
}

func TestOfficeDeleteFileLock_OK(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/file-locks/src/main.go", nil), "*", "src/main.go")
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteFileLock(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficePutSettings_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("PUT", "/api/office/settings", nil)
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	ctx.OfficePutSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestOfficePostWrapUpCancel_OK(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostWrapUpCancel, "/api/office/wrap-up/cancel", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
