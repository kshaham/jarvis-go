package handler

// Branch fills for the agent-memory section endpoints + getSessionWorkingDir:
//   - PostMemorySection with Append=true (the else branch of the happy path)
//   - PostMemorySection / GetMemorySection when the agent is unknown
//   - getSessionWorkingDir resolving across PTY / SDK / Codex / miss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestOfficeAgentPostMemorySection_AgentNotFound(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-ghost", "name": "log", "content": "x",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentGetMemorySection_UnknownAgentEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/memory/section?sessionId=s-ghost&name=log", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemorySection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty content) for unknown agent, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["content"] != "" {
		t.Errorf("expected empty content for unknown agent, got %v", resp["content"])
	}
}

func TestGetSessionWorkingDir_AllBackends(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := newTimelineContext(t)

	// PTY shell session.
	wdPTY := t.TempDir()
	ptySess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: wdPTY, Mode: pty.ModeShell, Name: "wd-pty",
	})
	if err != nil {
		t.Fatalf("create pty: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), ptySess.ID) })
	if got := ctx.getSessionWorkingDir(ptySess.ID); got != wdPTY {
		t.Errorf("PTY workingDir=%q, want %q", got, wdPTY)
	}

	// SDK session (never started -- registered directly in the manager so the
	// GetSDKSession arm resolves without spawning a subprocess).
	wdSDK := t.TempDir()
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "wd-sdk", WorkingDir: wdSDK, Mode: pty.ModeClaudeSDK})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)
	if got := ctx.getSessionWorkingDir("wd-sdk"); got != wdSDK {
		t.Errorf("SDK workingDir=%q, want %q", got, wdSDK)
	}

	// Codex session: create a real one against a stub `codex` on PATH so the
	// GetCodexSession arm resolves. Stop it in cleanup.
	installCodexOnPATH(t, "{\"type\":\"thread.started\",\"thread_id\":\"wd-th\"}\n")
	wdCodex := t.TempDir()
	cx, cerr := ctx.SessionManager.CreateCodexSession(context.Background(), pty.SessionConfig{
		ID: "wd-codex", WorkingDir: wdCodex, Mode: pty.ModeCodexExec, Name: "wd-codex",
	})
	if cerr != nil {
		t.Fatalf("create codex: %v", cerr)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopCodexSession("wd-codex") })
	_ = cx
	if got := ctx.getSessionWorkingDir("wd-codex"); got != wdCodex {
		t.Errorf("Codex workingDir=%q, want %q", got, wdCodex)
	}

	// Unknown session -> empty.
	if got := ctx.getSessionWorkingDir("nope"); got != "" {
		t.Errorf("unknown session workingDir=%q, want empty", got)
	}
}
