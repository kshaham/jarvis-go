package handler

// Coverage for the Telegram, hook-event, task-gate, Run, and generate-plan
// handlers in misc.go. These either short-circuit on a nil dependency
// (Telegram/scheduler-style "service unavailable" branches) or are pure,
// deterministic transforms (hook signature verification, plan markdown).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/telegram"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// --- maskToken (pure) ---

func TestMaskToken_Boundaries(t *testing.T) {
	t.Parallel()
	if got := maskToken("abc"); got != "***" {
		t.Errorf("maskToken(short)=%q, want ***", got)
	}
	if got := maskToken("12345678"); got != "********" {
		t.Errorf("maskToken(8 chars)=%q, want 8 stars", got)
	}
	got := maskToken("1234567890ab")
	if !strings.HasPrefix(got, "1234") || !strings.HasSuffix(got, "90ab") || !strings.Contains(got, "*") {
		t.Errorf("maskToken(long)=%q unexpected format", got)
	}
}

// --- Telegram: nil-bot short circuits ---

func TestGetTelegramStatus_NilBot(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	resp := getJSON(t, ctx.GetTelegramStatus, "/api/settings/telegram")
	if resp["connected"] != false {
		t.Errorf("connected=%v, want false", resp["connected"])
	}
	tg, _ := resp["telegram"].(map[string]interface{})
	if tg["connected"] != false {
		t.Errorf("telegram.connected=%v, want false", tg["connected"])
	}
}

func TestGetTelegramStatus_ConfiguredMasksToken(t *testing.T) {
	t.Parallel()
	ctx := &Context{TelegramBot: telegram.New()}
	ctx.TelegramBot.Configure("123456789:bot-secret-token", "chat-99")
	resp := getJSON(t, ctx.GetTelegramStatus, "/api/settings/telegram")
	if resp["connected"] != true {
		t.Errorf("connected=%v, want true", resp["connected"])
	}
	tg, _ := resp["telegram"].(map[string]interface{})
	if tg["chatId"] != "chat-99" {
		t.Errorf("chatId=%v, want chat-99", tg["chatId"])
	}
	if tg["token"] == "123456789:bot-secret-token" {
		t.Error("token must be masked in status response")
	}
	if mt, _ := tg["token"].(string); !strings.Contains(mt, "*") {
		t.Errorf("masked token should contain *, got %q", mt)
	}
}

func TestUpdateTelegramConfig_NilBot503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.UpdateTelegramConfig, "/api/settings/telegram", map[string]interface{}{"token": "t", "chatId": "c"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when bot nil, got %d", rec.Code)
	}
}

