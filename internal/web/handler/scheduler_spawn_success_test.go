package handler

// Round-4 fill for executeSpawnTeamJob's success return (scheduler.go:235): a
// fully-resolved spawn_team config drives a real (stub-backed) SDK team spawn
// so recorder.status stays < 400 and the function returns "spawned team: ...".

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/scheduler"
)

func TestExecuteSpawnTeamJob_Success(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-sched-1","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-sched-1","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	installClaudeOnPATH(t, stream)

	ctx := newOfficeExtCtx(t)
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}

	workdir, err := os.MkdirTemp("", "sched-spawn-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
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

	cfg, _ := json.Marshal(map[string]interface{}{
		"workingDir": workdir,
		"templateId": "sched-team",
		"mission":    "scheduled ship",
		"memberOverrides": []map[string]interface{}{
			{"id": "solo", "name": "Solo", "role": "Engineer", "tier": "worker"},
		},
	})

	out, jerr := executeSpawnTeamJob(context.Background(), ctx, &scheduler.Job{Config: cfg})
	if jerr != nil {
		t.Fatalf("expected success, got err=%v out=%q", jerr, out)
	}
	if !strings.Contains(out, "spawned team: sched-team") {
		t.Errorf("expected success summary mentioning templateId, got %q", out)
	}
	if !ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be spawned after a successful scheduled spawn job")
	}
}
