package handler

// Round-3 branch fills for spawnTeamFromRequest that the SDK/hooks/rollback
// live tests don't reach:
//   - Z.AI model allocation (provider=zai-glm -> AllocateZAIModels +
//     per-member zai env build + zaiSonnet resolution)
//   - TelegramHandler.NotifySessionStart per spawned member
//   - the post-spawn Telegram config block (req.TelegramEnabled)
//   - per-member workingDir pre-validation failure (a member override pointing
//     at a non-allowlisted dir -> 400 before any spawn)

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/telegram"
)

func initSpawnMaps(ctx *Context) {
	ctx.RalphTrackers = map[string]*ralph.Tracker{}
	ctx.RunSummaries = map[string]*runsummary.Tracker{}
	ctx.RespawnControllers = map[string]*respawn.Controller{}
	ctx.SessionColors = map[string]string{}
	ctx.ActivePlanOrchestrators = map[string]string{}
}

// TestSpawnTeamFromRequest_ZAIWithTelegram drives the Z.AI allocation arms plus
// the Telegram-enabled config block. Two members so AllocateZAIModels has a real
// distribution to compute and the per-member zai env build runs for each.
func TestSpawnTeamFromRequest_ZAIWithTelegram(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"c-zai","model":"glm-5.1"}` + "\n" +
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-zai","usage":{"input_tokens":5,"output_tokens":2}}` + "\n"
	installClaudeOnPATH(t, stream)

	ctx := newOfficeExtCtx(t)
	initSpawnMaps(ctx)

	// Real but unstarted telegram handler -> NotifySessionStart per member runs
	// (early-returns since unstarted) and the SetTelegramConfig block executes.
	settingsStore, err := telegram.NewSettingsStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("telegram settings store: %v", err)
	}
	ctx.TelegramHandler = telegram.NewHandler(telegram.New(), settingsStore, nil)

	workdir, err := os.MkdirTemp("", "spawnzai-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "zai-glm",
		"zaiApiKey":           "zk-test-key",
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

	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir":          workdir,
		"mission":             "zai ship",
		"perpetual":           false,
		"telegramEnabled":     true,
		"telegramIntervalMin": 30,
		"memberOverrides": []map[string]interface{}{
			{"id": "z1", "name": "Zee", "role": "Engineer", "tier": "worker"},
			{"id": "z2", "name": "Zed", "role": "Reviewer", "tier": "worker"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("zai spawn: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	spawned, _ := resp["spawned"].([]interface{})
	if len(spawned) != 2 {
		t.Fatalf("expected 2 spawned zai agents, got %d: %v", len(spawned), resp)
	}
	// The Telegram config block must have flipped the office telegram flag on.
	enabled, interval, _ := ctx.OfficeManager.GetTelegramConfig()
	if !enabled {
		t.Error("expected office telegram config enabled after telegramEnabled spawn")
	}
	if interval != 30 {
		t.Errorf("expected telegram interval 30, got %d", interval)
	}
}

// TestSpawnTeamFromRequest_PerMemberWorkingDirRejected drives the up-front
// per-member workingDir validation failure (office.go:689-693): a member override
// pins a workingDir outside the allowlist -> 400 before any agent spawns, and the
// spawn slot is released.
func TestSpawnTeamFromRequest_PerMemberWorkingDirRejected(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	initSpawnMaps(ctx)

	workdir, err := os.MkdirTemp("", "spawnpmwd-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workdir) })

	// Allowlist ONLY workdir; the member override points elsewhere.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{workdir},
		"provider":            "claude-subscription",
		"officeSessionMode":   "sdk",
	})

	rec := postJSON(t, ctx.OfficeSpawnTeam, "/api/office/spawn", map[string]interface{}{
		"workingDir": workdir,
		"mission":    "bad per-member dir",
		"memberOverrides": []map[string]interface{}{
			{"id": "m1", "name": "Mal", "role": "Engineer", "tier": "worker",
				"workingDir": "/definitely/not/allowlisted/anywhere"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-allowlisted per-member dir, got %d: %s", rec.Code, rec.Body.String())
	}
	// Slot released -> team not spawned, so a retry is possible.
	if ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should not be spawned after per-member workingDir rejection")
	}
}
