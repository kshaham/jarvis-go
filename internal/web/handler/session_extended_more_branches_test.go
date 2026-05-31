package handler

// More branch fills for session_extended.go:
//   - SessionResize: SDK-session no-op (200), bad-JSON (400), non-positive
//     dimensions (400)
//   - GetSessionGitStatus: the happy path (IntegrationMonitor + live session)

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/integrations"
	"github.com/kshaham/jarvis/internal/pty"
)

func TestSessionResize_SDKSessionNoop(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "resize-sdk", Mode: pty.ModeClaudeSDK, WorkingDir: t.TempDir()})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	rec := httptest.NewRecorder()
	ctx.SessionResize(rec, idReq("POST", "/api/sessions/resize-sdk/resize", `{"cols":80,"rows":24}`, "resize-sdk"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 no-op for SDK session, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["noop"] == nil {
		t.Errorf("expected a noop marker in SDK-resize response, got %v", resp)
	}
}

func TestSessionResize_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.SessionResize(rec, idReq("POST", "/api/sessions/"+id+"/resize", "{bad", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestSessionResize_NonPositiveDims(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.SessionResize(rec, idReq("POST", "/api/sessions/"+id+"/resize", `{"cols":0,"rows":0}`, id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-positive dims, got %d", rec.Code)
	}
}

func TestGetSessionGitStatus_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	ctx.IntegrationMonitor = integrations.NewMonitor()
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionGitStatus(rec, idReq("GET", "/api/sessions/"+id+"/git-status", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("git-status: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
}
