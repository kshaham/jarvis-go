package handler

// R18 · Happy-path + edge-case tests for the per-agent kill / inject
// operator endpoints. The handlers are browser-auth only (wired at the
// router level), so these tests focus on handler-level behaviors:
// missing fields, not-found agents, prompt size limits, SSE side
// effects, and OfficeManager unregistration.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

type recordingSessions struct {
	stubSessions
	injected map[string]string
}

func (r *recordingSessions) WriteInkPrompt(sessionID, prompt string) bool {
	if r.injected == nil {
		r.injected = map[string]string{}
	}
	r.injected[sessionID] = prompt
	return true
}

func newAgentControlsContext(t *testing.T, sess office.SessionStore) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(100 * time.Millisecond) })

	mgr := office.NewManager(sess, func(event string, data interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
}

func postJSON(t *testing.T, handler func(http.ResponseWriter, *http.Request), path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestOfficeAgentKill_MissingSessionID(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	rec := postJSON(t, ctx.OfficeAgentKill, "/api/office/agent/kill", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentKill_UnknownSessionSucceeds(t *testing.T) {
	// Unknown sessions are tolerated -- the handler just no-ops the
	// session stop and unregisters whatever the office manager has
	// (also a no-op). This matches /shutdown-team's forgiving behavior.
	ctx := newAgentControlsContext(t, &recordingSessions{})
	rec := postJSON(t, ctx.OfficeAgentKill, "/api/office/agent/kill", map[string]interface{}{
		"sessionId": "does-not-exist",
		"reason":    "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentKill_UnregistersAndBroadcasts(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.OfficeManager.RegisterAgent("sess-a", "lead", &office.TeamMemberConfig{Name: "Ops"})

	rec := postJSON(t, ctx.OfficeAgentKill, "/api/office/agent/kill", map[string]interface{}{
		"sessionId": "sess-a",
		"reason":    "runaway",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if agent := ctx.OfficeManager.GetAgent("sess-a"); agent != nil {
		t.Fatalf("expected agent unregistered, still present")
	}
}

func TestOfficeAgentInject_MissingSessionID(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"prompt": "hi",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentInject_MissingPrompt(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.OfficeManager.RegisterAgent("sess-a", "lead", &office.TeamMemberConfig{Name: "Ops"})

	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-a",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentInject_PromptTooLarge(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.OfficeManager.RegisterAgent("sess-a", "lead", &office.TeamMemberConfig{Name: "Ops"})

	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-a",
		"prompt":    strings.Repeat("x", maxInjectPromptBytes+1),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentInject_UnknownAgent(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "nope",
		"prompt":    "hi",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentInject_HappyPath(t *testing.T) {
	sess := &recordingSessions{}
	ctx := newAgentControlsContext(t, sess)
	ctx.OfficeManager.RegisterAgent("sess-a", "lead", &office.TeamMemberConfig{Name: "Ops"})

	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-a",
		"prompt":    "please stop debating and commit what you have",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, ok := sess.injected["sess-a"]
	if !ok {
		t.Fatalf("expected WriteInkPrompt called for sess-a")
	}
	if !strings.HasPrefix(got, "[OPERATOR PROMPT]") {
		t.Fatalf("expected [OPERATOR PROMPT] prefix, got %q", got)
	}
	if !strings.Contains(got, "commit what you have") {
		t.Fatalf("expected prompt body preserved, got %q", got)
	}
}
