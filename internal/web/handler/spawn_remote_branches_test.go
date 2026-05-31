package handler

// Round-2 branch fills for OfficeSpawnRemote / runSpawnRemote: malformed-JSON
// 400, oversize-title 400, CLI-error 502 (non-zero exit -> spawnRemoteCLIError),
// and the timeout 504 arm through the HTTP handler.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

func TestOfficeSpawnRemote_MalformedJSON400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	req := httptest.NewRequest(http.MethodPost, "/api/office/spawn-remote", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeSpawnRemote(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeSpawnRemote_OversizeTitle400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{
		"task":  "do a thing",
		"title": strings.Repeat("t", maxSpawnRemoteTitleLen+1),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize title, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("error should mention 'too long': %s", rec.Body.String())
	}
}

// TestOfficeSpawnRemote_CLIError502 drives the CLI-failure arm: the stub exits
// non-zero, so runSpawnRemote returns a *spawnRemoteCLIError (not a timeout),
// and the handler maps it to 502 with a generic, credential-safe message.
func TestOfficeSpawnRemote_CLIError502(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	prev := spawnRemoteBinary
	spawnRemoteBinary = stubbin.FailStub(t, 3, "Authorization: Bearer SECRET-leak\nboom")
	t.Cleanup(func() { spawnRemoteBinary = prev })

	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{"task": "do a thing"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on CLI failure, got %d: %s", rec.Code, rec.Body.String())
	}
	// The credential-bearing stderr must NOT be echoed to the client.
	if strings.Contains(rec.Body.String(), "Bearer") || strings.Contains(rec.Body.String(), "SECRET-leak") {
		t.Errorf("response leaked CLI stderr/credentials: %s", rec.Body.String())
	}
}

// TestOfficeSpawnRemote_Timeout504 drives the timeout arm end-to-end through
// the HTTP handler: a slow stub + a tiny timeout -> errSpawnRemoteTimeout -> 504.
func TestOfficeSpawnRemote_Timeout504(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prevBin := spawnRemoteBinary
	prevTO := spawnRemoteTimeout
	spawnRemoteBinary = path
	spawnRemoteTimeout = 50_000_000 // 50ms
	t.Cleanup(func() {
		spawnRemoteBinary = prevBin
		spawnRemoteTimeout = prevTO
	})

	ctx := newSnapshotCtx(t)
	rec := spawnRemoteHandlerReq(t, ctx, map[string]interface{}{"task": "do a thing"})
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 on timeout, got %d: %s", rec.Code, rec.Body.String())
	}
}
