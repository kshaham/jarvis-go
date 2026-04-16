package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/ralph"
	"github.com/claudeman/jarvis-go/internal/runsummary"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

var allowedCommands = []string{"git", "npm", "go", "ls", "cat", "head", "tail", "grep", "find", "wc", "sort", "diff", "echo", "pwd", "which", "env", "node", "npx", "tsc"}

func isCommandAllowed(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := filepath.Base(parts[0])
	for _, allowed := range allowedCommands {
		if base == allowed {
			return true
		}
	}
	return false
}

// shellMetachars matches dangerous shell metacharacters that could enable injection.
var shellMetachars = regexp.MustCompile(`[;|&$` + "`" + `><(){}]`)

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
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"telegram": map[string]interface{}{
			"token":     body.Token,
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
		request.WriteError(w, http.StatusInternalServerError, err.Error())
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
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"telegram": map[string]interface{}{
			"token":     body.Token,
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

// HandleHookEvent processes an incoming hook event and forwards signals
// to the session's RespawnController when applicable.
// POST /api/hook-event
func (ctx *Context) HandleHookEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	eventType, _ := body["event"].(string)
	if eventType == "" {
		eventType = "hook:received"
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

	// Create Ralph tracker for quick-start session
	qsTracker := ralph.NewTracker()
	ctx.RalphTrackersMu.Lock()
	ctx.RalphTrackers[session.ID] = qsTracker
	ctx.RalphTrackersMu.Unlock()

	// Create run summary tracker for quick-start session
	qsRunTracker := runsummary.New(session.ID)
	ctx.RunSummariesMu.Lock()
	ctx.RunSummaries[session.ID] = qsRunTracker
	ctx.RunSummariesMu.Unlock()

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
	// Validate command against whitelist and reject shell metacharacters.
	if !isCommandAllowed(body.Command) || shellMetachars.MatchString(body.Command) {
		request.WriteError(w, http.StatusBadRequest, "command not allowed")
		return
	}
	// Validate working directory exists.
	if body.WorkingDir != "" {
		info, err := os.Stat(body.WorkingDir)
		if err != nil || !info.IsDir() {
			request.WriteError(w, http.StatusBadRequest, "workingDir does not exist or is not a directory")
			return
		}
	}
	// Split command into args to avoid shell injection (no sh -c).
	parts := strings.Fields(body.Command)
	if len(parts) == 0 {
		request.WriteError(w, http.StatusBadRequest, "empty command")
		return
	}
	cmd := exec.Command(parts[0], parts[1:]...)
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

	// Validate name format: only letters, numbers, hyphens, underscores
	for _, c := range body.Name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			request.WriteError(w, http.StatusBadRequest, "invalid case name: use only letters, numbers, hyphens, underscores")
			return
		}
	}

	if body.Path == "" {
		request.WriteError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Expand ~ to home directory
	expandedPath := body.Path
	if strings.HasPrefix(expandedPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			request.WriteError(w, http.StatusInternalServerError, "failed to resolve home directory")
			return
		}
		expandedPath = filepath.Join(homeDir, expandedPath[1:])
	}

	// Validate the path exists and is a directory
	info, err := os.Stat(expandedPath)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "folder not found: "+expandedPath)
		return
	}
	if !info.IsDir() {
		request.WriteError(w, http.StatusBadRequest, "path is not a directory")
		return
	}

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

	// Determine the working directory from the case (if specified)
	workingDir := ""
	if req.CaseName != "" {
		caseInfo, err := ctx.CaseManager.Get(req.CaseName)
		if err == nil {
			workingDir = caseInfo.Path

			// Clear old ralph-wizard directory for fresh generation
			wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
			if _, statErr := os.Stat(wizardDir); statErr == nil {
				os.RemoveAll(wizardDir)
			}
		}
	}

	if workingDir == "" {
		// Fallback to home directory or current working directory
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

	// Create Ralph tracker for plan session
	planTracker := ralph.NewTracker()
	ctx.RalphTrackersMu.Lock()
	ctx.RalphTrackers[session.ID] = planTracker
	ctx.RalphTrackersMu.Unlock()

	// Create run summary tracker for plan session
	planRunTracker := runsummary.New(session.ID)
	ctx.RunSummariesMu.Lock()
	ctx.RunSummaries[session.ID] = planRunTracker
	ctx.RunSummariesMu.Unlock()

	go forwardSessionEvents(session, ctx)

	// Send the plan prompt to the session after a short delay
	go func() {
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

	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
	entries, err := os.ReadDir(wizardDir)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "ralph wizard directory not found")
		return
	}

	type wizardFile struct {
		AgentType  string `json:"agentType"`
		PromptFile string `json:"promptFile,omitempty"`
		ResultFile string `json:"resultFile,omitempty"`
	}
	var files []wizardFile

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(wizardDir, entry.Name())
		wf := wizardFile{AgentType: entry.Name()}

		if _, statErr := os.Stat(filepath.Join(agentDir, "prompt.md")); statErr == nil {
			wf.PromptFile = entry.Name() + "/prompt.md"
		}
		if _, statErr := os.Stat(filepath.Join(agentDir, "result.json")); statErr == nil {
			wf.ResultFile = entry.Name() + "/result.json"
		}

		if wf.PromptFile != "" || wf.ResultFile != "" {
			files = append(files, wf)
		}
	}

	if files == nil {
		files = []wizardFile{}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"files":    files,
			"caseName": caseName,
		},
	})
}

// GetRalphWizardFile reads a specific file from the ralph-wizard directory.
// GET /api/cases/{caseName}/ralph-wizard/file/*
func (ctx *Context) GetRalphWizardFile(w http.ResponseWriter, r *http.Request) {
	caseName := chi.URLParam(r, "caseName")
	caseInfo, err := ctx.CaseManager.Get(caseName)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "case not found")
		return
	}

	// Prevent browser caching
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	// Extract the file path from the URL (everything after ralph-wizard/file/)
	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		request.WriteError(w, http.StatusBadRequest, "file path is required")
		return
	}

	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")

	// Security: ensure path is within wizard directory
	fullPath := filepath.Join(wizardDir, filePath)
	resolvedPath, err := filepath.Abs(fullPath)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid file path")
		return
	}
	resolvedWizard, err := filepath.Abs(wizardDir)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to resolve wizard directory")
		return
	}
	if !strings.HasPrefix(resolvedPath, resolvedWizard+string(filepath.Separator)) && resolvedPath != resolvedWizard {
		request.WriteError(w, http.StatusForbidden, "invalid file path")
		return
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, "file not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read file: "+err.Error())
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

	// Parse JSON content if applicable
	if isJSON {
		var parsed interface{}
		if jsonErr := json.Unmarshal(data, &parsed); jsonErr == nil {
			result["data"].(map[string]interface{})["parsed"] = parsed
		}
	}

	request.WriteJSON(w, http.StatusOK, result)
}
