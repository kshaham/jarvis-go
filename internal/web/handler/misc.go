package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/safeexec"
	"github.com/kshaham/jarvis/internal/web/request"
)

// --- Templates ---

// ListTemplates returns all available templates.
// GET /api/templates
func (ctx *Context) ListTemplates(w http.ResponseWriter, r *http.Request) {
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"templates": ctx.TemplateStore.List(),
	})
}

// GetTemplate returns a single template by ID.
// GET /api/templates/{id}
func (ctx *Context) GetTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t := ctx.TemplateStore.Get(id)
	if t == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// CreateTemplate creates a new template.
// POST /api/templates
func (ctx *Context) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	t := ctx.TemplateStore.Create(body.Name, body.Description, body.Content)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// UpdateTemplate updates an existing template.
// PUT /api/templates/{id}
func (ctx *Context) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t := ctx.TemplateStore.Update(id, body)
	if t == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// DeleteTemplate deletes a template.
// DELETE /api/templates/{id}
func (ctx *Context) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !ctx.TemplateStore.Delete(id) {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// DuplicateTemplate duplicates a template.
// POST /api/templates/{id}/duplicate
func (ctx *Context) DuplicateTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t := ctx.TemplateStore.Duplicate(id)
	if t == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// ExportTemplate exports a template as downloadable JSON.
// GET /api/templates/{id}/export
func (ctx *Context) ExportTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t := ctx.TemplateStore.Get(id)
	if t == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"template-%s.json\"", id))
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// ImportTemplate imports a template.
// POST /api/templates/import
func (ctx *Context) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	t := ctx.TemplateStore.Create(body.Name, body.Description, body.Content)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"template": t,
	})
}

// --- Telegram ---

// maskToken returns a masked version of a token for safe display.
func maskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

// GetTelegramStatus returns Telegram integration status.
// GET /api/settings/telegram
func (ctx *Context) GetTelegramStatus(w http.ResponseWriter, r *http.Request) {
	if ctx.TelegramBot == nil {
		ok(w, map[string]interface{}{"connected": false, "telegram": map[string]interface{}{"connected": false}})
		return
	}
	connected := ctx.TelegramBot.IsConnected()
	token, chatID := ctx.TelegramBot.GetConfig()
	maskedToken := ""
	if token != "" {
		maskedToken = maskToken(token)
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"connected": connected,
		"telegram": map[string]interface{}{
			"connected": connected,
			"token":     maskedToken,
			"chatId":    chatID,
		},
	})
}

// UpdateTelegramConfig updates Telegram configuration.
// POST /api/settings/telegram
func (ctx *Context) UpdateTelegramConfig(w http.ResponseWriter, r *http.Request) {
	if ctx.TelegramBot == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "telegram not available")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Token  string `json:"token"`
		ChatID string `json:"chatId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.TelegramBot.Configure(body.Token, body.ChatID)
	// R27 state review · The authoritative Telegram credential store
	// is ~/.jarvis/telegram-settings.json (via the SettingsStore).
	// Previously we ALSO wrote the bot token into state.json, which
	// put a plaintext copy alongside non-secret config. Now we only
	// persist the non-secret bits here — chatId and connected flag —
	// so state.json stays credential-free. The token lives in
	// telegram-settings.json where the SettingsStore manages it.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"telegram": map[string]interface{}{
			"chatId":    body.ChatID,
			"connected": ctx.TelegramBot.IsConnected(),
		},
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// SendTelegramTest sends a test message via Telegram.
// POST /api/telegram/test
func (ctx *Context) SendTelegramTest(w http.ResponseWriter, r *http.Request) {
	if ctx.TelegramBot == nil || !ctx.TelegramBot.IsConnected() {
		request.WriteError(w, http.StatusBadRequest, "telegram not configured")
		return
	}
	if err := ctx.TelegramBot.SendMessage("Test from Jarvis"); err != nil {
		// R28 · Never echo the underlying error to the client: a transport
		// failure surfaces as *url.Error whose URL field embeds the bot
		// token. The error is already token-redacted by the telegram
		// package before it reaches here, but we also keep the client-
		// facing body generic so nothing leaks to devtools or proxy logs.
		slog.Error("telegram test send failed", "err", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to send telegram message")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "test message sent",
	})
}

// ConnectTelegram connects the Telegram integration.
// POST /api/telegram/send-test
func (ctx *Context) ConnectTelegram(w http.ResponseWriter, r *http.Request) {
	if ctx.TelegramBot == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "telegram not available")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Token  string `json:"token"`
		ChatID string `json:"chatId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.TelegramBot.Configure(body.Token, body.ChatID)
	// R27 state review · The authoritative Telegram credential store
	// is ~/.jarvis/telegram-settings.json (via the SettingsStore).
	// Previously we ALSO wrote the bot token into state.json, which
	// put a plaintext copy alongside non-secret config. Now we only
	// persist the non-secret bits here — chatId and connected flag —
	// so state.json stays credential-free. The token lives in
	// telegram-settings.json where the SettingsStore manages it.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"telegram": map[string]interface{}{
			"chatId":    body.ChatID,
			"connected": ctx.TelegramBot.IsConnected(),
		},
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"connected": ctx.TelegramBot.IsConnected(),
	})
}

