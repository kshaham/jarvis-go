package handler

// Drives spawnTeamFromRequest end-to-end through the OfficeSpawnTeam HTTP
// handler with a single synthetic member, SDK session mode (the default, which
// skips the slow PTY kickstart-injection goroutines), and a stub `claude`
// binary on PATH. Covers the validation -> resolve -> spawn-loop ->
// FinalizeTeamSpawn happy path plus the post-spawn response. The team is shut
// down in cleanup so monitoring goroutines + the SDK session don't leak.

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
)

func TestSpawnTeamFromRequest_SDKHappyPath(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-team-1","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-team-1","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	installClaudeOnPATH(t, stream)

	ctx := newOfficeExtCtx(t)
	// cleanupSessionResources (run on shutdown) mutates these maps.
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	// Working dir must pass the allowlist; widen it to a dir we manage
	// ourselves (the SDK session writes an append-system-prompt file into it
	// asynchronously, which races Go's t.TempDir auto-removal — so we own the
	// dir and clean it best-effort AFTER the team is fully torn down).
	workdir, err := os.MkdirTemp("", "spawnteam-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "claude-subscription",
		"officeSessionMode":   "sdk",
	})

	t.Cleanup(func() {
		// Tear the team down so monitoring goroutines + the SDK session stop,
		// then stop any straggler SDK sessions before removing the workdir.
		_ = shutdownTeamForCleanup(t, ctx)
		for _, a := range ctx.OfficeManager.GetRoster() {
			_ = ctx.SessionManager.StopSDKSession(a.SessionID())
		}
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(workdir)
	})

	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir":    workdir,
		"mission":       "ship the feature",
		"perpetual":     false,
		"memberOverrides": []map[string]interface{}{
			{"id": "solo", "name": "Solo", "role": "Engineer", "tier": "worker"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("spawn team: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	spawned, _ := resp["spawned"].([]interface{})
	if len(spawned) != 1 {
		t.Fatalf("expected 1 spawned agent, got %d: %v", len(spawned), resp)
	}
	if !ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be marked spawned after a successful spawn")
	}
	// The roster should carry the single agent.
	if got := len(ctx.OfficeManager.GetRoster()); got != 1 {
		t.Errorf("roster size=%d, want 1", got)
	}
}

// shutdownTeamForCleanup drives OfficeShutdownTeam to tear down the spawned team.
func shutdownTeamForCleanup(t *testing.T, ctx *Context) bool {
	t.Helper()
	rec := postJSON(t, ctx.OfficeShutdownTeam, "/api/office/shutdown", map[string]interface{}{})
	return rec.Code == http.StatusOK
}

// TestSpawnTeamFromRequest_NewProjectInvalidName400 drives the NewProject
// branch with an invalid slug -> office.ErrInvalidProjectName -> 400.
func TestSpawnTeamFromRequest_NewProjectInvalidName400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"newProject":  true,
		"projectName": "../evil",
		"mission":     "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid project name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSpawnTeamFromRequest_NewProjectMkdirFails500 drives the NewProject branch
// with a valid slug whose target (/app/workspace/<slug>) can't be created in
// the local (non-container) environment -> 500.
func TestSpawnTeamFromRequest_NewProjectMkdirFails500(t *testing.T) {
	if _, err := os.Stat("/app/workspace"); err == nil {
		t.Skip("/app/workspace exists (container env); mkdir would succeed")
	}
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"newProject":  true,
		"projectName": "valid-slug",
		"mission":     "x",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when /app/workspace mkdir fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSpawnTeamFromRequest_MissingWorkingDir asserts the 400 guard for an
// empty workingDir (and not NewProject).
func TestSpawnTeamFromRequest_MissingWorkingDir(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"mission": "no dir",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = context.Background()
}

// TestSpawnTeamFromRequest_HooksMode drives the officeSessionMode="hooks" path:
// it pre-assigns session IDs, sets up per-agent worktrees + settings.local.json
// (falling back to the shared workingDir for a non-git dir), and spawns each
// member via spawnAgentSessionWithHooks against a stub `claude` on PATH.
func TestSpawnTeamFromRequest_HooksMode(t *testing.T) {
	skipIfNoStubShell(t)
	ctx := newOfficeExtCtx(t)
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	// A hooks-mode `claude` stub that just reads stdin forever (stays alive).
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/claude", []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("install hooks claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	workdir, err := os.MkdirTemp("", "spawnhooks-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "claude-subscription",
		"officeSessionMode":   "hooks",
	})

	t.Cleanup(func() {
		_ = shutdownTeamForCleanup(t, ctx)
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(workdir)
	})

	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir": workdir,
		"mission":    "hooks mission",
		"memberOverrides": []map[string]interface{}{
			{"id": "h1", "name": "Hooky", "role": "Engineer", "tier": "worker"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("hooks spawn: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	spawned, _ := resp["spawned"].([]interface{})
	if len(spawned) != 1 {
		t.Fatalf("expected 1 hooks-mode agent spawned, got %d: %v", len(spawned), resp)
	}
}

// TestSpawnTeamFromRequest_AllSpawnsFailRollback drives the rollback path:
// with no claude binary reachable, every member fails to spawn, len(spawned)==0,
// the slot is released, and the handler returns 500.
func TestSpawnTeamFromRequest_AllSpawnsFailRollback(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	workdir, err := os.MkdirTemp("", "spawnfail-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workdir) })

	// Empty PATH so neither the SDK nor the PTY fallback can resolve `claude`.
	emptyPath := t.TempDir()
	t.Setenv("PATH", emptyPath)

	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "claude-subscription",
		"officeSessionMode":   "legacy", // raw PTY -> needs `claude` on PATH, which is gone
	})

	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir": workdir,
		"mission":    "doomed spawn",
		"memberOverrides": []map[string]interface{}{
			{"id": "solo", "name": "Solo", "role": "Engineer", "tier": "worker"},
		},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when all spawns fail, got %d: %s", rec.Code, rec.Body.String())
	}
	// The slot was released, so the team is NOT considered spawned.
	if ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should not be spawned after a full rollback")
	}
}

// TestSpawnTeamFromRequest_AlreadySpawnedConflict asserts the 409 path: a
// second spawn while a team is already claimed returns conflict.
func TestSpawnTeamFromRequest_AlreadySpawnedConflict(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	workdir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
	})
	// Claim the spawn slot directly so the next request collides.
	if !ctx.OfficeManager.TrySetTeamSpawning() {
		t.Fatal("could not claim spawn slot for setup")
	}
	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir": workdir,
		"mission":    "second",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when team already spawning, got %d: %s", rec.Code, rec.Body.String())
	}
}
