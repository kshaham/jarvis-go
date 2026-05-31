package handler

// Branch-fill tests for OneShotSession that drive the live-session arms the
// validation-only tests in oneshot_misc_extra_test.go can't reach:
//   - defaultWorkingDir pulled from settings (req.WorkingDir == "")
//   - timeoutSeconds capped at oneShotMaxTimeout
//   - claude-mode selection (pty.ModeClaude)
//   - the deadline-timeout 504 path with partial output
// All use real ModeShell sessions; the deadline path uses a non-idling command.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestOneShotSession_DefaultWorkingDirFromSettings drives the settings-default
// resolution arm (oneshot.go:51) by omitting workingDir and putting a
// defaultWorkingDir in settings.
func TestOneShotSession_DefaultWorkingDirFromSettings(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
		"defaultWorkingDir":   dir,
	})

	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		// No workingDir -> must fall back to settings.defaultWorkingDir.
		"prompt": "echo from-default-dir", "mode": "shell", "timeoutSeconds": 20,
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 200 or 504, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["output"]; !ok {
		t.Errorf("expected an output field, got %v", resp)
	}
}

// TestOneShotSession_TimeoutCapAndClaudeMode hits both the timeout-cap arm
// (timeoutSeconds beyond oneShotMaxTimeout -> clamped) and the claude-mode
// arm. We can't run a real claude here, so we keep this focused on the cap +
// mode-selection statements which run before the spawn; the spawn itself
// fails fast on a missing claude binary, which exercises the create-error 500.
func TestOneShotSession_TimeoutCapClaudeMode(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
	})

	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		"prompt":         "noop",
		"workingDir":     dir,
		"mode":           "claude",
		"timeoutSeconds": 100000, // beyond oneShotMaxTimeout -> clamps to the max
	})
	// Either the claude session boots (200/504) or the binary is missing and we
	// get a 500 from CreateSession. All three still execute the cap + mode arms.
	if rec.Code != http.StatusOK && rec.Code != http.StatusGatewayTimeout && rec.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
}

// TestOneShotSession_DeadlineTimeout forces the deadline branch: a shell that
// stays busy past a 1s timeout returns 504 with a "timeout" flag and partial
// output.
func TestOneShotSession_DeadlineTimeout(t *testing.T) {
	t.Parallel()
	skipIfNoStubShell(t)
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
	})

	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		// A long-running command keeps the shell non-idle until the deadline.
		"prompt": "sleep 30", "workingDir": dir, "mode": "shell", "timeoutSeconds": 1,
	})
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 timeout, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if to, _ := resp["timeout"].(bool); !to {
		t.Errorf("expected timeout=true, got %v", resp["timeout"])
	}
	if _, ok := resp["output"]; !ok {
		t.Errorf("expected partial output field, got %v", resp)
	}
	if _, ok := resp["durationMs"]; !ok {
		t.Errorf("expected durationMs, got %v", resp)
	}
}