// DisconnectTelegram disconnects the Telegram integration.
// POST /api/telegram/disconnect
func (ctx *Context) DisconnectTelegram(w http.ResponseWriter, r *http.Request) {
	if ctx.TelegramBot == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "telegram not available")
		return
	}
	ctx.TelegramBot.Disconnect()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"telegram": map[string]interface{}{
			"connected": false,
		},
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"connected": false,
	})
}

// --- Hooks ---

// allowedHookEvents gates which event types the hook endpoint will process.
// Arbitrary event names could be used to inject data into the SSE stream
// or trigger unintended signals in the respawn controller.
var allowedHookEvents = map[string]bool{
	"stop":               true,
	"idle_prompt":        true,
	"permission_prompt":  true,
	"elicitation_dialog": true,
	"subagent_started":   true,
	"subagent_completed": true,
	"subagent_tool":      true,
}

// verifyHookSignature validates the HMAC-SHA256 signature sent in the
// X-Hook-Signature header (format: "sha256=<hex>") against the raw body
// and the server's hook signing key. Used to authenticate the local
// Claude CLI hook caller without requiring a session cookie.
//
// R27 security review · Accepts signatures under HookSigningKey
// (preferred) OR the legacy session-cookie SigningKey for back-compat
// with older hook signers. Constant-time compare on both paths. The
// legacy fallback can be removed in a future round once every
// deployed hook signer has migrated to the domain-separated key.
func (ctx *Context) verifyHookSignature(body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	presented, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}

	check := func(key []byte) bool {
		if len(key) == 0 {
			return false
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(body)
		return hmac.Equal(mac.Sum(nil), presented)
	}

	if check(ctx.AuthManager.HookSigningKey()) {
		return true
	}
	// Legacy: older signers used the session-cookie key directly.
	return check(ctx.AuthManager.SigningKey())
}

// HandleHookEvent processes an incoming hook event and forwards signals
// to the session's RespawnController when applicable. The caller must
// attach an X-Hook-Signature header that HMAC-signs the raw body with the
// server's signing key; unsigned or mis-signed requests are rejected.
// POST /api/hook-event
func (ctx *Context) HandleHookEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if !ctx.verifyHookSignature(rawBody, r.Header.Get("X-Hook-Signature")) {
		request.WriteError(w, http.StatusUnauthorized, "invalid hook signature")
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	eventType, _ := body["event"].(string)
	if eventType == "" {
		eventType = "hook:received"
	}
	if !allowedHookEvents[eventType] {
		request.WriteError(w, http.StatusBadRequest, "unknown event type")
		return
	}
	sessionID, _ := body["sessionId"].(string)

	// Forward relevant signals to the session's RespawnController.
	if sessionID != "" {
		ctx.RespawnMu.RLock()
		rc := ctx.RespawnControllers[sessionID]
		ctx.RespawnMu.RUnlock()

		if rc != nil {
			switch eventType {
			case "stop":
				log.Printf("[hook] forwarding stop signal to respawn controller for session %s", sessionID)
				rc.OnStopHook()
			case "idle_prompt":
				log.Printf("[hook] forwarding idle_prompt signal to respawn controller for session %s", sessionID)
				rc.OnIdlePrompt()
			}
		}
	}

	// Forward hook events to Telegram handler.
	if ctx.TelegramHandler != nil && sessionID != "" {
		switch eventType {
		case "permission_prompt":
			data, _ := body["data"].(map[string]interface{})
			ctx.TelegramHandler.NotifyPermissionPrompt(sessionID, data)
		case "elicitation_dialog":
			data, _ := body["data"].(map[string]interface{})
			ctx.TelegramHandler.NotifyElicitation(sessionID, data)
		}
	}

	// Broadcast the hook event via SSE with hook:{eventType} prefix.
	ctx.SSEBroker.Broadcast("hook:"+eventType, body)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// GetHookConfig returns the hook configuration.
// GET /api/hook-config
func (ctx *Context) GetHookConfig(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	hookConfig, _ := settings["hookConfig"]
	if hookConfig == nil {
		hookConfig = map[string]interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"hookConfig": hookConfig,
	})
}

