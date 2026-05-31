package handler

// Round-4 fills for CloneSession's residual arms:
//   - empty source name -> "session" baseName fallback (42-44)
//   - CreateSession failure for a clone -> errs entry (62-64), forced by a
//     capacity-1 manager already saturated by the source session.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/telegram"
)

func TestCloneSession_EmptyNameAndCreateFailure(t *testing.T) {
	skipIfNoStubShell(t)
	ctx := newSessionLiveCtx(t)
	// Replace the manager with a capacity-1 one so the source fills the only
	// slot and every clone's CreateSession fails.
	ctx.SessionManager = pty.NewSessionManager(1)

	// Source session with an EMPTY name -> the baseName == "" fallback fires.
	src, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	ctx.initSessionTrackers(src.ID)
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), src.ID)
		ctx.cleanupSessionResources(src.ID)
		time.Sleep(50 * time.Millisecond)
	})

	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+src.ID+"/clone?count=2", nil), "id", src.ID)
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	clones, _ := resp["sessions"].([]interface{})
	errs, _ := resp["errors"].([]interface{})
	// All clones fail (manager saturated) so sessions is empty and errors has 2.
	if len(clones) != 0 {
		t.Errorf("expected 0 successful clones with a saturated manager, got %d", len(clones))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 create-failure errors, got %d (%v)", len(errs), errs)
	}
}

// TestCloneSession_NotifiesTelegram drives the TelegramHandler != nil notify arm
// (70-72): a successful clone with a (disconnected) TelegramHandler wired calls
// NotifySessionStart, which no-ops safely when the bot isn't connected.
func TestCloneSession_NotifiesTelegram(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	settingsStore, err := telegram.NewSettingsStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("telegram settings store: %v", err)
	}
	ctx.TelegramHandler = telegram.NewHandler(telegram.New(), settingsStore, nil)

	srcID, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+srcID+"/clone?count=1", nil), "id", srcID)
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	clones, _ := resp["sessions"].([]interface{})
	if len(clones) != 1 {
		t.Fatalf("expected 1 clone, got %d", len(clones))
	}
	cm, _ := clones[0].(map[string]interface{})
	cloneID, _ := cm["id"].(string)
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), cloneID)
		ctx.cleanupSessionResources(cloneID)
		time.Sleep(50 * time.Millisecond)
	})
}
