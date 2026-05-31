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