// --- Task Gate ---

// GetTaskGate returns the task gate state for a session.
// GET /api/task-gate/{sessionId}
func (ctx *Context) GetTaskGate(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	settings := ctx.StateStore.GetSettings()
	gate, _ := settings["taskGate-"+sessionID]
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"gate":    gate,
	})
}

// --- Cases ---

// ListCases returns all available cases.
// GET /api/cases
func (ctx *Context) ListCases(w http.ResponseWriter, r *http.Request) {
	cases := ctx.CaseManager.List()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"cases":   cases,
	})
}

// GetCase returns a single case by name.
// GET /api/cases/{name}
func (ctx *Context) GetCase(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	info, err := ctx.CaseManager.Get(name)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "case not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"case":    info,
	})
}

// CreateCase creates a new case.
// POST /api/cases
func (ctx *Context) CreateCase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	info, err := ctx.CaseManager.Create(body.Name)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"case":    info,
	})
}

// UpdateCase updates an existing case.
// PUT /api/cases/{name}
func (ctx *Context) UpdateCase(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	caseInfo, err := ctx.CaseManager.Get(name)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "case not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	configPath := filepath.Join(caseInfo.Path, "config.json")
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to marshal config")
		return
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write config.json")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// DeleteCase deletes a case.
// DELETE /api/cases/{name}
func (ctx *Context) DeleteCase(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := ctx.CaseManager.Delete(name); err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// --- Quick Start ---

// QuickStart creates a session and starts it from a case template.
// POST /api/quick-start
func (ctx *Context) QuickStart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req request.QuickStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CaseName == "" {
		request.WriteError(w, http.StatusBadRequest, "caseName is required")
		return
	}

	// Determine working directory:
	// 1. Check defaultWorkingDir from account settings (highest priority)
	// 2. Fall back to case directory
	workingDir := ""
	settings := ctx.StateStore.GetSettings()
	if defaultDir, ok := settings["defaultWorkingDir"].(string); ok && defaultDir != "" {
		// Validate the directory exists
		if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
			workingDir = defaultDir
		}
	}

	if workingDir == "" {
		caseInfo, err := ctx.CaseManager.Get(req.CaseName)
		if err != nil {
			request.WriteError(w, http.StatusNotFound, "case not found")
			return
		}
		workingDir = caseInfo.Path
	}

	mode := pty.ModeClaude
	if req.Mode == "shell" {
		mode = pty.ModeShell
	}

	// Build provider env vars from account settings
	providerEnv := ctx.getProviderEnvVars()

	// Use Background context -- sessions outlive the HTTP request
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:  workingDir,
			Mode:        mode,
			Name:        req.CaseName,
			ProviderEnv: providerEnv,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start event forwarding before broadcast to avoid losing early events
	go forwardSessionEvents(session, ctx)

	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))

	ctx.initSessionTrackers(session.ID)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"session": sessionToJSON(session),
	})
}

// --- Scheduled Tasks ---

// ListScheduledTasks returns all scheduled tasks.
// GET /api/scheduled
func (ctx *Context) ListScheduledTasks(w http.ResponseWriter, r *http.Request) {
	ctx.ScheduledTasksMu.RLock()
	tasks := make([]interface{}, 0, len(ctx.ScheduledTasks))
	for _, t := range ctx.ScheduledTasks {
		tasks = append(tasks, t)
	}
	ctx.ScheduledTasksMu.RUnlock()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tasks":   tasks,
	})
}

