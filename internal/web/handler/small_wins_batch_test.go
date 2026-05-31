package handler

// Round-2 batch of small reachable branches: monitoring.Status session loop,
// sessionWorkingDir SDK lookup, OfficeAgentStandupResponse bad-JSON, the
// SessionAuto* bad-JSON + immediate-clear/compact/trim arms, and the
// OfficeGetStandupAudio "latest" + ServeFile success path.

import (
	"context"
	"encoding/json"
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

func TestStatus_IncludesRegisteredSession(t *testing.T) {
	ctx := newTestContext()
	// Status's GetAllSessions covers PTY sessions; register one via a live shell
	// so the serialization loop runs over a real entry.
	skipIfNoStubShell(t)
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "stat-pty",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	rec := httptest.NewRecorder()
	ctx.Status(rec, httptest.NewRequest("GET", "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	sessions, _ := resp["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session in status, got %d", len(sessions))
	}
}

func TestSessionWorkingDir_SDKSession(t *testing.T) {
	ctx := &Context{SessionManager: pty.NewSessionManager(10)}
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "wd-sdk", WorkingDir: "/work/sdk", Mode: pty.ModeClaudeSDK})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	dir, ok := ctx.sessionWorkingDir("wd-sdk")
	if !ok {
		t.Fatal("expected sessionWorkingDir to resolve a registered SDK session")
	}
	if dir != "/work/sdk" {
		t.Errorf("workingDir=%q, want /work/sdk", dir)
	}
	// Unknown id -> (",", false).
	if _, ok := ctx.sessionWorkingDir("nope"); ok {
		t.Error("expected false for unknown session id")
	}
}

func TestOfficeAgentStandupResponse_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/standup/response", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentStandupResponse(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

// autoSessionCtx builds a Context with a live shell session and returns it + id.
func autoSessionCtx(t *testing.T) (*Context, string) {
	t.Helper()
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "auto",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		time.Sleep(40 * time.Millisecond)
	})
	return ctx, sess.ID
}

func autoReq(t *testing.T, id, body string) *http.Request {
	t.Helper()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+id+"/auto", strings.NewReader(body)), "id", id)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestSessionAutoClear_BadJSONAndImmediateClear(t *testing.T) {
	ctx, id := autoSessionCtx(t)

	// Bad JSON -> 400.
	recBad := httptest.NewRecorder()
	ctx.SessionAutoClear(recBad, autoReq(t, id, "{bad"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", recBad.Code)
	}

	// Enabled with threshold 0 -> immediate /clear, 200.
	recOK := httptest.NewRecorder()
	ctx.SessionAutoClear(recOK, autoReq(t, id, `{"enabled":true,"threshold":0}`))
	if recOK.Code != http.StatusOK {
		t.Fatalf("immediate clear: expected 200, got %d: %s", recOK.Code, recOK.Body.String())
	}
}

func TestSessionAutoCompact_BadJSONAndImmediate(t *testing.T) {
	ctx, id := autoSessionCtx(t)
	recBad := httptest.NewRecorder()
	ctx.SessionAutoCompact(recBad, autoReq(t, id, "{bad"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", recBad.Code)
	}
	recOK := httptest.NewRecorder()
	ctx.SessionAutoCompact(recOK, autoReq(t, id, `{"enabled":true,"threshold":0}`))
	if recOK.Code != http.StatusOK {
		t.Fatalf("immediate compact: expected 200, got %d: %s", recOK.Code, recOK.Body.String())
	}
}

func TestSessionAutoTrim_BadJSONAndImmediate(t *testing.T) {
	ctx, id := autoSessionCtx(t)
	recBad := httptest.NewRecorder()
	ctx.SessionAutoTrim(recBad, autoReq(t, id, "{bad"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", recBad.Code)
	}
	recOK := httptest.NewRecorder()
	ctx.SessionAutoTrim(recOK, autoReq(t, id, `{"enabled":true,"threshold":0}`))
	if recOK.Code != http.StatusOK {
		t.Fatalf("immediate trim: expected 200, got %d: %s", recOK.Code, recOK.Body.String())
	}
}

func TestSessionPlanTask_BadJSON400(t *testing.T) {
	ctx, id := autoSessionCtx(t)
	recBad := httptest.NewRecorder()
	ctx.SessionPlanTask(recBad, autoReq(t, id, "{bad"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", recBad.Code)
	}
}

// OfficeGetStandupAudio: the "latest" resolution + ServeFile success path.
func TestOfficeGetStandupAudio_LatestServesFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{SSEBroker: sse.NewBroker(100, 10)}

	// Persist a completed standup so LatestCompletedStandupID resolves it.
	ps := office.PersistedStandup{ID: "latest1", Participants: []string{"a"}, CompletedAt: time.Now().UnixMilli()}
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}
	pb, _ := json.Marshal(ps)
	if err := os.WriteFile(filepath.Join(dir, "latest1.json"), pb, 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}
	// Write the generated audio file so ServeFile has something to serve.
	if err := os.WriteFile(office.StandupAudioPath("latest1"), []byte("ID3fakeaudio"), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupAudio(rec, standupAudioReq("GET", "latest"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 serving latest audio, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q, want audio/mpeg", ct)
	}
}
