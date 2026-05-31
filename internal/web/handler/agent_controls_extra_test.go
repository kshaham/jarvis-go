package handler

// Coverage for the agent-control validation branches and the writeSessionPrompt
// helper. The full bump/restart happy paths attach SDK sessions (subprocess),
// so these focus on the deterministic validation + not-found guards plus the
// pure session-prompt dispatch.

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
)

func TestOfficeAgentBumpWorkdir_Validation(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "state.json"))

	// Missing sessionId -> 400.
	rec := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"workingDir": "/tmp",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sessionId: expected 400, got %d", rec.Code)
	}

	// Missing workingDir -> 400.
	rec2 := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId": "s1",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("missing workingDir: expected 400, got %d", rec2.Code)
	}

	// Invalid workingDir -> 400 (ResolveWorkingDir rejects).
	rec3 := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId": "s1", "workingDir": "/no/such/dir/ever",
	})
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("invalid workingDir: expected 400, got %d", rec3.Code)
	}
}

func TestOfficeAgentBumpWorkdir_AgentNotFound(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	// Allowlist a real dir so resolution passes and we reach the agent lookup.
	dir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{"workingDirAllowlist": []interface{}{dir}})

	rec := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId": "ghost", "workingDir": dir,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentRestart_Validation(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})

	// Missing sessionId -> 400.
	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sessionId: expected 400, got %d", rec.Code)
	}

	// Unknown agent -> 404.
	rec2 := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "ghost",
	})
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: expected 404, got %d", rec2.Code)
	}
}

func TestOfficeAgentInjectPrompt_Validation(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})

	// Missing sessionId.
	rec := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{"prompt": "p"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sessionId: expected 400, got %d", rec.Code)
	}

	// Missing prompt.
	rec2 := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{"sessionId": "s"})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("missing prompt: expected 400, got %d", rec2.Code)
	}

	// Unknown agent -> 404.
	rec3 := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "ghost", "prompt": "nudge",
	})
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: expected 404, got %d", rec3.Code)
	}
}

// writeSessionPrompt: nil manager -> false; live PTY session -> true.
func TestWriteSessionPrompt(t *testing.T) {
	// Nil session manager returns false.
	bare := &Context{}
	if bare.writeSessionPrompt("x", "p") {
		t.Error("nil manager should return false")
	}

	// Live PTY session accepts the prompt.
	ctx := newSessionExtCtx(t)
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "wp",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	if !ctx.writeSessionPrompt(sess.ID, "hello") {
		t.Error("expected writeSessionPrompt to return true for a live PTY session")
	}
	// Unknown session -> false.
	if ctx.writeSessionPrompt("nope", "hello") {
		t.Error("unknown session should return false")
	}
}

// stopAgentSessionByID no-ops gracefully on an unknown/nil session.
func TestStopAgentSessionByID_Graceful(t *testing.T) {
	bare := &Context{}
	bare.stopAgentSessionByID("x") // nil manager -> no panic

	ctx := newSessionExtCtx(t)
	ctx.stopAgentSessionByID("unknown") // unknown id -> no panic
}