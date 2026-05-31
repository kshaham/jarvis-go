package handler

// Round-4 branch fills for OfficeAgentRestart's residual arms:
//
//   - the Codex-session workingDir snapshot (302-305): an agent whose live
//     session is a Codex backend (no SDK session) snapshots cx.WorkingDir.
//   - the ReassignSessionID UUID-collision 500 (353-364): forced by pinning the
//     spawn's freshly generated session ID (via the newSpawnSessionID seam) to a
//     value already registered in the office manager, so the reassign refuses.

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// restartCtxWithAllowlist builds a Context wired for a real codex respawn:
// codex stub on PATH, openai-custom provider (no auth.json staging), and the
// supplied dir on the working-dir allowlist.
func restartCtxWithAllowlist(t *testing.T, dir string) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(100 * time.Millisecond) })
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(t.Context()),
		OfficeManager:  mgr,
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
		"provider":            "openai-custom",
		"openaiApiKey":        "sk-restart",
	})
	return ctx
}

// TestOfficeAgentRestart_CodexWorkingDirSnapshot drives the GetCodexSession
// workingDir-snapshot arm: the agent's live session is a Codex backend, so the
// restart reads cx.WorkingDir (not the worktree path) and respawns on Codex.
func TestOfficeAgentRestart_CodexWorkingDirSnapshot(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-restart-cx"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	codexWD := t.TempDir()
	ctx := restartCtxWithAllowlist(t, codexWD)

	// Register a Codex agent. Its provider drives the codex respawn route.
	ctx.OfficeManager.RegisterAgent("sess-cx-restart", "lead",
		&office.TeamMemberConfig{ID: "lead", Name: "CxLead", Tier: "lead"})
	ag := ctx.OfficeManager.GetAgent("sess-cx-restart")
	ag.SetProvider("openai-custom", "gpt-5.5")
	// Intentionally NO worktree path: the only source of a working dir is the
	// live Codex session, so the GetCodexSession snapshot arm must supply it.

	// Create the live Codex session AT the agent's session ID so GetCodexSession
	// resolves while GetSDKSession does not.
	cx, err := ctx.SessionManager.CreateCodexSession(t.Context(), pty.SessionConfig{
		ID: "sess-cx-restart", WorkingDir: codexWD, Mode: pty.ModeCodexExec, Name: "CxLead",
	})
	if err != nil {
		t.Fatalf("create codex session: %v", err)
	}
	_ = cx

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "sess-cx-restart", "reason": "codex context bloat",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	newID, _ := resp["sessionId"].(string)
	if newID == "" || newID == "sess-cx-restart" {
		t.Fatalf("expected a fresh sessionId, got %q", newID)
	}
	// The respawned agent should be a Codex session at the snapshotted workingDir.
	if got := ctx.OfficeManager.GetAgent(newID); got == nil {
		t.Fatalf("expected reassigned agent at %s", newID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(newID)
		_ = ctx.SessionManager.StopCodexSession("sess-cx-restart")
		time.Sleep(50 * time.Millisecond)
	})
}

// TestOfficeAgentRestart_ReassignCollision500 drives the UUID-collision arm
// (353-364): the spawn's new session ID is pinned (via the newSpawnSessionID
// seam) to an ID already held by another registered agent, so
// ReassignSessionID refuses and the handler returns 500 after stopping the
// orphaned replacement session.
func TestOfficeAgentRestart_ReassignCollision500(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-collide"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	wd := t.TempDir()
	ctx := restartCtxWithAllowlist(t, wd)

	// Agent to restart.
	ctx.OfficeManager.RegisterAgent("sess-orig", "lead",
		&office.TeamMemberConfig{ID: "lead", Name: "Orig", Tier: "lead"})
	ctx.OfficeManager.GetAgent("sess-orig").SetProvider("openai-custom", "gpt-5.5")
	ctx.OfficeManager.GetAgent("sess-orig").SetWorktreePath(wd)

	// A SECOND agent already occupies the ID the spawn will generate, forcing the
	// reassign collision.
	collisionID := "collision-occupied-id"
	ctx.OfficeManager.RegisterAgent(collisionID, "other",
		&office.TeamMemberConfig{ID: "other", Name: "Occupant", Tier: "worker"})

	prev := newSpawnSessionID
	newSpawnSessionID = func() string { return collisionID }
	t.Cleanup(func() { newSpawnSessionID = prev })

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "sess-orig", "reason": "force collision",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on reassign collision, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reassign session id") {
		t.Errorf("expected 'reassign session id' in body, got %s", rec.Body.String())
	}
	// The occupant agent must still be present (the replacement collided with it,
	// it wasn't overwritten).
	if ctx.OfficeManager.GetAgent(collisionID) == nil {
		t.Error("occupant agent should remain after a refused reassign")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(collisionID)
		time.Sleep(50 * time.Millisecond)
	})
}