// CreateScheduledTask creates a new scheduled task.
// POST /api/scheduled
func (ctx *Context) CreateScheduledTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id := uuid.New().String()
	body["id"] = id
	ctx.ScheduledTasksMu.Lock()
	ctx.ScheduledTasks[id] = body
	ctx.ScheduledTasksMu.Unlock()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    body,
	})
}

// GetScheduledTask returns a single scheduled task.
// GET /api/scheduled/{id}
func (ctx *Context) GetScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx.ScheduledTasksMu.RLock()
	task, ok := ctx.ScheduledTasks[id]
	ctx.ScheduledTasksMu.RUnlock()
	if !ok {
		request.WriteError(w, http.StatusNotFound, "scheduled task not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// UpdateScheduledTask updates a scheduled task.
// PUT /api/scheduled/{id}
func (ctx *Context) UpdateScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx.ScheduledTasksMu.Lock()
	existing, exists := ctx.ScheduledTasks[id]
	if !exists {
		ctx.ScheduledTasksMu.Unlock()
		request.WriteError(w, http.StatusNotFound, "scheduled task not found")
		return
	}

	// Merge updates into existing task, preserving the ID
	existingMap, _ := existing.(map[string]interface{})
	if existingMap == nil {
		existingMap = make(map[string]interface{})
	}
	for k, v := range body {
		existingMap[k] = v
	}
	existingMap["id"] = id
	ctx.ScheduledTasks[id] = existingMap
	ctx.ScheduledTasksMu.Unlock()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    existingMap,
	})
}

// DeleteScheduledTask deletes a scheduled task.
// DELETE /api/scheduled/{id}
func (ctx *Context) DeleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx.ScheduledTasksMu.Lock()
	_, ok := ctx.ScheduledTasks[id]
	if ok {
		delete(ctx.ScheduledTasks, id)
	}
	ctx.ScheduledTasksMu.Unlock()
	if !ok {
		request.WriteError(w, http.StatusNotFound, "scheduled task not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// --- Run ---

// Run executes a command outside of a session context.
// POST /api/run
func (ctx *Context) Run(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Command    string `json:"command"`
		WorkingDir string `json:"workingDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Command == "" {
		request.WriteError(w, http.StatusBadRequest, "command is required")
		return
	}
	// Reject shell metacharacters before any other parsing.
	if safeexec.ContainsShellMetachars(body.Command) {
		request.WriteError(w, http.StatusBadRequest, "command not allowed")
		return
	}
	// Resolve to an absolute path and confirm the base name is in the
	// allowlist AND the resolved path is inside an approved prefix. This
	// blocks the `PATH=/tmp:...` shim attack on the allowlisted names.
	absPath, args, ok := safeexec.Resolve(body.Command)
	if !ok {
		request.WriteError(w, http.StatusBadRequest, "command not allowed")
		return
	}
	// Validate working directory via the standard resolver (symlink + allowlist).
	if body.WorkingDir != "" {
		resolved, err := ctx.ResolveWorkingDir(body.WorkingDir)
		if err != nil {
			request.WriteError(w, http.StatusBadRequest, "invalid working directory: "+err.Error())
			return
		}
		body.WorkingDir = resolved
	}
	// Cap subprocess wall time so a hung child can't block the HTTP
	// handler indefinitely. 30s comfortably covers every safeexec-
	// allowed command; anything slower is the misbehaving-binary case
	// we want to abort. Derived from r.Context() so client disconnect
	// also tears the subprocess down.
	cmdCtx, cmdCancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cmdCancel()
	cmd := exec.CommandContext(cmdCtx, absPath, args...)
	if body.WorkingDir != "" {
		cmd.Dir = body.WorkingDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Cap output at 1MB.
	const maxOutput = 1 << 20
	outBytes := stdout.Bytes()
	if len(outBytes) > maxOutput {
		outBytes = outBytes[:maxOutput]
	}
	errBytes := stderr.Bytes()
	if len(errBytes) > maxOutput {
		errBytes = errBytes[:maxOutput]
	}
	exitCode := 0
	if err != nil {
		if exitErr, isExit := err.(*exec.ExitError); isExit {
			exitCode = exitErr.ExitCode()
		} else {
			request.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"output":   string(outBytes),
		"stderr":   string(errBytes),
		"exitCode": exitCode,
	})
}

// --- Generate Plan ---

// GeneratePlan generates an implementation plan.
// POST /api/generate-plan
func (ctx *Context) GeneratePlan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		SessionID   string `json:"sessionId"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Generate a basic markdown plan from the description
	plan := fmt.Sprintf("# Implementation Plan\n\n## Goal\n%s\n\n## Steps\n1. Analyze requirements\n2. Implement changes\n3. Write tests\n4. Verify\n\n## Notes\nGenerated by Jarvis Go\n", req.Description)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"plan":    plan,
	})
}

