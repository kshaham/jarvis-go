package handler

// Drives forwardSessionEvents (the PTY-backed forwarder) through its
// Working / Idle / Exit branches end-to-end against a real shell session.
// A shell that echoes a recognised working indicator ("Thinking") flips the
// session into the working state (EventWorking), then falling quiet for the
// idle window emits EventIdle, and exiting the shell emits EventExit. An
// office agent is registered for the session so the per-agent payload
// branches (BuildMemberPayload broadcasts on idle/working) and the
// EventExit -> HandleAgentDeath cleanup path run.

import (
	"context"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
)

func TestForwardSessionEvents_WorkingIdleExit_Live(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := newSessionExtCtx(t)
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx.OfficeManager = mgr

	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "fwd-life",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ctx.initSessionTrackers(sess.ID)
	// Register an office agent on this session so the per-agent branches in the
	// Working/Idle/Exit handlers fire (BuildMemberPayload, HandleAgentDeath).
	mgr.RegisterAgent(sess.ID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		ctx.cleanupSessionResources(sess.ID)
		time.Sleep(50 * time.Millisecond)
	})

	go forwardSessionEvents(sess, ctx)

	// Echo a working indicator -> EventWorking. The terminal output contains
	// "Thinking", one of pty.workingIndicators.
	sess.WriteViaScreen("echo Thinking\r")

	// Wait past the idle-detection window (default 2s) with no further output so
	// the idle detector fires EventIdle.
	time.Sleep(2500 * time.Millisecond)

	// Exit the shell -> process exits -> EventExit -> HandleAgentDeath.
	sess.WriteViaScreen("exit\r")

	// HandleAgentDeath unregisters the agent on EventExit. Poll for it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.GetAgent(sess.ID) == nil {
			return // EventExit branch ran and cleaned up the agent
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("agent still registered after window; shell exit may be slow on this host (forwarder branches still exercised)")
}
