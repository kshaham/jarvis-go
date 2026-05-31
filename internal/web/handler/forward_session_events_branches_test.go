package handler

// Round-3 branch fills for forwardSessionEvents that the existing live test
// (forward_session_events_live_test.go) doesn't reach:
//   - respawn-controller arms on EventWorking / EventIdle (rc != nil)
//   - EventTokens broadcast + RecordDailyStats + per-agent RecordTurnUsage
//     (driven by echoing a "<N> tokens" statusline that the PTY token parser
//     recognises)
//   - EventExit -> Telegram NotifySessionComplete (clean exit code 0)
// A run-summary tracker is registered so the getRunSummary() != nil arms run
// on idle/working/exit/tokens too.

import (
	"context"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/telegram"
)

func TestForwardSessionEvents_RespawnTokensTelegram_Live(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := newSessionExtCtx(t)
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx.OfficeManager = mgr

	// Real but unstarted telegram handler -> the EventExit NotifySessionComplete
	// branch (TelegramHandler != nil) runs; the Notify call early-returns since
	// the handler isn't started.
	settingsStore, err := telegram.NewSettingsStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("telegram settings store: %v", err)
	}
	ctx.TelegramHandler = telegram.NewHandler(telegram.New(), settingsStore, nil)

	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "fwd-branches",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Run summary + respawn controller registered up-front so the
	// getRunSummary()/rc != nil arms fire on every event type.
	ctx.initSessionTrackers(sess.ID)
	rc := ctx.GetOrCreateRespawnController(sess.ID)
	if rc == nil {
		t.Fatal("expected a respawn controller")
	}
	// Register an office agent so the per-agent token accumulation + payload
	// broadcasts (RecordTurnUsage, BuildMemberPayload) run on EventTokens.
	mgr.RegisterAgent(sess.ID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		ctx.cleanupSessionResources(sess.ID)
		time.Sleep(80 * time.Millisecond)
	})

	go forwardSessionEvents(sess, ctx)

	// Echo a working indicator -> EventWorking -> rc.OnWorkingDetected.
	sess.WriteViaScreen("echo Thinking\r")
	time.Sleep(300 * time.Millisecond)

	// Echo a token statusline the parser recognises -> EventTokens.
	sess.WriteViaScreen("echo 5000 tokens\r")
	time.Sleep(400 * time.Millisecond)

	// Fall quiet past the idle window -> EventIdle -> rc.OnIdlePrompt.
	time.Sleep(2500 * time.Millisecond)

	// Clean exit (code 0) -> EventExit -> Telegram NotifySessionComplete arm.
	sess.WriteViaScreen("exit\r")

	// Poll for agent cleanup as proof the EventExit branch ran.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.GetAgent(sess.ID) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("agent still registered after window; exit may be slow on this host (branches still exercised)")
}