// --- Code Printer ---

// CodePrinterPrint formats and prints code.
// POST /api/code-printer/print
func (ctx *Context) CodePrinterPrint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Code     string `json:"code"`
		Language string `json:"language"`
		Theme    string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"code":     req.Code,
		"language": req.Language,
		"lines":    len(strings.Split(req.Code, "\n")),
	})
}

// CodePrinterPreview previews formatted code.
// POST /api/code-printer/preview
func (ctx *Context) CodePrinterPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Code     string `json:"code"`
		Language string `json:"language"`
		Theme    string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"code":     req.Code,
		"language": req.Language,
		"lines":    len(strings.Split(req.Code, "\n")),
	})
}

// CodePrinterConfig returns code printer configuration.
// GET /api/code-printer/config
func (ctx *Context) CodePrinterConfig(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	codePrinter, _ := settings["codePrinter"].(map[string]interface{})

	languages := []string{
		"go", "javascript", "typescript", "python", "rust", "java",
		"c", "cpp", "csharp", "ruby", "swift", "kotlin", "php",
		"html", "css", "sql", "bash", "yaml", "json", "markdown",
	}
	themes := []string{"dark", "light", "monokai", "github", "dracula", "solarized"}

	if codePrinter != nil {
		if langs, ok := codePrinter["languages"].([]interface{}); ok && len(langs) > 0 {
			languages = make([]string, 0, len(langs))
			for _, l := range langs {
				if s, ok := l.(string); ok {
					languages = append(languages, s)
				}
			}
		}
		if ts, ok := codePrinter["themes"].([]interface{}); ok && len(ts) > 0 {
			themes = make([]string, 0, len(ts))
			for _, t := range ts {
				if s, ok := t.(string); ok {
					themes = append(themes, s)
				}
			}
		}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"languages": languages,
		"themes":    themes,
	})
}

// --- Group 2: Cases & Planning ---

// LinkCase links an existing folder as a case without creating a new directory.
// POST /api/cases/link
func (ctx *Context) LinkCase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !cases.IsValidCaseName(body.Name) {
		request.WriteError(w, http.StatusBadRequest, "invalid case name: use only letters, numbers, hyphens, underscores, dots (no ..)")
		return
	}
	if body.Path == "" {
		request.WriteError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Expand ~ to home directory so operators can paste paths
	// straight from their shell.
	expandedPath := body.Path
	if strings.HasPrefix(expandedPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			request.WriteError(w, http.StatusInternalServerError, "failed to resolve home directory")
			return
		}
		expandedPath = filepath.Join(homeDir, expandedPath[1:])
	}

	// CaseManager.Link does the stat + IsDir + symlink work and
	// returns a 400-shaped error on the unhappy paths. Defer to it.
	caseInfo, err := ctx.CaseManager.Link(body.Name, expandedPath)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx.SSEBroker.Broadcast("case:linked", map[string]interface{}{
		"name": caseInfo.Name,
		"path": caseInfo.Path,
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"case": map[string]interface{}{
			"name": caseInfo.Name,
			"path": caseInfo.Path,
		},
	})
}

