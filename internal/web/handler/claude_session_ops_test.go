package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
	"github.com/kshaham/jarvis/internal/web/sse"
	"github.com/go-chi/chi/v5"
)

func installClaudeSessionOpsStub(t *testing.T, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	return stubbin.EchoStub(t, output)
}

func withClaudeSessionOpsStub(t *testing.T, output string) {
	t.Helper()
	prev := claudeSessionOpsBinary
	claudeSessionOpsBinary = installClaudeSessionOpsStub(t, output)
	t.Cleanup(func() { claudeSessionOpsBinary = prev })
}

func TestRunClaudeSessionCmd_LogsHappy(t *testing.T) {
	withClaudeSessionOpsStub(t, "line 1\nline 2\n")
	out, err := runClaudeSessionCmd(context.Background(), "logs", "claude-sid-abc")
	if err != nil {
		t.Fatalf("runClaudeSessionCmd: %v", err)
	}
	if !strings.Contains(string(out), "line 1") {
		t.Errorf("output missing 'line 1': %q", out)
	}
}

func TestRunClaudeSessionCmd_StopHappy(t *testing.T) {
	withClaudeSessionOpsStub(t, "stopped\n")
	_, err := runClaudeSessionCmd(context.Background(), "stop", "claude-sid-xyz")
	if err != nil {
		t.Fatalf("runClaudeSessionCmd stop: %v", err)
	}
}

func TestRunClaudeSessionCmd_StubFailureBubblesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fail.sh")
	body := "#!/bin/sh\necho 'session does not exist' 1>&2\nexit 7\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prev := claudeSessionOpsBinary
	claudeSessionOpsBinary = path
	t.Cleanup(func() { claudeSessionOpsBinary = prev })

	_, err := runClaudeSessionCmd(context.Background(), "logs", "missing-sid")
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	if !strings.Contains(err.Error(), "session does not exist") {
		t.Errorf("err should surface stderr; got %v", err)
	}
}

func TestRunClaudeSessionCmd_TimeoutSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.sh")
	body := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prev := claudeSessionOpsBinary
	prevTimeout := claudeSessionOpsTimeout
	claudeSessionOpsBinary = path
	claudeSessionOpsTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		claudeSessionOpsBinary = prev
		claudeSessionOpsTimeout = prevTimeout
	})
	_, err := runClaudeSessionCmd(context.Background(), "logs", "sid")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestResolveClaudeSessionID_EmptyInputErrors(t *testing.T) {
	ctx := newSnapshotCtx(t)
	_, err := ctx.resolveClaudeSessionID("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestResolveClaudeSessionID_UnknownPassesThrough(t *testing.T) {
	// When the input doesn't match any live SDK session, it's returned
	// verbatim so operators can hand the function a raw Claude
	// session ID directly.
	ctx := newSnapshotCtx(t)
	got, err := ctx.resolveClaudeSessionID("unknown-claude-sid-123")
	if err != nil {
		t.Fatalf("expected pass-through, got err: %v", err)
	}
	if got != "unknown-claude-sid-123" {
		t.Errorf("got %q, want pass-through of input", got)
	}
}

func TestOfficeClaudeAttachCommand_ReturnsArgv(t *testing.T) {
	ctx := newSnapshotCtx(t)
	prev := claudeSessionOpsBinary
	claudeSessionOpsBinary = "claude"
	t.Cleanup(func() { claudeSessionOpsBinary = prev })

	req := httptest.NewRequest(http.MethodGet, "/api/office/claude-sessions/raw-session-id-1234/attach-command", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "raw-session-id-1234")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeAttachCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["claudeSessionId"] != "raw-session-id-1234" {
		t.Errorf("claudeSessionId=%v, want raw-session-id-1234", resp["claudeSessionId"])
	}
	if resp["command"] != "claude" {
		t.Errorf("command=%v, want claude", resp["command"])
	}
	args, _ := resp["args"].([]interface{})
	if len(args) != 2 || args[0] != "attach" || args[1] != "raw-session-id-1234" {
		t.Errorf("args=%v, want [attach, raw-session-id-1234]", args)
	}
}

func TestOfficeClaudeLogs_PlainTextHeader(t *testing.T) {
	withClaudeSessionOpsStub(t, "log line 1\nlog line 2\n")
	ctx := newSnapshotCtx(t)

	req := httptest.NewRequest(http.MethodGet, "/api/office/claude-sessions/raw-session-id-1234/logs", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "raw-session-id-1234")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type=%q, want text/plain; charset=utf-8", got)
	}
	if !strings.Contains(rec.Body.String(), "log line 1") {
		t.Errorf("body missing stub output: %q", rec.Body.String())
	}
}

func TestOfficeClaudeStop_BroadcastsSSE(t *testing.T) {
	withClaudeSessionOpsStub(t, "stopped\n")
	ctx := newSnapshotCtx(t)
	// Hook the SSE broker so we can assert exactly which events fire.
	var received []string
	ctx.SSEBroker.AddPostBroadcastHook(func(msg sse.EventMessage) {
		received = append(received, msg.Event)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/office/claude-sessions/raw-session-id-1234/stop", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "raw-session-id-1234")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeStop(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	count := 0
	for _, e := range received {
		if e == "office:claudeSessionStopped" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected office:claudeSessionStopped to fire exactly once, fired %d times (all events: %v)", count, received)
	}
}

func TestOfficeClaudeStop_RejectsBadSessionID(t *testing.T) {
	// Regression for P7.1.4: a malformed sessionId should never reach
	// exec.Command. Returns 400 from resolveClaudeSessionID.
	ctx := newSnapshotCtx(t)
	req := httptest.NewRequest(http.MethodPost, "/api/office/claude-sessions/short/stop", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "short") // <8 chars
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeStop(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short sessionId, got %d", rec.Code)
	}
}

func TestOfficeClaudeAttachCommand_RejectsEmptySessionID(t *testing.T) {
	ctx := newSnapshotCtx(t)
	req := httptest.NewRequest(http.MethodGet, "/api/office/claude-sessions//attach-command", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeClaudeAttachCommand(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty sessionId, got %d", rec.Code)
	}
}
