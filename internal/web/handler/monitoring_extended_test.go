package handler

// Coverage for monitoring_extended.go: the auth lifecycle (setup -> login ->
// change-password -> logout) against a real file-backed auth.Manager, plus the
// screen + subagent handlers whose nil-manager branches are exercised with a
// bare Context and whose StateStore CRUD paths use a temp-dir store.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/auth"
)

func newAuthCtx(t *testing.T) *Context {
	t.Helper()
	am, err := auth.NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return &Context{
		AuthManager: am,
		StateStore:  state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
}

// --- Auth lifecycle ---

func TestGetAuthStatus_UnconfiguredThenConfigured(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)

	// Fresh manager: no password set.
	resp := getJSON(t, ctx.GetAuthStatus, "/api/auth/status")
	if resp["passwordConfigured"] != false {
		t.Errorf("passwordConfigured=%v, want false", resp["passwordConfigured"])
	}
	if resp["authenticated"] != false {
		t.Errorf("authenticated=%v, want false", resp["authenticated"])
	}

	// Configure a password directly, then status should flip.
	if err := ctx.AuthManager.SetPassword("hunter2pass"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	resp2 := getJSON(t, ctx.GetAuthStatus, "/api/auth/status")
	if resp2["passwordConfigured"] != true {
		t.Errorf("passwordConfigured=%v, want true after SetPassword", resp2["passwordConfigured"])
	}
}

func TestAuthSetup_HappyAndAlreadyConfigured(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)

	// Weak password -> 400.
	recWeak := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{"password": "short"})
	if recWeak.Code != http.StatusBadRequest {
		t.Fatalf("weak password: expected 400, got %d", recWeak.Code)
	}

	// Valid setup -> 200 + Set-Cookie.
	rec := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{"password": "strongpassword1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Error("expected a session cookie after setup")
	}

	// Second setup -> 400 (already configured).
	rec2 := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{"password": "anotherpassword1"})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("repeat setup: expected 400, got %d", rec2.Code)
	}
}

func TestAuthSetup_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	req := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.AuthSetup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAuthLogin_WrongAndRight(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	if err := ctx.AuthManager.SetPassword("correcthorse"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	// Wrong password -> 401.
	recWrong := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": "wrongguess"})
	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: expected 401, got %d: %s", recWrong.Code, recWrong.Body.String())
	}

	// Empty password -> 400.
	recEmpty := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": ""})
	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("empty password: expected 400, got %d", recEmpty.Code)
	}

	// Correct password -> 200 + cookie.
	recOK := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": "correcthorse"})
	if recOK.Code != http.StatusOK {
		t.Fatalf("correct password: expected 200, got %d", recOK.Code)
	}
	if len(recOK.Result().Cookies()) == 0 {
		t.Error("expected a session cookie after login")
	}
}

func TestAuthLogin_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	if err := ctx.AuthManager.SetPassword("correcthorse"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.AuthLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAuthLogout_ClearsCookie(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	rec := postJSON(t, ctx.AuthLogout, "/api/auth/logout", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// A clear cookie should be set (MaxAge<=0 or empty value).
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a clear cookie in logout response")
	}
}

func TestAuthChangePassword_Flow(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	if err := ctx.AuthManager.SetPassword("originalpass1"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	// Empty current password -> 400.
	recEmpty := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"newPassword": "newstrongpass1",
	})
	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("empty current: expected 400, got %d", recEmpty.Code)
	}

	// Weak new password -> 400.
	recWeak := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"currentPassword": "originalpass1", "newPassword": "weak",
	})
	if recWeak.Code != http.StatusBadRequest {
		t.Fatalf("weak new: expected 400, got %d", recWeak.Code)
	}

	// Wrong current password -> 401.
	recWrong := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"currentPassword": "notthepassword", "newPassword": "newstrongpass1",
	})
	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current: expected 401, got %d: %s", recWrong.Code, recWrong.Body.String())
	}

	// Correct change -> 200.
	recOK := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"currentPassword": "originalpass1", "newPassword": "newstrongpass1",
	})
	if recOK.Code != http.StatusOK {
		t.Fatalf("change: expected 200, got %d: %s", recOK.Code, recOK.Body.String())
	}
	// The new password should now verify.
	valid, err := ctx.AuthManager.VerifyPassword("newstrongpass1")
	if err != nil || !valid {
		t.Errorf("new password should verify: valid=%v err=%v", valid, err)
	}
}

