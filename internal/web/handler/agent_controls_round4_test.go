package handler

// Round-4 branch fills for agent_controls.go's residual small arms:
//   - bad-JSON 400 for Kill / Inject / Bump / Restart (DecodeAndValidateMax fail)
//   - nil session-store 500 for Inject (168-171) and Bump (231-234)
//   - writeSessionPrompt's Codex dispatch arm (125-127)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// postRawBadJSON posts a malformed body to a handler and asserts 400.
func postRawBadJSON(t *testing.T, h func(http.ResponseWriter, *http.Request), path string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%s: expected 400 on bad JSON, got %d: %s", path, rec.Code, rec.Body.String())
	}
}

func TestAgentControls_BadJSON400(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	postRawBadJSON(t, ctx.OfficeAgentKill, "/api/office/agent/kill")
	postRawBadJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject")
	postRawBadJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir")
	postRawBadJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart")
}

// agentCtlCtxNilSessions builds a Context whose OfficeManager has a nil
// SessionStore so the handlers' `Sessions() == nil` 500 arms fire.
func agentCtlCtxNilSessions(t *testing.T, dir string) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(50 * time.Millisecond) })
	mgr := office.NewManager(nil, func(string, interface{}) {}, nil)
	ctx := &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{"workingDirAllowlist": []interface{}{dir}})
	return ctx
}

func TestOfficeAgentInject_NilSessionStore500(t *testing.T) {
	ctx := agentCtlCtxNilSessions(t, t.TempDir())
	ctx.OfficeManager.RegisterAgent("sess-n", "cfg-n", &office.TeamMemberConfig{ID: "cfg-n", Name: "Nil"})

	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-n", "prompt": "hello",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the session store is nil, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "session store unavailable") {
		t.Errorf("expected 'session store unavailable', got %s", rec.Body.String())
	}
}

func TestOfficeAgentBumpWorkdir_NilSessionStore500(t *testing.T) {
	dir := t.TempDir()
	ctx := agentCtlCtxNilSessions(t, dir)
	ctx.OfficeManager.RegisterAgent("sess-n", "cfg-n", &office.TeamMemberConfig{ID: "cfg-n", Name: "Nil"})

	rec := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId": "sess-n", "workingDir": dir, "reason": "shift",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the session store is nil, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestWriteSessionPrompt_CodexSession drives the Codex dispatch arm: a live
// Codex session (no PTY, no SDK) means writeSessionPrompt falls through to
// cx.WriteInkPrompt.
func TestWriteSessionPrompt_CodexSession(t *testing.T) {
	installCodexOnPATH(t, "{\"type\":\"thread.started\",\"thread_id\":\"wp-th\"}\n")
	ctx := newSessionExtCtx(t)

	cx, err := ctx.SessionManager.CreateCodexSession(context.Background(), pty.SessionConfig{
		ID: "codex-wp", WorkingDir: t.TempDir(), Mode: pty.ModeCodexExec, Name: "CodexWP",
	})
	if err != nil {
		t.Fatalf("create codex session: %v", err)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(cx.ID)
		time.Sleep(50 * time.Millisecond)
	})

	// No PTY or SDK session for this ID, so the helper reaches the Codex branch.
	_ = ctx.writeSessionPrompt("codex-wp", "via codex")
}
