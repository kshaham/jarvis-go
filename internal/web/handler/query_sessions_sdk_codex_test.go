package handler

// Round-3 branch fill for QuerySessions: the SDK-session loop body, the
// Codex-session loop body, and the ascending-sort arm. Requires a live SDK
// session and a live Codex session against stub binaries on PATH.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestQuerySessions_IncludesSDKAndCodexAsc(t *testing.T) {
	skipIfNoStubShell(t)
	installClaudeOnPATH(t, "")
	installCodexOnPATH(t, "")

	ctx := newSessionLiveCtx(t)

	sdk, err := ctx.SessionManager.CreateSDKSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Name: "q-sdk", Mode: pty.ModeClaudeSDK,
	})
	if err != nil {
		t.Skipf("SDK session unavailable: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSDKSession(sdk.ID); time.Sleep(40 * time.Millisecond) })

	cx, err := ctx.SessionManager.CreateCodexSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Name: "q-codex", Mode: pty.ModeCodexExec,
	})
	if err != nil {
		t.Skipf("Codex session unavailable: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopCodexSession(cx.ID); time.Sleep(40 * time.Millisecond) })

	req := httptest.NewRequest("GET", "/api/sessions/query?order=asc&sort=name&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.QuerySessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Total    int                      `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Both the SDK and Codex sessions should surface in the listing.
	names := map[string]bool{}
	for _, s := range resp.Sessions {
		if n, _ := s["name"].(string); n != "" {
			names[n] = true
		}
	}
	if !names["q-sdk"] {
		t.Errorf("SDK session missing from query listing: %v", resp.Sessions)
	}
	if !names["q-codex"] {
		t.Errorf("Codex session missing from query listing: %v", resp.Sessions)
	}
}
