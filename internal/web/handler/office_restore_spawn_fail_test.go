package handler

// Covers the OfficeRestoreSnapshot "spawn failed" relay branch and the
// snapshot-not-found branch. The spawn-failure path needs a StateStore (the
// inner spawnTeamFromRequest resolves the working dir against settings), so we
// build a dedicated context rather than reusing newSnapshotCtx.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newRestoreCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func fireRestore(ctx *Context, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots/"+name+"/restore", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeRestoreSnapshot(rec, req)
	return rec
}

func TestOfficeRestoreSnapshot_SpawnFailureRelayed(t *testing.T) {
	ctx := newRestoreCtx(t)
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:       "bad-wd",
		WorkingDir: "/no/such/dir/that/resolves",
		Members: []office.SnapshotMemberEntry{
			{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}},
		},
	})
	// Members present + valid fork, but the workingDir fails the allowlist
	// resolver inside spawnTeamFromRequest -> a 4xx is buffered and relayed.
	rec := fireRestore(ctx, "bad-wd")
	if rec.Code < 400 {
		t.Fatalf("expected the spawn failure (>=400) to be relayed, got %d: %s", rec.Code, rec.Body.String())
	}
	ctx.OfficeManager.SetTeamSpawned(false)
}

func TestOfficeRestoreSnapshot_NotFound(t *testing.T) {
	ctx := newRestoreCtx(t)
	rec := fireRestore(ctx, "does-not-exist")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing snapshot, got %d", rec.Code)
	}
}
