package handler

// Branch coverage for executeSpawnTeamJob's resolve-error and spawn-failure
// paths, plus BuildSchedulerExecutor's spawn_team dispatch. The happy spawn
// path is covered by the spawn_team live test; here we drive the two error
// exits that don't need a real claude subprocess.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/scheduler"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func schedulerSpawnCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Context{
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func TestExecuteSpawnTeamJob_ResolveWorkingDirError(t *testing.T) {
	ctx := schedulerSpawnCtx(t)
	// A path outside the allowlist fails ResolveWorkingDir.
	cfg, _ := json.Marshal(map[string]interface{}{"workingDir": "/etc/definitely-not-allowed"})
	_, err := executeSpawnTeamJob(context.Background(), ctx, &scheduler.Job{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "invalid workingDir") {
		t.Fatalf("expected invalid workingDir error, got %v", err)
	}
}

func TestExecuteSpawnTeamJob_SpawnFailureBubblesHTTPStatus(t *testing.T) {
	ctx := schedulerSpawnCtx(t)
	workdir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
	})
	// Pre-claim the spawn slot so spawnTeamFromRequest returns 409 Conflict,
	// driving the recorder.status >= 400 branch.
	if !ctx.OfficeManager.TrySetTeamSpawning() {
		t.Fatal("could not pre-claim spawn slot")
	}
	cfg, _ := json.Marshal(map[string]interface{}{"workingDir": workdir})
	out, err := executeSpawnTeamJob(context.Background(), ctx, &scheduler.Job{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "spawn_team failed: HTTP 409") {
		t.Fatalf("expected spawn_team HTTP 409 failure, got out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "already been spawned") {
		t.Errorf("expected conflict body in output, got %q", out)
	}
}

func TestBuildSchedulerExecutor_DispatchesSpawnTeam(t *testing.T) {
	ctx := schedulerSpawnCtx(t)
	exec := BuildSchedulerExecutor(ctx)
	// Missing workingDir -> executeSpawnTeamJob returns its required-field error,
	// proving the ActionSpawnTeam dispatch arm routed here.
	cfg, _ := json.Marshal(map[string]interface{}{"templateId": "t"})
	_, err := exec(context.Background(), &scheduler.Job{Action: scheduler.ActionSpawnTeam, Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "workingDir is required") {
		t.Fatalf("expected spawn_team dispatch to executeSpawnTeamJob, got %v", err)
	}
}

func TestBuildSchedulerExecutor_DispatchesOneShot(t *testing.T) {
	ctx := schedulerSpawnCtx(t)
	exec := BuildSchedulerExecutor(ctx)
	cfg, _ := json.Marshal(map[string]interface{}{"prompt": "hi"})
	_, err := exec(context.Background(), &scheduler.Job{Action: scheduler.ActionOneShot, Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "one_shot action not yet supported") {
		t.Fatalf("expected one_shot dispatch, got %v", err)
	}
}
