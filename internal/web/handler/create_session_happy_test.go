package handler

// Branch fills for CreateSession's happy path + the defaultWorkingDir override
// branch (settings["defaultWorkingDir"] points at an existing dir, so it wins
// over an empty request workingDir).

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
)

func TestCreateSession_HappyShell(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := newSessionExtCtx(t)
	ctx.OfficeManager = office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	wd := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
	})

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"workingDir": wd,
		"mode":       "shell",
		"name":       "happy",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	id, _ := resp["id"].(string)
	if id == "" {
		// Some responses nest under "session".
		if sess, ok := resp["session"].(map[string]interface{}); ok {
			id, _ = sess["id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("expected a session id in response, got %v", resp)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		time.Sleep(50 * time.Millisecond)
	})
	if _, ok := ctx.SessionManager.GetSession(id); !ok {
		t.Errorf("created session %s not registered in manager", id)
	}
}

func TestCreateSession_DefaultWorkingDirOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := newSessionExtCtx(t)
	ctx.OfficeManager = office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	wd := t.TempDir()
	// defaultWorkingDir (an existing dir) overrides the empty request workingDir.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
		"defaultWorkingDir":   wd,
	})

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"mode": "shell", "name": "defaulted",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 using defaultWorkingDir, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	id, _ := resp["id"].(string)
	if id == "" {
		if sess, ok := resp["session"].(map[string]interface{}); ok {
			id, _ = sess["id"].(string)
		}
	}
	t.Cleanup(func() {
		if id != "" {
			_ = ctx.SessionManager.StopSession(context.Background(), id)
			time.Sleep(50 * time.Millisecond)
		}
	})
	// The created session should be rooted at the resolved default dir.
	if id != "" {
		if sess, ok := ctx.SessionManager.GetSession(id); ok {
			resolved, _ := ctx.ResolveWorkingDir(wd)
			if sess.WorkingDir != resolved {
				t.Errorf("session workingDir=%q, want %q (resolved default)", sess.WorkingDir, resolved)
			}
		}
	}
	_ = pty.ModeShell
}