func TestUpdateTelegramConfig_PersistsNonSecretOnly(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		TelegramBot: telegram.New(),
		StateStore:  state.NewStore(t.TempDir() + "/state.json"),
	}
	rec := postJSON(t, ctx.UpdateTelegramConfig, "/api/settings/telegram", map[string]interface{}{
		"token":  "987654321:another-secret",
		"chatId": "room-7",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !ctx.TelegramBot.IsConnected() {
		t.Error("bot should be connected after configure")
	}
	// R27: only the non-secret bits land in state.json -- never the token.
	tg, _ := ctx.StateStore.GetSettings()["telegram"].(map[string]interface{})
	if tg == nil {
		t.Fatal("telegram settings not persisted")
	}
	if tg["chatId"] != "room-7" {
		t.Errorf("chatId=%v, want room-7", tg["chatId"])
	}
	if _, leaked := tg["token"]; leaked {
		t.Error("bot token leaked into state.json (R27 regression)")
	}
}

func TestUpdateTelegramConfig_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{TelegramBot: telegram.New(), StateStore: state.NewStore(t.TempDir() + "/s.json")}
	req := httptest.NewRequest("POST", "/api/settings/telegram", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateTelegramConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendTelegramTest_NotConfigured400(t *testing.T) {
	t.Parallel()
	// Nil bot.
	ctx := &Context{}
	rec := postJSON(t, ctx.SendTelegramTest, "/api/telegram/test", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with nil bot, got %d", rec.Code)
	}
	// Bot present but disconnected.
	ctx2 := &Context{TelegramBot: telegram.New()}
	rec2 := postJSON(t, ctx2.SendTelegramTest, "/api/telegram/test", map[string]interface{}{})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with disconnected bot, got %d", rec2.Code)
	}
}

func TestConnectTelegram_NilBot503AndConfigure(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.ConnectTelegram, "/api/telegram/send-test", map[string]interface{}{"token": "t", "chatId": "c"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil bot, got %d", rec.Code)
	}

	ctx2 := &Context{
		TelegramBot: telegram.New(),
		StateStore:  state.NewStore(t.TempDir() + "/s.json"),
	}
	rec2 := postJSON(t, ctx2.ConnectTelegram, "/api/telegram/send-test", map[string]interface{}{
		"token": "111:tok", "chatId": "c1",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	resp := decodeMap(t, rec2)
	if resp["connected"] != true {
		t.Errorf("connected=%v, want true", resp["connected"])
	}
}

func TestConnectTelegram_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{TelegramBot: telegram.New(), StateStore: state.NewStore(t.TempDir() + "/s.json")}
	req := httptest.NewRequest("POST", "/api/telegram/send-test", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.ConnectTelegram(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDisconnectTelegram_NilAndHappy(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.DisconnectTelegram, "/api/telegram/disconnect", map[string]interface{}{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil bot, got %d", rec.Code)
	}

	ctx2 := &Context{TelegramBot: telegram.New(), StateStore: state.NewStore(t.TempDir() + "/s.json")}
	ctx2.TelegramBot.Configure("1:t", "c")
	rec2 := postJSON(t, ctx2.DisconnectTelegram, "/api/telegram/disconnect", map[string]interface{}{})
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	resp := decodeMap(t, rec2)
	if resp["connected"] != false {
		t.Errorf("connected=%v, want false after disconnect", resp["connected"])
	}
	if ctx2.TelegramBot.IsConnected() {
		t.Error("bot should report disconnected")
	}
}

// --- GetHookConfig ---

func TestGetHookConfig_DefaultEmptyAndSeeded(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	resp := getJSON(t, ctx.GetHookConfig, "/api/hook-config")
	hc, ok := resp["hookConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("hookConfig should be object, got %T", resp["hookConfig"])
	}
	if len(hc) != 0 {
		t.Errorf("expected empty hook config, got %v", hc)
	}

	ctx.StateStore.UpdateSettings(map[string]interface{}{"hookConfig": map[string]interface{}{"stop": true}})
	resp2 := getJSON(t, ctx.GetHookConfig, "/api/hook-config")
	hc2, _ := resp2["hookConfig"].(map[string]interface{})
	if hc2["stop"] != true {
		t.Errorf("hookConfig.stop=%v, want true", hc2["stop"])
	}
}

// --- HandleHookEvent: signature gating + event allowlist ---

// hookCtx builds a Context with a real AuthManager so signatures verify
// against a known signing key.
func hookCtx(t *testing.T) (*Context, []byte) {
	t.Helper()
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", "") // force derived-key path
	am, err := auth.NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	ctx := &Context{
		AuthManager: am,
		SSEBroker:   sse.NewBroker(100, 10),
		StateStore:  state.NewStore(t.TempDir() + "/s.json"),
	}
	return ctx, am.HookSigningKey()
}

func signHook(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestHandleHookEvent_MissingSignature401(t *testing.T) {
	ctx, _ := hookCtx(t)
	body, _ := json.Marshal(map[string]interface{}{"event": "stop"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing signature, got %d", rec.Code)
	}
}

func TestHandleHookEvent_BadSignature401(t *testing.T) {
	ctx, _ := hookCtx(t)
	body, _ := json.Marshal(map[string]interface{}{"event": "stop"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", "sha256=deadbeef")
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", rec.Code)
	}
}

func TestHandleHookEvent_ValidSignatureAllowedEvent(t *testing.T) {
	ctx, key := hookCtx(t)
	body, _ := json.Marshal(map[string]interface{}{"event": "stop", "sessionId": "s1"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", signHook(key, body))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for signed allowed event, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
}

func TestHandleHookEvent_UnknownEventType400(t *testing.T) {
	ctx, key := hookCtx(t)
	body, _ := json.Marshal(map[string]interface{}{"event": "evil_inject"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", signHook(key, body))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed event type, got %d", rec.Code)
	}
}

func TestHandleHookEvent_InvalidJSONAfterValidSig400(t *testing.T) {
	ctx, key := hookCtx(t)
	raw := []byte("{not json")
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(raw)))
	req.Header.Set("X-Hook-Signature", signHook(key, raw))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandleHookEvent_ForwardsStopToRespawnController(t *testing.T) {
	ctx, key := hookCtx(t)
	// Register a respawn controller for the session; a valid stop hook
	// should be forwarded to it via OnStopHook without panicking and the
	// request should succeed.
	ctx.RespawnControllers = map[string]*respawn.Controller{
		"s-fwd": respawn.NewController("s-fwd", respawn.DefaultRespawnConfig()),
	}
	body, _ := json.Marshal(map[string]interface{}{"event": "idle_prompt", "sessionId": "s-fwd"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", signHook(key, body))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The controller recorded the idle-prompt signal (confidence > 0).
	status := ctx.RespawnControllers["s-fwd"].GetStatus()
	if status == nil {
		t.Fatal("expected controller status after forwarded hook")
	}
}

func TestHandleHookEvent_StopForwardedToController(t *testing.T) {
	ctx, key := hookCtx(t)
	// The "stop" event takes the OnStopHook forwarding branch (distinct from
	// idle_prompt), so register a controller and fire a signed stop hook.
	ctx.RespawnControllers = map[string]*respawn.Controller{
		"s-stop": respawn.NewController("s-stop", respawn.DefaultRespawnConfig()),
	}
	body, _ := json.Marshal(map[string]interface{}{"event": "stop", "sessionId": "s-stop"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", signHook(key, body))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleHookEvent_SubagentEventBroadcastOnly(t *testing.T) {
	ctx, key := hookCtx(t)
	// A subagent_started event with no sessionID skips both the respawn and
	// Telegram forwarding paths and is broadcast via SSE only.
	body, _ := json.Marshal(map[string]interface{}{"event": "subagent_started"})
	req := httptest.NewRequest("POST", "/api/hook-event", strings.NewReader(string(body)))
	req.Header.Set("X-Hook-Signature", signHook(key, body))
	rec := httptest.NewRecorder()
	ctx.HandleHookEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- GetTaskGate ---

func TestGetTaskGate_EmptyAndSeeded(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	// Unknown session -> gate nil.
	req := withURLParam(httptest.NewRequest("GET", "/api/task-gate/none", nil), "sessionId", "none")
	rec := httptest.NewRecorder()
	ctx.GetTaskGate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["gate"] != nil {
		t.Errorf("gate should be nil for unknown session, got %v", resp["gate"])
	}

	// Seed a gate for session s5.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"taskGate-s5": map[string]interface{}{"open": true}})
	req2 := withURLParam(httptest.NewRequest("GET", "/api/task-gate/s5", nil), "sessionId", "s5")
	rec2 := httptest.NewRecorder()
	ctx.GetTaskGate(rec2, req2)
	resp2 := decodeMap(t, rec2)
	gate, _ := resp2["gate"].(map[string]interface{})
	if gate["open"] != true {
		t.Errorf("gate.open=%v, want true", gate["open"])
	}
}

// --- Run: validation branches (no subprocess) ---

func TestRun_MissingCommand400(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{"command": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing command, got %d", rec.Code)
	}
}

func TestRun_ShellMetachars400(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{"command": "ls; rm -rf /"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for shell metachars, got %d", rec.Code)
	}
}

func TestRun_DisallowedBinary400(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{"command": "rm -rf foo"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed binary, got %d", rec.Code)
	}
}

func TestRun_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("POST", "/api/run", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.Run(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- GeneratePlan: pure markdown generation ---

func TestGeneratePlan_RendersMarkdown(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.GeneratePlan, "/api/generate-plan", map[string]interface{}{
		"description": "Refactor the auth flow",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	plan, _ := resp["plan"].(string)
	if !strings.Contains(plan, "Refactor the auth flow") {
		t.Errorf("plan should embed the description, got %q", plan)
	}
	if !strings.Contains(plan, "Implementation Plan") {
		t.Errorf("plan missing header: %q", plan)
	}
}

func TestGeneratePlan_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("POST", "/api/generate-plan", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.GeneratePlan(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- QuickStart: validation branches (no subprocess) ---

func TestQuickStart_MissingCaseName400(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	rec := postJSON(t, ctx.QuickStart, "/api/quick-start", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing caseName, got %d", rec.Code)
	}
}

func TestQuickStart_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	req := httptest.NewRequest("POST", "/api/quick-start", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.QuickStart(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
