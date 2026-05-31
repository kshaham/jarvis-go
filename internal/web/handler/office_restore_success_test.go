package handler

// Round-2 branch fill for OfficeRestoreSnapshot's success path: a valid snapshot
// whose working dir passes the allowlist, restored through the full
// spawnTeamFromRequest pipeline against a stub `claude` binary. Drives the
// post-spawn arms (ApplyPostSpawnTaskQueues, pause restore, snapshotRestored
// broadcast) that only run when the inner spawn returns < 400.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestOfficeRestoreSnapshot_SDKSuccess(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-restore-1","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-restore-1","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	t.Setenv("HOME", t.TempDir()) // isolate snapshot + office-memory writes
	installClaudeOnPATH(t, stream)

	// Own the working dir (the SDK writes an append-system-prompt file into it
	// asynchronously, racing t.TempDir auto-removal).
	workdir, err := os.MkdirTemp("", "restore-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}

	ctx := &Context{
		SessionManager:          pty.NewSessionManager(10),
		SSEBroker:               sse.NewBroker(100, 10),
		StateStore:              state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:           office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
		RalphTrackers:           map[string]*ralph.Tracker{},
		RunSummaries:            map[string]*runsummary.Tracker{},
		RespawnControllers:      map[string]*respawn.Controller{},
		SessionColors:           map[string]string{},
		ActivePlanOrchestrators: map[string]string{},
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "claude-subscription",
		"officeSessionMode":   "sdk",
	})

	t.Cleanup(func() {
		_ = shutdownTeamForCleanup(t, ctx)
		for _, a := range ctx.OfficeManager.GetRoster() {
			_ = ctx.SessionManager.StopSDKSession(a.SessionID())
		}
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(workdir)
	})

	// Seed a paused snapshot with one member + a queued task so the
	// ApplyPostSpawnTaskQueues + pause-restore arms both run.
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:        "restore-me",
		WorkingDir:  workdir,
		TeamMission: "resume the mission",
		Paused:      true,
		PauseReason: "operator paused",
		Members: []office.SnapshotMemberEntry{
			{
				ConfigID: "solo",
				Config:   office.TeamMemberConfig{ID: "solo", Name: "Solo", Role: "Engineer", Tier: "worker"},
				Tasks:    []*office.Task{{ID: "t-1", Title: "finish auth", Status: office.TaskPending}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots/restore-me/restore", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "restore-me")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeRestoreSnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful restore, got %d: %s", rec.Code, rec.Body.String())
	}
	if !ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be spawned after a successful restore")
	}
	// Pause state from the snapshot should have been reapplied.
	if !ctx.OfficeManager.IsPaused() {
		t.Error("expected the restored team to be paused (snapshot was paused)")
	}
}
