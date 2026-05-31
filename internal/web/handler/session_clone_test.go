package handler

// Coverage for session_clone.go. The happy path spawns real PTY
// subprocesses (Session.Start execs `claude`/`bash`), which is an IO
// path we can't exercise hermetically. The cleanly testable seam is the
// not-found guard: cloning a session ID the manager has never heard of
// must 404 before any subprocess work.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestCloneSession_NotFound(t *testing.T) {
	ctx := newTestContext()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/ghost/clone", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCloneSession_NotFoundWithCountParam(t *testing.T) {
	// The count query param is parsed only after the source lookup, so an
	// unknown session still 404s even with a count override present.
	ctx := newTestContext()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/ghost/clone?count=3", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// Guard: GetSession on a fresh manager returns not-found, which is the
// precondition the handler relies on. Keeps the 404 contract honest if the
// manager's lookup semantics ever change.
func TestCloneSession_ManagerLookupContract(t *testing.T) {
	mgr := pty.NewSessionManager(10)
	if _, ok := mgr.GetSession("missing"); ok {
		t.Fatal("expected GetSession(missing)=false on empty manager")
	}
	_ = context.Background()
}
