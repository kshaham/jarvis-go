package handler

// Coverage for the read + validation paths of session.go that don't require
// spawning a real PTY subprocess: list/query serialization, the 404 guards on
// get/input/delete/terminal, and the CreateSession request-validation branches
// (missing/invalid workingDir) that reject before any process is launched.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newSessionCtx(t *testing.T) *Context {
	t.Helper()
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
}

func TestListSessions_EmptyIsArray(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	ctx.ListSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Must decode as a bare JSON array (frontend calls .find() on it).
	var arr []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("ListSessions must return an array: %v (body=%s)", err, rec.Body.String())
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got %d", len(arr))
	}
}

func TestQuerySessions_EmptyEnvelope(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := httptest.NewRequest("GET", "/api/sessions/query?limit=10&offset=0&status=idle&search=foo&sort=name&order=asc", nil)
	rec := httptest.NewRecorder()
	ctx.QuerySessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["total"] != float64(0) {
		t.Errorf("total=%v, want 0", resp["total"])
	}
	if resp["hasMore"] != false {
		t.Errorf("hasMore=%v, want false", resp["hasMore"])
	}
	if resp["limit"] != float64(10) {
		t.Errorf("limit=%v, want 10", resp["limit"])
	}
}

func TestQuerySessions_DefaultsAndBadParams(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	// Invalid limit/offset fall back to defaults (limit=50, offset=0).
	req := httptest.NewRequest("GET", "/api/sessions/query?limit=abc&offset=-5", nil)
	rec := httptest.NewRecorder()
	ctx.QuerySessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["limit"] != float64(50) {
		t.Errorf("limit default=%v, want 50", resp["limit"])
	}
	if resp["offset"] != float64(0) {
		t.Errorf("offset default=%v, want 0", resp["offset"])
	}
}

func TestGetSession_NotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/ghost", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetSession(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSessionInput_NotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/ghost/input", strings.NewReader(`{"input":"x"}`)), "id", "ghost")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.SessionInput(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetSessionTerminal_NotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/ghost/terminal", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetSessionTerminal(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/sessions/ghost", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.DeleteSession(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting unknown session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSession_MissingWorkingDir400(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{"name": "x", "mode": "shell"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workingDir") {
		t.Errorf("error should mention workingDir, got %s", rec.Body.String())
	}
}

func TestCreateSession_InvalidWorkingDir400(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	// A path that fails the allowlist/symlink resolver -> 400 before spawn.
	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"workingDir": "/nonexistent/path/that/should/never/resolve",
		"mode":       "shell",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSession_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateSession(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// stopAnySession on an empty manager returns a not-found error from every
// backend, surfacing as the aggregate not-found.
func TestStopAnySession_AllBackendsNotFound(t *testing.T) {
	t.Parallel()
	ctx := newSessionCtx(t)
	err := ctx.stopAnySession(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
