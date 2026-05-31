package handler

// Round 5 -- residual monitoring_extended.go arms:
//   - AuthSetup: the post-lock "already configured between checks" TOCTOU
//     arm, driven deterministically through the afterSetupLockAcquired seam.
//   - AuthLogin: the VerifyPassword IO/parse-error -> 500 arm (corrupt
//     auth.json), and a happy-path 200 that sets a session cookie.
//   - DeleteScreen: the success arm with a screen pre-registered on disk so
//     KillScreen returns (true, nil) without a live `screen` binary.
//   - GetSubagentTranscript: the blank-line-skip arm in the JSONL parser.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/auth"
)

func TestAuthSetup_ConfiguredBetweenChecks400(t *testing.T) {
	dir := t.TempDir()
	mgr, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := &Context{AuthManager: mgr, StateStore: state.NewStore(filepath.Join(dir, "state.json"))}

	// While AuthSetup holds the setup lock (after the first IsPasswordConfigured
	// returned false), a password gets configured -> the post-lock double-check
	// must observe it and return 400.
	orig := afterSetupLockAcquired
	t.Cleanup(func() { afterSetupLockAcquired = orig })
	afterSetupLockAcquired = func() {
		if err := mgr.SetPassword("raceconfigured123"); err != nil {
			t.Errorf("concurrent SetPassword: %v", err)
		}
	}

	rec := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{
		"password": "hunter22hunter22",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when password configured between checks, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != "Password already configured" {
		t.Errorf("error=%v, want 'Password already configured'", resp["error"])
	}
}

func TestAuthLogin_VerifyPasswordError500(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.SetPassword("hunter22hunter22"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	// Corrupt auth.json so VerifyPassword's json.Unmarshal errors -> 500.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	ctx := &Context{AuthManager: mgr, StateStore: state.NewStore(filepath.Join(dir, "state.json"))}

	rec := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{
		"password": "hunter22hunter22",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when auth config unparseable, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthLogin_HappyPathSetsCookie(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.SetPassword("hunter22hunter22"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	ctx := &Context{AuthManager: mgr, StateStore: state.NewStore(filepath.Join(dir, "state.json"))}

	rec := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{
		"password": "hunter22hunter22",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Error("expected a session cookie to be set on successful login")
	}
}

func TestDeleteScreen_SuccessKillsRegistered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Seed a screens.json with a PID-0 entry so purgeOrphans keeps it
	// (only PID>0 dead processes are pruned) and KillScreen finds it.
	screens := []map[string]interface{}{{
		"sessionId":  "scr-1",
		"screenName": "jarvis-scr-1",
		"pid":        0,
		"createdAt":  "2024-01-01T00:00:00Z",
		"workingDir": dir,
		"mode":       "claude",
	}}
	data, _ := json.Marshal(screens)
	if err := os.WriteFile(filepath.Join(dir, "screens.json"), data, 0o644); err != nil {
		t.Fatalf("seed screens.json: %v", err)
	}
	sm := pty.NewScreenManager(dir)
	if _, ok := sm.GetScreen("scr-1"); !ok {
		t.Fatalf("setup: screen scr-1 not loaded")
	}

	ctx := &Context{ScreenManager: sm}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/screens/scr-1", nil), "sessionId", "scr-1")
	rec := httptest.NewRecorder()
	ctx.DeleteScreen(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	// The screen should be gone from the manager after a successful kill.
	if _, ok := sm.GetScreen("scr-1"); ok {
		t.Error("expected screen removed after DeleteScreen")
	}
}

func TestGetSubagentTranscript_BlankLineSkipped(t *testing.T) {
	t.Parallel()
	// An embedded blank line between two JSON records must be skipped, not
	// emitted as an entry.
	jsonl := "{\"type\":\"user\",\"content\":\"a\"}\n\n{\"type\":\"assistant\",\"content\":\"b\"}\n"
	w := watcherWithTranscript(t, "blankid", jsonl)
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}

	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, transcriptReq("blankid", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries, _ := resp["transcript"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 entries (blank line skipped), got %d: %v", len(entries), entries)
	}
	raw, _ := resp["raw"].(string)
	if !strings.Contains(raw, "\n\n") {
		t.Error("raw should preserve the blank line")
	}
}
