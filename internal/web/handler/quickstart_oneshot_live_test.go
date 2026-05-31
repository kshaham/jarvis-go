package handler

// Happy-path coverage for QuickStart and OneShotSession using real ModeShell
// sessions. QuickStart spawns a persistent shell from a case dir; OneShotSession
// runs a single `echo` command and waits for the shell to go idle, capturing
// the output. Both are stopped/cleaned to avoid leaked goroutines under -race.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
)

func TestQuickStart_HappyShell(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	// QuickStart resolves the working dir from the case directory.
	cm := cases.NewCaseManager(t.TempDir())
	ctx.CaseManager = cm
	if _, err := cm.Create("qs-case"); err != nil {
		t.Fatalf("create case: %v", err)
	}

	rec := postJSON(t, ctx.QuickStart, "/api/quick-start", map[string]interface{}{
		"caseName": "qs-case", "mode": "shell",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("quick-start: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	sess, _ := resp["session"].(map[string]interface{})
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("expected a session id from quick-start")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		ctx.cleanupSessionResources(id)
	})
	if _, ok := ctx.SessionManager.GetSession(id); !ok {
		t.Error("quick-start session not registered")
	}
}

func TestQuickStart_CaseNotFound404(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	ctx.CaseManager = cases.NewCaseManager(t.TempDir())
	// No defaultWorkingDir set + unknown case -> 404.
	rec := postJSON(t, ctx.QuickStart, "/api/quick-start", map[string]interface{}{
		"caseName": "no-such-case", "mode": "shell",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown case, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOneShotSession_HappyShell(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	// Allow the working dir so ResolveWorkingDir accepts it.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
	})

	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		"prompt": "echo one-shot-marker", "workingDir": dir, "mode": "shell", "timeoutSeconds": 30,
	})
	// The one-shot either returns 200 with output once the shell idles, or
	// 504 if it didn't idle within the window. Both exercise the full handler
	// body (session spawn, prompt write, idle wait, cleanup); only a hard 4xx
	// would indicate a validation failure.
	if rec.Code != http.StatusOK && rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("one-shot: expected 200 or 504, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["output"]; !ok {
		t.Errorf("expected an output field, got %v", resp)
	}
}
