package handler

// Round 5 -- the SessionResize 500 arm (session.Resize fails). We create a
// real shell PTY session, then stop the underlying *Session object directly
// (closing its PTY fd) WITHOUT removing it from the manager. The handler's
// GetSession still finds it, but pty.Setsize on the now-closed descriptor
// returns EBADF -> the handler returns 500.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionResize_ResizeErrorReturns500(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	// Grab the live session and close its PTY fd in place (leaving the
	// session registered so the handler reaches the Resize call).
	sess, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		t.Fatalf("setup: session %s not found", id)
	}
	if err := sess.Stop(); err != nil {
		t.Fatalf("stop session: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx.SessionResize(rec, idReq("POST", "/api/sessions/"+id+"/resize", `{"cols":120,"rows":50}`, id))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when PTY resize fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "resize failed") {
		t.Errorf("expected 'resize failed' in error body, got %q", rec.Body.String())
	}
}
