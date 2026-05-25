package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/claudeman/jarvis-go/internal/testutil/stubbin"
)

func installSpawnRemoteStub(t *testing.T, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	return stubbin.EchoStub(t, output)
}

func withSpawnRemoteStub(t *testing.T, output string) {
	t.Helper()
	prev := spawnRemoteBinary
	spawnRemoteBinary = installSpawnRemoteStub(t, output)
	t.Cleanup(func() { spawnRemoteBinary = prev })
}

func TestRunSpawnRemote_PrefersStructuredJSON(t *testing.T) {
	withSpawnRemoteStub(t, `{"url":"https://claude.ai/chat/abc123","sessionId":"abc123"}`)
	url, sid, err := runSpawnRemote(context.Background(), "do a thing", "")
	if err != nil {
		t.Fatalf("runSpawnRemote: %v", err)
	}
	if url != "https://claude.ai/chat/abc123" {
		t.Errorf("url mismatch: %q", url)
	}
	if sid != "abc123" {
		t.Errorf("sessionId mismatch: %q", sid)
	}
}

func TestRunSpawnRemote_FallsBackToRegex(t *testing.T) {
	withSpawnRemoteStub(t, "Some banner\nOpen this: https://claude.ai/chat/zzz999\n")
	url, sid, err := runSpawnRemote(context.Background(), "do a thing", "")
	if err != nil {
		t.Fatalf("runSpawnRemote: %v", err)
	}
	if url != "https://claude.ai/chat/zzz999" {
		t.Errorf("url mismatch: %q", url)
	}
	if sid != "zzz999" {
		t.Errorf("sessionId mismatch: %q", sid)
	}
}

func TestRunSpawnRemote_NoURLReturnsError(t *testing.T) {
	withSpawnRemoteStub(t, "Just some output without a link")
	_, _, err := runSpawnRemote(context.Background(), "do a thing", "")
	if err == nil {
		t.Fatal("expected error when CLI output has no URL")
	}
}

func spawnRemoteHandlerReq(t *testing.T, ctx *Context, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/office/spawn-remote", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeSpawnRemote(rec, req)
	return rec
}

func TestOfficeSpawnRemote_RejectsEmptyTask(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{"task": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty task, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeSpawnRemote_RejectsWhitespaceOnlyTask(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{"task": "    \n\t  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace-only task, got %d", rec.Code)
	}
}

func TestOfficeSpawnRemote_RejectsOversizeTask(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// 5KB > maxSpawnRemoteTaskLen (4KB) after trim.
	big := strings.Repeat("x", maxSpawnRemoteTaskLen+1)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{"task": big})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize task, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("error should mention 'too long': %s", rec.Body.String())
	}
}

func TestOfficeSpawnRemote_HappyPathBroadcastsAndReturns(t *testing.T) {
	withSpawnRemoteStub(t, `{"url":"https://claude.ai/chat/handler-test","sessionId":"handler-test"}`)
	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{
		"task":  "refactor the auth module",
		"title": "auth-refactor",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp spawnRemoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.URL != "https://claude.ai/chat/handler-test" {
		t.Errorf("URL=%q, want claude.ai URL from stub", resp.URL)
	}
	if resp.SessionID != "handler-test" {
		t.Errorf("SessionID=%q, want handler-test", resp.SessionID)
	}
}

func TestRunSpawnRemote_TimeoutSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.sh")
	body := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prev := spawnRemoteBinary
	prevTimeout := spawnRemoteTimeout
	spawnRemoteBinary = path
	spawnRemoteTimeout = 50_000_000 // 50ms in ns
	t.Cleanup(func() {
		spawnRemoteBinary = prev
		spawnRemoteTimeout = prevTimeout
	})
	_, _, err := runSpawnRemote(context.Background(), "task", "")
	if !errors.Is(err, errSpawnRemoteTimeout) {
		t.Fatalf("expected errSpawnRemoteTimeout, got %v", err)
	}
}
