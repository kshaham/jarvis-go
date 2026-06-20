package handler

// Coverage for OfficeShutdownTeam's teardown callback PTY-stop branch, which
// only runs when a registered agent is backed by a live session in the manager.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
)

func TestOfficeShutdownTeam_StopsLivePTYAgent(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// cleanupSessionResources (called by the teardown callback) mutates these
	// maps, so initialize them to avoid a nil-map panic.
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	// Real PTY shell session, registered as an office agent. Shutdown's
	// teardown callback should find it via GetSession and StopSession it.
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "shutdown-agent",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx.initSessionTrackers(sess.ID)
	ctx.OfficeManager.RegisterAgent(sess.ID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.SetTeamSpawned(true)

	rec := postJSON(t, ctx.OfficeShutdownTeam, "/api/office/shutdown", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("shutdown: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The live session should have been stopped by the teardown callback.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ctx.SessionManager.GetSession(sess.ID); !ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := ctx.SessionManager.GetSession(sess.ID); ok {
		// Best-effort cleanup if it somehow survived.
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		t.Error("expected the agent's PTY session to be stopped by shutdown teardown")
	}
	if ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be torn down (not spawned) after shutdown")
	}
}

// TestOfficeShutdownTeam_StopsOrphanSession verifies the backstop: a session in
// the manager that is NOT in the team roster (e.g. a member orphaned by a
// reassign/respawn, or one stuck in a provider-error retry loop) survives the
// roster-only TeardownTeam callback, but is still stopped by the StopAllSessions
// sweep added after teardown -- so no agent session lingers after shutdown.
func TestOfficeShutdownTeam_StopsOrphanSession(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	// A registered roster agent (stopped by the teardown callback).
	rosterSess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "roster-agent",
	})
	if err != nil {
		t.Fatalf("create roster session: %v", err)
	}
	ctx.initSessionTrackers(rosterSess.ID)
	ctx.OfficeManager.RegisterAgent(rosterSess.ID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// An ORPHAN session: live in the manager but NOT registered with the office,
	// so TeardownTeam never visits it. Only the StopAllSessions backstop catches it.
	orphan, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "orphan-agent",
	})
	if err != nil {
		t.Fatalf("create orphan session: %v", err)
	}
	ctx.OfficeManager.SetTeamSpawned(true)

	rec := postJSON(t, ctx.OfficeShutdownTeam, "/api/office/shutdown", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("shutdown: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Both the roster agent AND the orphan must be gone from the manager.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, rOK := ctx.SessionManager.GetSession(rosterSess.ID)
		_, oOK := ctx.SessionManager.GetSession(orphan.ID)
		if !rOK && !oOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := ctx.SessionManager.GetSession(orphan.ID); ok {
		_ = ctx.SessionManager.StopSession(context.Background(), orphan.ID)
		t.Error("expected the orphan (non-roster) session to be stopped by the shutdown backstop")
	}
	if _, ok := ctx.SessionManager.GetSession(rosterSess.ID); ok {
		_ = ctx.SessionManager.StopSession(context.Background(), rosterSess.ID)
		t.Error("expected the roster agent session to be stopped")
	}
}