// GetCaseFixPlan reads @fix_plan.md from a case directory.
// GET /api/cases/{name}/fix-plan
func (ctx *Context) GetCaseFixPlan(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	caseInfo, err := ctx.CaseManager.Get(name)
	if err != nil {
		// Case not found
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"exists":  false,
			"content": nil,
			"todos":   []interface{}{},
		})
		return
	}

	fixPlanPath := filepath.Join(caseInfo.Path, "@fix_plan.md")
	data, err := os.ReadFile(fixPlanPath)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"exists":  false,
			"content": nil,
			"todos":   []interface{}{},
		})
		return
	}

	// Parse todos from the markdown content
	content := string(data)
	var todos []map[string]interface{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [") {
			continue
		}
		// Parse checkbox: - [ ] text, - [x] text, - [-] text
		status := "pending"
		if len(trimmed) >= 5 {
			marker := trimmed[3:4]
			switch marker {
			case "x", "X":
				status = "completed"
			case "-":
				status = "in_progress"
			}
		}
		// Extract the text after the checkbox
		textStart := strings.Index(trimmed, "] ")
		if textStart >= 0 {
			todoContent := strings.TrimSpace(trimmed[textStart+2:])
			todos = append(todos, map[string]interface{}{
				"content":  todoContent,
				"status":   status,
				"priority": nil,
			})
		}
	}
	if todos == nil {
		todos = []map[string]interface{}{}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"exists":  true,
		"content": content,
		"todos":   todos,
	})
}

