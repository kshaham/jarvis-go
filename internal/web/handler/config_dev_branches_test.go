package handler

// Branch fills: the env-var path of GetSettingsAccounts' openaiKeyConfigured,
// and the dev-credentials round-trip + dev-instance conflict on the office
// agent endpoints.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSettingsAccounts_OpenAIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-only-key")
	ctx := newConfigCtx(t)
	// No openaiApiKey setting, but the env var is present -> configured=true.
	resp := getJSON(t, ctx.GetSettingsAccounts, "/api/settings/accounts")
	if resp["openaiKeyConfigured"] != true {
		t.Errorf("openaiKeyConfigured=%v, want true when OPENAI_API_KEY env is set", resp["openaiKeyConfigured"])
	}
}

func TestGetSettingsAccounts_OpenAIKeyUnset(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetSettingsAccounts, "/api/settings/accounts")
	if resp["openaiKeyConfigured"] != false {
		t.Errorf("openaiKeyConfigured=%v, want false with no key/env", resp["openaiKeyConfigured"])
	}
}

func TestOfficeAgentDevCredentials_RoundTripBranch(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Register a dev instance slot first (credentials attach to a slot).
	recInst := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance?force=true", map[string]interface{}{
		"name": "app", "url": "http://127.0.0.1:5173", "pid": float64(0),
	})
	if recInst.Code != http.StatusOK {
		t.Fatalf("post dev instance (forced): expected 200, got %d: %s", recInst.Code, recInst.Body.String())
	}

	// Post credentials for that slot.
	recCred := postJSON(t, ctx.OfficeAgentPostDevCredentials, "/api/office/agent/dev-credentials", map[string]interface{}{
		"name": "app", "credentials": "user: admin / pass: secret",
	})
	if recCred.Code != http.StatusOK {
		t.Fatalf("post dev credentials: expected 200, got %d: %s", recCred.Code, recCred.Body.String())
	}

	// Read them back.
	req := httptest.NewRequest("GET", "/api/office/agent/dev-credentials?name=app", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetDevCredentials(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get dev credentials: expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if creds, _ := resp["credentials"].(string); creds == "" {
		t.Error("expected non-empty credentials after round-trip")
	}
}

func TestOfficeAgentPostDevInstance_LocalhostRejected(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "app", "url": "http://localhost:3000",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("localhost without force: expected 400, got %d", rec.Code)
	}
}
