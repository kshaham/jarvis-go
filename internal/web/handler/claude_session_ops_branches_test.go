package handler

// Branch fills for claude_session_ops.go: the runErr -> 502 BadGateway path of
// OfficeClaudeLogs/OfficeClaudeStop, the resolveClaudeSessionID 400 guard at the
// handler boundary, and resolveClaudeSessionID's live-SDK-session arms
// (captured Claude ID returned; or "not produced yet" error).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/pty"
)

func claudeSessionIDReq(method, sessionID string) *http.Request {
	req := httptest.NewRequest(method, "/api/office/claude-sessions/"+sessionID+"/x", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", sessionID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// installFailingClaudeSessionOps points claudeSessionOpsBinary at a stub that
// exits non-zero, driving the runClaudeSessionCmd error path.
func installFailingClaudeSessionOps(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'no such session' 1>&2\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prev := claudeSessionOpsBinary
	claudeSessionOpsBinary = path
	t.Cleanup(func() { claudeSessionOpsBinary = prev })
}

func TestOfficeClaudeLogs_RunErrorBadGateway(t *testing.T) {
	installFailingClaudeSessionOps(t)
	ctx := newSnapshotCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeLogs(rec, claudeSessionIDReq(http.MethodGet, "valid-session-id-1234"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when claude logs fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeClaudeStop_RunErrorBadGateway(t *testing.T) {
	installFailingClaudeSessionOps(t)
	ctx := newSnapshotCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeStop(rec, claudeSessionIDReq(http.MethodPost, "valid-session-id-1234"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when claude stop fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeClaudeLogs_BadSessionID400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeLogs(rec, claudeSessionIDReq(http.MethodGet, "short")) // <8 chars
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short sessionId, got %d", rec.Code)
	}
}

func TestResolveClaudeSessionID_LiveSDKWithoutClaudeID(t *testing.T) {
	ctx := &Context{SessionManager: pty.NewSessionManager(10)}
	// A never-started SDK session has no captured Claude session ID yet.
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "sdk-no-cid-123456", Mode: pty.ModeClaudeSDK})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	_, err := ctx.resolveClaudeSessionID("sdk-no-cid-123456")
	if err == nil || !strings.Contains(err.Error(), "not produced a Claude session ID") {
		t.Fatalf("expected 'not produced' error for SDK session without captured cid, got %v", err)
	}
}
