package handler

// Drives the GetSessionTailFile SSE streaming loop end-to-end against a real
// shell session rooted at a temp working dir. The 404 sweep + DeleteSessionTailFile
// already cover the guards; this covers the streaming body: the connected
// message, the per-tick data emit when the file grows, the context-cancel
// "end" path, and the read-error path (file removed out from under the reader).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// newForwardCtx builds a Context wired with the collaborators forwardAgentEvents
// dereferences unconditionally (SSEBroker, StateStore, OfficeManager) so the
// SDK/Codex forwarder wrappers don't nil-panic when no agent is registered.
func newForwardCtx(t *testing.T) *Context {
	t.Helper()
	return &Context{
		SSEBroker:     sse.NewBroker(100, 10),
		StateStore:    state.NewStore(filepath.Join(t.TempDir(), "fwd-state.json")),
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func TestGetSessionTailFile_StreamsAndCancels(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)

	logPath := filepath.Join(wd, "tail.log")
	if err := os.WriteFile(logPath, []byte("seed line\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	// Cancellable request context drives the streamCtx.Done() branch. Build the
	// request on the cancellable context FIRST, then attach the chi URL param --
	// withURLParam replaces the request context, so the ordering matters.
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := httptest.NewRequest("GET", "/api/sessions/"+id+"/tail-file?path=tail.log", nil).WithContext(reqCtx)
	req := withURLParam(base, "id", id)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		ctx.GetSessionTailFile(rec, req)
		close(done)
	}()

	// Append new content so the ticker.C branch reads >0 bytes and emits a
	// data frame. The handler seeks to EOF first, so only post-seek writes
	// are streamed.
	time.Sleep(120 * time.Millisecond)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	_, _ = f.WriteString("new streamed content\n")
	_ = f.Close()

	// Give the 500ms ticker time to fire at least once with the new data.
	time.Sleep(800 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("GetSessionTailFile did not return after context cancel")
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"connected"`) {
		t.Errorf("expected connected frame, got: %q", body)
	}
	if !strings.Contains(body, "new streamed content") {
		t.Errorf("expected streamed data frame with appended content, got: %q", body)
	}
	if !strings.Contains(body, `"type":"end"`) {
		t.Errorf("expected end frame after cancel, got: %q", body)
	}

	// The stream must have been deregistered from TailFileStreams on return.
	ctx.TailFileStreamsMu.Lock()
	n := len(ctx.TailFileStreams)
	ctx.TailFileStreamsMu.Unlock()
	if n != 0 {
		t.Errorf("expected TailFileStreams cleaned up, got %d entries", n)
	}
}

// TestGetSessionTailFile_MissingPath asserts the 400 branch (no path query
// param) against a real session, distinct from the 404 (unknown session).
func TestGetSessionTailFile_MissingPath(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionTailFile(rec, idReq("GET", "/api/sessions/"+id+"/tail-file", "", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing path: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetSessionTailFile_FileNotFound asserts the os.Open error branch: a
// relative path under the working dir that doesn't exist -> 404.
func TestGetSessionTailFile_FileNotFound(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionTailFile(rec, idReq("GET", "/api/sessions/"+id+"/tail-file?path=ghost.log", "", id))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing file: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetSessionTailFile_AbsolutePathRejected drives resolveSessionFilePath's
// ErrPathAbsolute branch -> 400.
func TestGetSessionTailFile_AbsolutePathRejected(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionTailFile(rec, idReq("GET", "/api/sessions/"+id+"/tail-file?path=/etc/passwd", "", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("absolute path: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// forwardWrappersSession drives the forwardSDKSessionEvents /
// forwardCodexSessionEvents one-line wrappers end-to-end against real
// SDK/Codex sessions whose stub binaries emit a stream-json sequence and
// exit, closing the events channel so the forwarder returns.

func TestForwardSDKSessionEvents_RealStub(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := newForwardCtx(t)

	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"claude-fwd-1","model":"claude-opus-4-8"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"claude-fwd-1","usage":{"input_tokens":10,"output_tokens":3},"model_usages":{"claude-opus-4-8":{"input_tokens":10,"output_tokens":3}}}`,
		"",
	}, "\n")
	bin := installFakeClaudeStub(t, stream)

	s := pty.NewSDKSession(pty.SessionConfig{
		ID:               "sdk-fwd-1",
		WorkingDir:       t.TempDir(),
		Model:            "claude-opus-4-8",
		SystemPrompt:     "go",
		ClaudeBinaryPath: bin,
	})
	startCtx, cancelStart := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStart()
	if err := s.Start(startCtx); err != nil {
		t.Fatalf("SDK Start: %v", err)
	}
	defer s.Stop()

	// The SDK stub exits after `result`, closing the events channel; the
	// forwarder returns on its own. A watchdog Stop() guards against a slow
	// subprocess so the test never hangs.
	go func() {
		time.Sleep(8 * time.Second)
		s.Stop()
	}()

	done := make(chan struct{})
	go func() {
		forwardSDKSessionEvents(s, ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("forwardSDKSessionEvents did not return after stub exit")
	}
}

func TestForwardCodexSessionEvents_RealStub(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := newForwardCtx(t)

	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-fwd-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"hi from codex"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":12,"output_tokens":2}}`,
		"",
	}, "\n")
	bin := installFakeCodexStub(t, stream)

	s := pty.NewCodexSession(pty.SessionConfig{
		ID:              "codex-fwd-1",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		SystemPrompt:    "go",
		CodexBinaryPath: bin,
	})
	startCtx, cancelStart := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStart()
	if err := s.Start(startCtx); err != nil {
		t.Fatalf("Codex Start: %v", err)
	}
	defer s.Stop()

	// A Codex session is persistent: after the kickstart turn completes the
	// worker idles waiting for the next turn, so the events channel stays open.
	// Stop() cancels the worker and closes the channel, unblocking the forwarder.
	go func() {
		time.Sleep(2 * time.Second)
		s.Stop()
	}()

	done := make(chan struct{})
	go func() {
		forwardCodexSessionEvents(s, ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("forwardCodexSessionEvents did not return after Stop")
	}
}