func TestAuthChangePassword_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtx(t)
	req := httptest.NewRequest("POST", "/api/auth/change-password", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.AuthChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetClaudeAuthStatus_AlwaysOK(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("GET", "/api/claude/auth-status", nil)
	rec := httptest.NewRecorder()
	ctx.GetClaudeAuthStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- Screens (nil manager branches) ---

func TestGetScreens_NilManager(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.GetScreens, "/api/screens")
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	scr, _ := resp["screens"].([]interface{})
	if len(scr) != 0 {
		t.Errorf("expected empty screens, got %d", len(scr))
	}
}

func TestDeleteScreen_NilManager503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/screens/s1", nil), "sessionId", "s1")
	rec := httptest.NewRecorder()
	ctx.DeleteScreen(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestReconcileScreens_NilManager(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.ReconcileScreens, "/api/screens/reconcile")
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
}

func TestStartStopScreenStats_PersistsFlag(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	recStart := postJSON(t, ctx.StartScreenStats, "/api/screens/stats/start", map[string]interface{}{})
	if recStart.Code != http.StatusOK {
		t.Fatalf("start: expected 200, got %d", recStart.Code)
	}
	if ctx.StateStore.GetSettings()["screenStatsCollecting"] != true {
		t.Error("screenStatsCollecting should be true after start")
	}

	recStop := postJSON(t, ctx.StopScreenStats, "/api/screens/stats/stop", map[string]interface{}{})
	if recStop.Code != http.StatusOK {
		t.Fatalf("stop: expected 200, got %d", recStop.Code)
	}
	if ctx.StateStore.GetSettings()["screenStatsCollecting"] != false {
		t.Error("screenStatsCollecting should be false after stop")
	}
}

// --- Subagents (nil watcher branches) ---

func TestGetSubagents_NilWatcher(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.GetSubagents, "/api/subagents")
	agents, _ := resp["agents"].([]interface{})
	if len(agents) != 0 {
		t.Errorf("expected empty agents, got %d", len(agents))
	}
}

func TestGetSubagent_NilWatcher404(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("GET", "/api/subagents/a1", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.GetSubagent(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteSubagent_NilWatcher503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/subagents/a1", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.DeleteSubagent(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestGetSubagentTranscript_NilWatcher404(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("GET", "/api/subagents/a1/transcript", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCleanupSubagents_NilWatcher(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.CleanupSubagents, "/api/subagents/cleanup")
	if resp["cleaned"] != float64(0) {
		t.Errorf("cleaned=%v, want 0", resp["cleaned"])
	}
}

func TestClearAllSubagents_NilWatcher(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.ClearAllSubagents, "/api/subagents")
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
}

func TestGetSessionSubagents_SessionNotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/ghost/subagents", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetSessionSubagents(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d", rec.Code)
	}
}

// --- Subagent window-states + parents (StateStore CRUD) ---

func TestSubagentWindowStates_GetUpdate(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}

	// Default empty.
	resp := getJSON(t, ctx.GetSubagentWindowStates, "/api/subagent-window-states")
	states, _ := resp["states"].(map[string]interface{})
	if len(states) != 0 {
		t.Errorf("expected empty states, got %v", states)
	}

	// Update then re-get.
	rec := postJSON(t, ctx.UpdateSubagentWindowStates, "/api/subagent-window-states", map[string]interface{}{"a1": "open"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rec.Code)
	}
	resp2 := getJSON(t, ctx.GetSubagentWindowStates, "/api/subagent-window-states")
	states2, _ := resp2["states"].(map[string]interface{})
	if states2["a1"] != "open" {
		t.Errorf("states.a1=%v, want open", states2["a1"])
	}
}

func TestUpdateSubagentWindowStates_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	req := httptest.NewRequest("PUT", "/api/subagent-window-states", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateSubagentWindowStates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSubagentParents_GetUpdate(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}

	resp := getJSON(t, ctx.GetSubagentParents, "/api/subagent-parents")
	parents, _ := resp["parents"].(map[string]interface{})
	if len(parents) != 0 {
		t.Errorf("expected empty parents, got %v", parents)
	}

	rec := postJSON(t, ctx.UpdateSubagentParents, "/api/subagent-parents", map[string]interface{}{"child": "parent"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rec.Code)
	}
	resp2 := getJSON(t, ctx.GetSubagentParents, "/api/subagent-parents")
	parents2, _ := resp2["parents"].(map[string]interface{})
	if parents2["child"] != "parent" {
		t.Errorf("parents.child=%v, want parent", parents2["child"])
	}
}

func TestUpdateSubagentParents_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	req := httptest.NewRequest("PUT", "/api/subagent-parents", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateSubagentParents(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
