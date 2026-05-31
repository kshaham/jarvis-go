package handler

// Branch fill for OfficeAgentRestart's success path: a registered agent with a
// worktree path restarts onto a fresh SDK session against a stub `claude` on
// PATH. Covers the spawn -> RegisterAgentWithProvider -> 200 tail the existing
// failure-path tests don't reach.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeAgentRestart_HappySDK(t *testing.T) {
	skipIfNoStubShell(t)
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-restart","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-restart","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	// Install a claude stub on PATH (named exactly "claude").
	dir := t.TempDir()
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null &\nprintf '%s' '" + escaped + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(body), 0o755); err != nil {
		t.Fatalf("install claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	worktree := t.TempDir()
	ctx := newAgentCtlCtxWithStore(t, &recordingSessions{}, worktree)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{worktree},
		"provider":            "claude-subscription",
		"officeSessionMode":   "sdk",
	})
	ctx.OfficeManager.RegisterAgent("sess-restart", "lead", &office.TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "lead"})
	ctx.OfficeManager.GetAgent("sess-restart").SetWorktreePath(worktree)

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "sess-restart", "reason": "context bloated",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful restart, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	newID, _ := resp["sessionId"].(string)
	if newID == "" || newID == "sess-restart" {
		t.Errorf("expected a fresh sessionId, got %q", newID)
	}
	t.Cleanup(func() {
		if newID != "" {
			_ = ctx.SessionManager.StopSDKSession(newID)
			time.Sleep(50 * time.Millisecond)
		}
	})
}
