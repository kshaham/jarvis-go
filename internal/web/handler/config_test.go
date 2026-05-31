package handler

// Coverage for config.go: stats, config/settings CRUD, and the secret
// masking helpers (which carry real security behavior worth asserting).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newConfigCtx(t *testing.T) *Context {
	t.Helper()
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		ScheduledTasks: make(map[string]interface{}),
	}
}

func TestGetStats_Shape(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetStats, "/api/stats")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	if resp["sessionCount"] != float64(0) {
		t.Errorf("expected sessionCount=0, got %v", resp["sessionCount"])
	}
}

func TestGetTokenStats_Empty(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetTokenStats, "/api/token-stats")
	stats, _ := resp["stats"].(map[string]interface{})
	if stats["totalTokens"] != float64(0) {
		t.Errorf("expected totalTokens=0, got %v", stats["totalTokens"])
	}
	if stats["sessionCount"] != float64(0) {
		t.Errorf("expected sessionCount=0, got %v", stats["sessionCount"])
	}
}

// TestGetTokenStats_IncludesSDKAndCodex guards the fix for the token-stats
// blind spot: SDK and Codex sessions live in separate registries from
// GetAllSessions, so before the fix their usage never appeared in
// /api/token-stats. Registering one of each must make sessionCount reflect
// them (3 total alongside a PTY session).
func TestGetTokenStats_IncludesSDKAndCodex(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.SessionManager.RegisterSDKSessionForTest(pty.NewSDKSession(pty.SessionConfig{ID: "sdk-1"}))
	ctx.SessionManager.RegisterCodexSessionForTest(pty.NewCodexSession(pty.SessionConfig{ID: "codex-1"}))

	resp := getJSON(t, ctx.GetTokenStats, "/api/token-stats")
	stats, _ := resp["stats"].(map[string]interface{})
	if stats["sessionCount"] != float64(2) {
		t.Fatalf("expected sessionCount=2 (sdk+codex counted), got %v", stats["sessionCount"])
	}
	sessions, _ := stats["sessions"].([]interface{})
	ids := map[string]bool{}
	for _, s := range sessions {
		if m, ok := s.(map[string]interface{}); ok {
			ids[m["sessionId"].(string)] = true
		}
	}
	if !ids["sdk-1"] || !ids["codex-1"] {
		t.Fatalf("expected sdk-1 and codex-1 in token-stats sessions, got %v", ids)
	}
}

func TestGetConfig_Shape(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetConfig, "/api/config")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	if _, ok := resp["config"]; !ok {
		t.Error("expected config key")
	}
}

func TestUpdateConfig_PersistsAndBadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	rec := postJSON(t, ctx.UpdateConfig, "/api/config", map[string]interface{}{"theme": "dark"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ctx.StateStore.GetConfig()["theme"] != "dark" {
		t.Error("config update did not persist")
	}

	// Malformed JSON -> 400.
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	recBad := httptest.NewRecorder()
	ctx.UpdateConfig(recBad, req)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", recBad.Code)
	}
}

func TestGetSettings_MasksSecrets(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"claudeApiKey": "sk-ant-1234567890abcdef",
		"theme":        "dark",
	})
	resp := getJSON(t, ctx.GetSettings, "/api/settings")
	settings, _ := resp["settings"].(map[string]interface{})
	masked, _ := settings["claudeApiKey"].(string)
	if masked == "sk-ant-1234567890abcdef" {
		t.Error("claudeApiKey should be masked in GetSettings response")
	}
	if !strings.Contains(masked, "****") {
		t.Errorf("masked key should contain ****, got %q", masked)
	}
	// Non-secret fields pass through.
	if settings["theme"] != "dark" {
		t.Errorf("theme should pass through, got %v", settings["theme"])
	}
}

func TestMaskSensitiveSettings_NilAndNested(t *testing.T) {
	if MaskSensitiveSettings(nil) != nil {
		t.Error("nil input should return nil")
	}
	out := MaskSensitiveSettings(map[string]interface{}{
		"zaiApiKey": "zai-secret-key-value-here",
		"telegram":  map[string]interface{}{"token": "123456:bot-token-secret", "chatId": "999"},
		"plain":     "visible",
	})
	if out["zaiApiKey"] == "zai-secret-key-value-here" {
		t.Error("zaiApiKey should be masked")
	}
	tg, _ := out["telegram"].(map[string]interface{})
	if tg["token"] == "123456:bot-token-secret" {
		t.Error("telegram.token should be masked")
	}
	if tg["chatId"] != "999" {
		t.Errorf("telegram.chatId should pass through, got %v", tg["chatId"])
	}
	if out["plain"] != "visible" {
		t.Errorf("plain field should pass through, got %v", out["plain"])
	}
}

