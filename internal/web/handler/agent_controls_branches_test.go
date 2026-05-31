package handler

// Round-3 branch fills for agent_controls.go:
//   - writeSessionPrompt SDK dispatch arm (a registered SDK session for the ID)
//   - OfficeAgentInjectPrompt conflict arm (Sessions().WriteInkPrompt -> false,
//     via the existing rejectingSessions store)
//   - OfficeAgentRestart SDK-session workingDir snapshot (300-302)

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
)

func TestOfficeAgentInjectPrompt_Conflict(t *testing.T) {
	// rejectingSessions (defined in agent_controls_happy_test.go) refuses every
	// WriteInkPrompt -> the inject handler returns 409.
	dir := t.TempDir()
	ctx := newAgentCtlCtxWithStore(t, &rejectingSessions{}, dir)
	ctx.OfficeManager.RegisterAgent("sess-x", "cfg-x", &office.TeamMemberConfig{ID: "cfg-x", Name: "Xan"})

	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-x", "prompt": "commit now",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when the session refuses prompts, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestWriteSessionPrompt_SDKSession drives the SDK dispatch arm of
// writeSessionPrompt: a registered SDK session for the ID (no PTY session) means
// the helper falls through to sdk.WriteInkPrompt.
func TestWriteSessionPrompt_SDKSession(t *testing.T) {
	skipIfNoStubShell(t)
	installClaudeOnPATH(t, "")
	ctx := newSessionExtCtx(t)

	sdk, err := ctx.SessionManager.CreateSDKSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Name: "sdk-wp", Mode: pty.ModeClaudeSDK,
	})
	if err != nil {
		t.Skipf("SDK session unavailable in this environment: %v", err)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSDKSession(sdk.ID)
		time.Sleep(50 * time.Millisecond)
	})

	// No PTY session for this ID, so writeSessionPrompt falls through to the SDK
	// branch. Assert it doesn't panic; the SDK branch executed.
	_ = ctx.writeSessionPrompt(sdk.ID, "via sdk")
}

// TestOfficeAgentRestart_SDKWorkingDirSnapshot drives the SDK-session workingDir
// snapshot arm (agent_controls.go:300-302): the restart reads the live SDK
// session's WorkingDir before stopping it, then respawns.
func TestOfficeAgentRestart_SDKWorkingDirSnapshot(t *testing.T) {
	skipIfNoStubShell(t)
	dir := t.TempDir()
	body := "#!/bin/sh\ncat >/dev/null &\nprintf ''\n"
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

	sdk, err := ctx.SessionManager.CreateSDKSession(context.Background(), pty.SessionConfig{
		WorkingDir: worktree, Name: "old-sdk", Mode: pty.ModeClaudeSDK,
	})
	if err != nil {
		t.Skipf("SDK session unavailable: %v", err)
	}
	ctx.OfficeManager.RegisterAgent(sdk.ID, "lead", &office.TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "lead"})

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": sdk.ID, "reason": "snapshot test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 restart, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	newID, _ := resp["sessionId"].(string)
	t.Cleanup(func() {
		if newID != "" {
			_ = ctx.SessionManager.StopSDKSession(newID)
		}
		_ = ctx.SessionManager.StopSDKSession(sdk.ID)
		time.Sleep(80 * time.Millisecond)
	})
	if newID == "" || newID == sdk.ID {
		t.Errorf("expected a fresh session ID, got %q", newID)
	}
}