// GeneratePlanDetailed generates a detailed implementation plan using a Claude session.
// Creates a session, sends the plan prompt, and returns the orchestration ID.
// POST /api/generate-plan-detailed
func (ctx *Context) GeneratePlanDetailed(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		TaskDescription string `json:"taskDescription"`
		CaseName        string `json:"caseName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.TaskDescription) == "" {
		request.WriteError(w, http.StatusBadRequest, "task description is required")
		return
	}

	if len(req.TaskDescription) > 10000 {
		request.WriteError(w, http.StatusBadRequest, "task description too long (max 10000 chars)")
		return
	}

	// Determine the working directory from the case (if specified).
	// Wizard dir gets cleared before the run so stale agent prompts/
	// results from a prior generation don't contaminate this one.
	workingDir := ""
	if req.CaseName != "" {
		if caseInfo, err := ctx.CaseManager.Get(req.CaseName); err == nil {
			workingDir = caseInfo.Path
			if clearErr := cases.ClearRalphWizardDir(caseInfo); clearErr != nil {
				log.Printf("[plan-session] failed to clear ralph-wizard dir for case=%s: %v",
					req.CaseName, clearErr)
			}
		}
	}
	if workingDir == "" {
		// Fallback to home directory so plan generation always has a
		// valid cwd, even without an attached case.
		homeDir, _ := os.UserHomeDir()
		workingDir = homeDir
	}

	// Build provider env vars
	providerEnv := ctx.getProviderEnvVars()

	// Create a Claude session for plan generation
	sessionName := "plan-gen"
	if req.CaseName != "" {
		sessionName = "plan-gen-" + req.CaseName
	}

	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:  workingDir,
			Mode:        pty.ModeClaude,
			Name:        sessionName,
			ProviderEnv: providerEnv,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to create plan session: "+err.Error())
		return
	}

	orchestratorID := fmt.Sprintf("plan-%d", time.Now().UnixMilli())

	// Store the orchestration
	ctx.ActivePlanOrchestratorsMu.Lock()
	if ctx.ActivePlanOrchestrators == nil {
		ctx.ActivePlanOrchestrators = make(map[string]string)
	}
	ctx.ActivePlanOrchestrators[orchestratorID] = session.ID
	ctx.ActivePlanOrchestratorsMu.Unlock()

	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))
	ctx.SSEBroker.Broadcast("plan:started", map[string]interface{}{
		"orchestratorId": orchestratorID,
	})

	ctx.initSessionTrackers(session.ID)

	go forwardSessionEvents(session, ctx)

	// Send the plan prompt to the session after a short delay.
	// A panic inside WriteViaScreen -- closed PTY fd, screen torn
	// down during the 2s wait -- would otherwise kill the whole
	// jarvis process. Match the panic-recovery pattern used in
	// office.go for background prompt injections.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[plan-session] prompt injection panic session=%s err=%v", session.ID, r)
			}
		}()
		select {
		case <-time.After(2 * time.Second):
			prompt := fmt.Sprintf("Generate a detailed implementation plan for the following task:\n\n%s", req.TaskDescription)
			session.WriteViaScreen(prompt + "\r")
		case <-session.Done():
			return
		}
	}()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"orchestratorId": orchestratorID,
			"sessionId":      session.ID,
		},
	})
}

// CancelPlanGeneration cancels active plan generation by stopping the plan session.
// POST /api/cancel-plan-generation
func (ctx *Context) CancelPlanGeneration(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		OrchestratorID string `json:"orchestratorId"`
	}
	// Decode body, but allow empty body (cancel all)
	_ = json.NewDecoder(r.Body).Decode(&req)

	cancelled := []string{}

	ctx.ActivePlanOrchestratorsMu.Lock()
	if req.OrchestratorID != "" {
		// Cancel specific orchestrator
		sessionID, found := ctx.ActivePlanOrchestrators[req.OrchestratorID]
		if !found {
			ctx.ActivePlanOrchestratorsMu.Unlock()
			request.WriteError(w, http.StatusNotFound, "plan generation not found or already completed")
			return
		}
		delete(ctx.ActivePlanOrchestrators, req.OrchestratorID)
		ctx.ActivePlanOrchestratorsMu.Unlock()

		_ = ctx.SessionManager.StopSession(r.Context(), sessionID)
		ctx.SSEBroker.Broadcast("plan:cancelled", map[string]interface{}{
			"orchestratorId": req.OrchestratorID,
		})
		cancelled = append(cancelled, req.OrchestratorID)
	} else {
		// Cancel all active plan generations
		for orchID, sessionID := range ctx.ActivePlanOrchestrators {
			_ = ctx.SessionManager.StopSession(r.Context(), sessionID)
			ctx.SSEBroker.Broadcast("plan:cancelled", map[string]interface{}{
				"orchestratorId": orchID,
			})
			cancelled = append(cancelled, orchID)
		}
		ctx.ActivePlanOrchestrators = make(map[string]string)
		ctx.ActivePlanOrchestratorsMu.Unlock()
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"cancelled": cancelled,
		},
	})
}

// GetRalphWizardFiles lists ralph-wizard files (prompts and results) in a case directory.
// GET /api/cases/{caseName}/ralph-wizard/files
func (ctx *Context) GetRalphWizardFiles(w http.ResponseWriter, r *http.Request) {
	caseName := chi.URLParam(r, "caseName")
	caseInfo, err := ctx.CaseManager.Get(caseName)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "case not found")
		return
	}
	files, err := cases.ListRalphWizardFiles(caseInfo)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"files":    files,
			"caseName": caseName,
		},
	})
}

// GetRalphWizardFile reads a specific file from the ralph-wizard
// directory. The symlink-safe path resolution + read lives in
// cases.ReadRalphWizardFile; this handler does HTTP shape only.
// GET /api/cases/{caseName}/ralph-wizard/file/*
func (ctx *Context) GetRalphWizardFile(w http.ResponseWriter, r *http.Request) {
	caseName := chi.URLParam(r, "caseName")
	caseInfo, err := ctx.CaseManager.Get(caseName)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "case not found")
		return
	}

	// Prevent browser caching.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		request.WriteError(w, http.StatusBadRequest, "file path is required")
		return
	}

	data, err := cases.ReadRalphWizardFile(caseInfo, filePath)
	if err != nil {
		switch {
		case errors.Is(err, cases.ErrRalphWizardFileMissing):
			request.WriteError(w, http.StatusNotFound, "file not found")
		case errors.Is(err, cases.ErrRalphWizardPathInvalid):
			request.WriteError(w, http.StatusForbidden, "invalid file path")
		default:
			request.WriteError(w, http.StatusInternalServerError, "failed to read file: "+err.Error())
		}
		return
	}

	content := string(data)
	isJSON := strings.HasSuffix(filePath, ".json")
	result := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"content":  content,
			"filePath": filePath,
			"isJson":   isJSON,
			"parsed":   nil,
		},
	}
	if isJSON {
		var parsed interface{}
		if jsonErr := json.Unmarshal(data, &parsed); jsonErr == nil {
			result["data"].(map[string]interface{})["parsed"] = parsed
		}
	}
	request.WriteJSON(w, http.StatusOK, result)
}