func TestMaskSecret_Boundaries(t *testing.T) {
	// Empty -> empty.
	if got := maskSecret(""); got != "" {
		t.Errorf("maskSecret(empty)=%q, want empty", got)
	}
	// Non-string -> empty.
	if got := maskSecret(12345); got != "" {
		t.Errorf("maskSecret(int)=%q, want empty", got)
	}
	// Short (< 12) -> ****.
	if got := maskSecret("short"); got != "****" {
		t.Errorf("maskSecret(short)=%q, want ****", got)
	}
	// Long -> first6 + **** + last4.
	got := maskSecret("sk-ant-abcdefghij1234")
	if !strings.HasPrefix(got, "sk-ant") || !strings.HasSuffix(got, "1234") || !strings.Contains(got, "****") {
		t.Errorf("maskSecret(long)=%q, unexpected format", got)
	}
}

func TestUpdateSettings_PersistsAndBadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	rec := postJSON(t, ctx.UpdateSettings, "/api/settings", map[string]interface{}{"defaultWorkingDir": "/srv/x"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ctx.StateStore.GetSettings()["defaultWorkingDir"] != "/srv/x" {
		t.Error("settings update did not persist")
	}

	req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	recBad := httptest.NewRecorder()
	ctx.UpdateSettings(recBad, req)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", recBad.Code)
	}
}

func TestGetSettingsAccounts_MasksKeys(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"provider":     "zai-glm",
		"zaiApiKey":    "zai-1234567890abcdef",
		"openaiApiKey": "sk-openai-1234567890",
	})
	resp := getJSON(t, ctx.GetSettingsAccounts, "/api/settings/accounts")
	if resp["provider"] != "zai-glm" {
		t.Errorf("provider=%v, want zai-glm", resp["provider"])
	}
	if zk, _ := resp["zaiApiKey"].(string); zk == "zai-1234567890abcdef" {
		t.Error("zaiApiKey should be masked")
	}
	// openaiKeyConfigured should be true since a key is set.
	if resp["openaiKeyConfigured"] != true {
		t.Errorf("expected openaiKeyConfigured=true, got %v", resp["openaiKeyConfigured"])
	}
}

func TestCreateSettingsAccount_DropsMaskedKeys(t *testing.T) {
	ctx := newConfigCtx(t)
	// Seed a real key, then re-submit with the masked placeholder. The
	// handler must NOT overwrite the stored key with the mask.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"claudeApiKey": "sk-ant-real-key-value-here"})
	rec := postJSON(t, ctx.CreateSettingsAccount, "/api/settings/accounts", map[string]interface{}{
		"claudeApiKey": "sk-ant****here", // masked placeholder
		"provider":     "claude-api",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ctx.StateStore.GetSettings()["claudeApiKey"] != "sk-ant-real-key-value-here" {
		t.Errorf("masked placeholder should not clobber the real key, got %v", ctx.StateStore.GetSettings()["claudeApiKey"])
	}
	// Non-secret provider field should have been written.
	if ctx.StateStore.GetSettings()["provider"] != "claude-api" {
		t.Error("provider should have been saved")
	}
}

func TestCreateSettingsAccount_SavesNewKey(t *testing.T) {
	ctx := newConfigCtx(t)
	rec := postJSON(t, ctx.CreateSettingsAccount, "/api/settings/accounts", map[string]interface{}{
		"claudeApiKey": "sk-ant-brand-new-key",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ctx.StateStore.GetSettings()["claudeApiKey"] != "sk-ant-brand-new-key" {
		t.Error("a new (unmasked) key should be saved")
	}
}

func TestGetDebugMemory_Shape(t *testing.T) {
	ctx := newConfigCtx(t)
	// GetDebugMemory dereferences OfficeManager.AgentCount(), so wire one up.
	ctx.OfficeManager = office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	resp := getJSON(t, ctx.GetDebugMemory, "/api/debug/memory")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	mem, _ := resp["memory"].(map[string]interface{})
	if _, ok := mem["allocMB"]; !ok {
		t.Error("expected allocMB in memory block")
	}
	counts, _ := resp["counts"].(map[string]interface{})
	if counts["sessions"] != float64(0) {
		t.Errorf("expected 0 sessions, got %v", counts["sessions"])
	}
}

func TestGetSystemStats_Shape(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("GET", "/api/system/stats", nil)
	rec := httptest.NewRecorder()
	ctx.GetSystemStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Body should be valid JSON (metrics.SystemStats output).
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestCleanupState_OK(t *testing.T) {
	ctx := newConfigCtx(t)
	rec := postJSON(t, ctx.CleanupState, "/api/cleanup-state", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}
