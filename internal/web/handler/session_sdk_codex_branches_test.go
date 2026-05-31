package handler

// Round-2 branch fills for ListSessions + GetSessionTerminal's SDK and Codex
// arms. SDK sessions are registered via the manager's test seam; Codex sessions
// are spawned for real against a stub `codex` binary on PATH so their list +
// terminal-buffer accessors run.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestListSessions_IncludesSDKAndCodex(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-list"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}

	// Register an SDK session via the test seam.
	sdk := pty.NewSDKSession(pty.SessionConfig{
		ID: "sdk-list", WorkingDir: "/tmp/sdk", Mode: pty.ModeClaudeSDK, Name: "SdkList",
	})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	// Spawn a real (stub-backed) Codex session.
	member := teamtemplates.TeamTemplateMember{ID: "cx", Name: "CodexList", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship"}
	codexID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-test"},
		nil, false, nil, "openai-custom", "gpt-5.5")
	if mode != "codex" || codexID == "" {
		t.Fatalf("expected codex session, got mode=%q id=%q", mode, codexID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(codexID)
		time.Sleep(50 * time.Millisecond)
	})

	rec := httptest.NewRecorder()
	ctx.ListSessions(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := map[string]bool{}
	for _, s := range arr {
		if id, ok := s["id"].(string); ok {
			ids[id] = true
		}
	}
	if !ids["sdk-list"] {
		t.Error("ListSessions should include the registered SDK session")
	}
	if !ids[codexID] {
		t.Error("ListSessions should include the live Codex session")
	}
}

func TestGetSessionTerminal_SDKSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
	}
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "sdk-term", Mode: pty.ModeClaudeSDK, Name: "SdkTerm"})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	rec := httptest.NewRecorder()
	ctx.GetSessionTerminal(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/sdk-term/terminal", nil), "id", "sdk-term"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SDK terminal, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["terminalBuffer"]; !ok {
		t.Error("expected terminalBuffer field in SDK terminal response")
	}
}

func TestGetSessionTerminal_CodexSession(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-term"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	member := teamtemplates.TeamTemplateMember{ID: "cxt", Name: "CodexTerm", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship"}
	codexID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-test"},
		nil, false, nil, "openai-custom", "gpt-5.5")
	if mode != "codex" || codexID == "" {
		t.Fatalf("expected codex session, got mode=%q id=%q", mode, codexID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(codexID)
		time.Sleep(50 * time.Millisecond)
	})

	rec := httptest.NewRecorder()
	ctx.GetSessionTerminal(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+codexID+"/terminal", nil), "id", codexID))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for Codex terminal, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetSessionTerminal_TailTruncates drives the tail-truncation arm: a live
// shell session that emits a chunk of output, then a tail=1 request slices it.
func TestGetSessionTerminal_TailTruncates(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "tailer",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	go forwardSessionEvents(sess, ctx)
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		time.Sleep(50 * time.Millisecond)
	})

	// Emit output to fill the terminal buffer.
	sess.WriteToPTY("echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")

	// Poll until the buffer has more than 1 byte, then request tail=1.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(sess.TerminalBuffer()) > 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(sess.TerminalBuffer()) <= 1 {
		t.Skip("shell did not produce terminal output in time; tail-truncation not exercisable")
	}

	rec := httptest.NewRecorder()
	ctx.GetSessionTerminal(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+sess.ID+"/terminal?tail=1", nil), "id", sess.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["truncated"] != true {
		t.Errorf("expected truncated=true with tail=1 against a multi-byte buffer, got %v", resp["truncated"])
	}
	if buf, _ := resp["terminalBuffer"].(string); len(buf) != 1 {
		t.Errorf("expected a 1-byte tail buffer, got %d bytes", len(buf))
	}
}
