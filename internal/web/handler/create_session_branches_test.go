package handler

// Branch-fill tests for CreateSession's working-dir resolution arms that the
// existing happy/default tests don't reach:
//   - account-settings array iteration ("accounts" -> [].defaultWorkingDir)
//   - top-level "accountDefaultWorkingDir" key
//   - defaultWorkingDir set but NOT present on disk (warn arm, no override)
//   - claude mode selection
//   - Telegram NotifySessionStart forwarding (TelegramHandler != nil)

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/telegram"
)

func newCreateSessionCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ctx := newSessionExtCtx(t)
	ctx.OfficeManager = office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })
	return ctx
}

func stopSessionFromResp(t *testing.T, ctx *Context, resp map[string]interface{}) {
	t.Helper()
	id, _ := resp["id"].(string)
	if id == "" {
		if sess, ok := resp["session"].(map[string]interface{}); ok {
			id, _ = sess["id"].(string)
		}
	}
	if id != "" {
		t.Cleanup(func() {
			_ = ctx.SessionManager.StopSession(context.Background(), id)
			time.Sleep(40 * time.Millisecond)
		})
	}
}

// TestCreateSession_AccountSettingsArrayDefaultDir drives the
// settings["accounts"] []interface{} iteration that pulls defaultWorkingDir
// from the last account entry (session.go:252-263).
func TestCreateSession_AccountSettingsArrayDefaultDir(t *testing.T) {
	ctx := newCreateSessionCtx(t)
	wd := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
		"accounts": []interface{}{
			map[string]interface{}{"defaultWorkingDir": ""},  // skipped (empty)
			map[string]interface{}{"defaultWorkingDir": wd},  // wins (last non-empty)
			map[string]interface{}{"name": "no-dir-account"}, // no defaultWorkingDir key
		},
	})

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"mode": "shell", "name": "from-account-array",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 using account defaultWorkingDir, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	stopSessionFromResp(t, ctx, resp)
	id, _ := resp["id"].(string)
	if id == "" {
		if s, ok := resp["session"].(map[string]interface{}); ok {
			id, _ = s["id"].(string)
		}
	}
	if id != "" {
		if sess, ok := ctx.SessionManager.GetSession(id); ok {
			resolved, _ := ctx.ResolveWorkingDir(wd)
			if sess.WorkingDir != resolved {
				t.Errorf("session workingDir=%q, want resolved account dir %q", sess.WorkingDir, resolved)
			}
		}
	}
}

// TestCreateSession_AccountDefaultWorkingDirTopLevel drives the
// settings["accountDefaultWorkingDir"] top-level fallback (session.go:266-268).
func TestCreateSession_AccountDefaultWorkingDirTopLevel(t *testing.T) {
	ctx := newCreateSessionCtx(t)
	wd := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist":      []interface{}{wd},
		"accountDefaultWorkingDir": wd,
	})

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"mode": "shell", "name": "from-account-toplevel",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	stopSessionFromResp(t, ctx, decodeMap(t, rec))
}

// TestCreateSession_DefaultDirNotOnDisk drives the "defaultWorkingDir not found
// on disk" warn arm (session.go:277-279): defaultDir is set but does not exist,
// so it must NOT override the (valid) request workingDir.
func TestCreateSession_DefaultDirNotOnDisk(t *testing.T) {
	ctx := newCreateSessionCtx(t)
	wd := t.TempDir()
	missing := wd + "/does-not-exist-subdir"
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
		"defaultWorkingDir":   missing, // not on disk -> warn, no override
	})

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"workingDir": wd, "mode": "shell", "name": "fallback-to-request",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 (request dir wins when default missing), got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	stopSessionFromResp(t, ctx, resp)
	id, _ := resp["id"].(string)
	if id == "" {
		if s, ok := resp["session"].(map[string]interface{}); ok {
			id, _ = s["id"].(string)
		}
	}
	if id != "" {
		if sess, ok := ctx.SessionManager.GetSession(id); ok {
			resolved, _ := ctx.ResolveWorkingDir(wd)
			if sess.WorkingDir != resolved {
				t.Errorf("session should use request dir %q, got %q", resolved, sess.WorkingDir)
			}
		}
	}
}

// TestCreateSession_TelegramNotifyAndClaudeMode drives the claude-mode arm
// (session.go:296-298) and the Telegram NotifySessionStart forwarding arm
// (session.go:326-328). The telegram handler is real but unstarted, so the
// Notify call early-returns after the nil-check branch has been taken.
func TestCreateSession_TelegramNotifyAndClaudeMode(t *testing.T) {
	skipIfNoStubShell(t)
	// Put a claude stub on PATH so the claude-mode session boots cleanly.
	installClaudeOnPATH(t, "")

	ctx := newCreateSessionCtx(t)
	wd := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
	})
	settingsStore, err := telegram.NewSettingsStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("telegram settings store: %v", err)
	}
	ctx.TelegramHandler = telegram.NewHandler(telegram.New(), settingsStore, nil)

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"workingDir": wd, "mode": "claude", "name": "claude-telegram",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 for claude session, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	stopSessionFromResp(t, ctx, resp)
}
