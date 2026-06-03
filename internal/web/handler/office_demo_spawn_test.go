package handler

// Tests for OfficeDemoSpawn — the one-click "Demo Jarvis" endpoint. It is
// a thin preset over spawnTeamFromRequest, so the focus here is on the
// two behaviours unique to the demo: (1) the single-team guard rejects a
// demo while a team is already live, and (2) the happy path spawns the
// 3-member demo-jarvis template into a seeded sandbox dir and tags the
// team so the UI can recognise it.

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
)

// A demo spawn must be refused with 409 when a team is already
// (being) spawned — the demo can never clobber a real team's work.
func TestOfficeDemoSpawn_GuardConflict(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Claim the spawn slot as if a real team were already coming up.
	if !ctx.OfficeManager.TrySetTeamSpawning() {
		t.Fatal("precondition: expected to claim the spawn slot")
	}

	rec := postJSON(t, ctx.OfficeDemoSpawn, "/api/office/demo-spawn", map[string]interface{}{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("demo spawn with team active: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Happy path: the demo spawns the 3-member demo-jarvis template via the
// SDK session mode (skips the slow PTY kickstart goroutines) against a
// stub claude on PATH, seeds the sandbox CLAUDE.md, and records the
// demo-jarvis template tag for the UI.
func TestOfficeDemoSpawn_SDKHappyPath(t *testing.T) {
	if _, err := os.Stat("/app/workspace"); err == nil {
		// resolveDemoWorkspaceDir prefers /app/workspace when present,
		// which wouldn't match the test's narrowed allowlist below.
		t.Skip("/app/workspace exists (container env); demo dir would bypass the test allowlist")
	}

	stream := "" +
		`{"type":"system","subtype":"init","session_id":"c-demo-1","model":"claude-opus-4-8"}` + "\n" +
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-demo-1","usage":{"input_tokens":5,"output_tokens":2}}` + "\n"
	installClaudeOnPATH(t, stream)

	ctx := newOfficeExtCtx(t)
	// cleanupSessionResources (run on shutdown) mutates these maps.
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	// Own the base dir so the async append-system-prompt writes don't race
	// t.TempDir auto-removal. The demo dir is <base>/jarvis-demo, which the
	// allowlist accepts because it's nested under the allowlisted base.
	base, err := os.MkdirTemp("", "demospawn-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{base},
		"defaultWorkingDir":   base,
		"provider":            "claude-subscription",
		"officeSessionMode":   "sdk",
	})

	t.Cleanup(func() {
		_ = shutdownTeamForCleanup(t, ctx)
		for _, a := range ctx.OfficeManager.GetRoster() {
			_ = ctx.SessionManager.StopSDKSession(a.SessionID())
		}
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(base)
	})

	rec := postJSON(t, ctx.OfficeDemoSpawn, "/api/office/demo-spawn", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("demo spawn: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["templateUsed"] != "demo-jarvis" {
		t.Errorf("templateUsed=%v, want demo-jarvis", resp["templateUsed"])
	}
	spawned, _ := resp["spawned"].([]interface{})
	if len(spawned) != 3 {
		t.Fatalf("expected 3 spawned demo agents, got %d: %v", len(spawned), resp)
	}
	if !ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be marked spawned after the demo spawn")
	}
	if got := len(ctx.OfficeManager.GetRoster()); got != 3 {
		t.Errorf("roster size=%d, want 3", got)
	}

	// The team is tagged demo-jarvis (the UI keys "Stop demo" off this)
	// and points at the seeded sandbox dir.
	tplID, workingDir := ctx.OfficeManager.GetSpawnContext()
	if tplID != "demo-jarvis" {
		t.Errorf("spawn-context template=%q, want demo-jarvis", tplID)
	}
	// The tag must also ride on /api/office/status (GetOfficeState) — that's
	// the payload the SPA's useOfficeStatus hook reads to decide "Stop demo".
	if got := ctx.OfficeManager.GetOfficeState()["teamTemplateId"]; got != "demo-jarvis" {
		t.Errorf("GetOfficeState teamTemplateId=%v, want demo-jarvis (UI 'Stop demo' depends on it)", got)
	}
	// ResolveWorkingDir canonicalises symlinks (on macOS /tmp -> /private/tmp),
	// so resolve the expected base the same way before comparing.
	resolvedBase, evErr := filepath.EvalSymlinks(base)
	if evErr != nil {
		resolvedBase = base
	}
	wantDir := filepath.Join(resolvedBase, "jarvis-demo")
	if workingDir != wantDir {
		t.Errorf("working dir=%q, want %q", workingDir, wantDir)
	}
	if _, err := os.Stat(filepath.Join(workingDir, "CLAUDE.md")); err != nil {
		t.Errorf("expected seeded CLAUDE.md in sandbox dir: %v", err)
	}
}
