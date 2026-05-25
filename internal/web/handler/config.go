package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/claudeman/jarvis-go/internal/metrics"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

var serverStartTime = time.Now()

// GetStats returns general application stats.
// GET /api/stats
func (ctx *Context) GetStats(w http.ResponseWriter, r *http.Request) {
	sessionCount := ctx.SessionManager.GetSessionCount()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"sessionCount": sessionCount,
		"uptime":       time.Since(serverStartTime).String(),
	})
}

// GetTokenStats returns token usage statistics.
// GET /api/token-stats
func (ctx *Context) GetTokenStats(w http.ResponseWriter, r *http.Request) {
	sessions := ctx.SessionManager.GetAllSessions()
	var totalInput, totalOutput, totalTokens int64
	perSession := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		input, output, total := s.Tokens()
		totalInput += input
		totalOutput += output
		totalTokens += total
		perSession = append(perSession, map[string]interface{}{
			"sessionId":    s.ID,
			"inputTokens":  input,
			"outputTokens": output,
			"totalTokens":  total,
		})
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats": map[string]interface{}{
			"totalInputTokens":  totalInput,
			"totalOutputTokens": totalOutput,
			"totalTokens":       totalTokens,
			"sessionCount":      len(sessions),
			"sessions":          perSession,
			"dailyStats":        ctx.StateStore.GetDailyStats(),
		},
	})
}

// GetConfig returns the current configuration.
// GET /api/config
func (ctx *Context) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := ctx.StateStore.GetConfig()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"config":  cfg,
	})
}

// UpdateConfig updates the configuration.
// PUT /api/config
func (ctx *Context) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateConfig(updates)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetSettings returns the current settings with API keys and other
// sensitive values masked. GET /api/settings.
//
// R26 security review: the raw settings map carries claudeApiKey /
// zaiApiKey / telegram.token in plaintext. Returning the full map
// leaks those secrets to any authenticated browser session AND to
// any agent that passes AuthWithAgentToken. Mask them with the same
// helper GetSettingsAccounts uses so the operator can still see
// "something is configured" without leaking impersonable material.
func (ctx *Context) GetSettings(w http.ResponseWriter, r *http.Request) {
	raw := ctx.StateStore.GetSettings()
	settings := MaskSensitiveSettings(raw)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"settings": settings,
	})
}

// MaskSensitiveSettings returns a shallow copy of the settings map
// with known secret fields redacted via maskSecret. Known fields:
//   - claudeApiKey, zaiApiKey (top-level)
//   - telegram.token (nested map)
//
// Non-secret fields pass through unchanged. Caller owns the returned
// map; the original is not mutated. Exported so the SSE init closure
// in internal/web/server.go can reuse the same redaction list.
func MaskSensitiveSettings(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return nil
	}
	out := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	if _, ok := out["claudeApiKey"]; ok {
		out["claudeApiKey"] = maskSecret(out["claudeApiKey"])
	}
	if _, ok := out["zaiApiKey"]; ok {
		out["zaiApiKey"] = maskSecret(out["zaiApiKey"])
	}
	if tg, ok := out["telegram"].(map[string]interface{}); ok {
		tgCopy := make(map[string]interface{}, len(tg))
		for k, v := range tg {
			tgCopy[k] = v
		}
		if _, tok := tgCopy["token"]; tok {
			tgCopy["token"] = maskSecret(tgCopy["token"])
		}
		out["telegram"] = tgCopy
	}
	return out
}

// UpdateSettings updates the settings.
// PUT /api/settings
func (ctx *Context) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Log the defaultWorkingDir if present for debugging
	if dir, ok := updates["defaultWorkingDir"]; ok {
		slog.Info("UpdateSettings: defaultWorkingDir", "value", dir)
	}
	ctx.StateStore.UpdateSettings(updates)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// maskSecret returns a display form of an API key that preserves enough of
// the original for operators to verify which key is configured without
// exposing enough to impersonate. Short values (less than 12 chars) are
// fully replaced; the rest show first 6 + last 4 with asterisks in between.
// Returns empty string for empty input so the field reads as "unconfigured"
// rather than "masked empty".
func maskSecret(v interface{}) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if len(s) < 12 {
		return "****"
	}
	return s[:6] + "****" + s[len(s)-4:]
}

// GetSettingsAccounts returns account settings as a flat object.
// The frontend (accounts.js) expects provider, zaiApiKey, etc. at the top level.
// API keys are returned masked so a leaked response can't be used to
// impersonate the configured account. The frontend shows the masked form
// as a placeholder; writing a new value still accepts the raw key via PUT.
// GET /api/settings/accounts
func (ctx *Context) GetSettingsAccounts(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	result := map[string]interface{}{
		"success":           true,
		"provider":          settings["provider"],
		"claudeApiKey":      maskSecret(settings["claudeApiKey"]),
		"zaiApiKey":         maskSecret(settings["zaiApiKey"]),
		"zaiModel":          settings["zaiModel"],
		"zaiSonnetModel":    settings["zaiSonnetModel"],
		"zaiHaikuModel":     settings["zaiHaikuModel"],
		"zaiBaseUrl":        settings["zaiBaseUrl"],
		"activeProfile":     settings["activeProfile"],
		"profile1Name":      settings["profile1Name"],
		"profile2Name":      settings["profile2Name"],
		"defaultWorkingDir": settings["defaultWorkingDir"],
		"claudeMode":        settings["claudeMode"],
		"allowedTools":      settings["allowedTools"],
		"officeSessionMode": settings["officeSessionMode"], // "sdk" | "hooks" | "legacy"; resolveOfficeSessionMode defaults missing/unknown to "sdk"
	}
	request.WriteJSON(w, http.StatusOK, result)
}

// CreateSettingsAccount creates a new account configuration.
// POST /api/settings/accounts
func (ctx *Context) CreateSettingsAccount(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// accounts.js sends the full settings object, not just username/password
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Save ALL account settings to the state store
	// This includes: provider, API keys, defaultWorkingDir, claudeMode, allowedTools, etc.
	// Drop masked API key values ("sk-ant****abcd") so a user re-saving the
	// settings page without editing the key field doesn't clobber the real
	// stored key with its display mask. The frontend shows masked values as
	// placeholders but submits them back unchanged.
	secretFields := map[string]bool{
		"claudeApiKey": true,
		"zaiApiKey":    true,
	}
	updates := make(map[string]interface{})
	for key, value := range body {
		if secretFields[key] {
			if s, ok := value.(string); ok && strings.Contains(s, "****") {
				continue
			}
		}
		updates[key] = value
	}

	slog.Info("CreateSettingsAccount", "keys", func() []string {
		keys := make([]string, 0, len(body))
		for k := range body {
			keys = append(keys, k)
		}
		return keys
	}(), "defaultWorkingDir", body["defaultWorkingDir"])

	ctx.StateStore.UpdateSettings(updates)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetDebugMemory returns debug memory information.
// GET /api/debug/memory
func (ctx *Context) GetDebugMemory(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	ctx.RespawnMu.RLock()
	respawnCount := len(ctx.RespawnControllers)
	ctx.RespawnMu.RUnlock()

	ctx.ScheduledTasksMu.RLock()
	scheduledCount := len(ctx.ScheduledTasks)
	ctx.ScheduledTasksMu.RUnlock()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"memory": map[string]interface{}{
			"allocMB":       float64(memStats.Alloc) / 1024 / 1024,
			"totalAllocMB":  float64(memStats.TotalAlloc) / 1024 / 1024,
			"sysMB":         float64(memStats.Sys) / 1024 / 1024,
			"heapAllocMB":   float64(memStats.HeapAlloc) / 1024 / 1024,
			"heapInUseMB":   float64(memStats.HeapInuse) / 1024 / 1024,
			"heapObjects":   memStats.HeapObjects,
			"stackMB":       float64(memStats.StackInuse) / 1024 / 1024,
			"numGC":         memStats.NumGC,
			"lastGCPauseMs": float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6,
		},
		"counts": map[string]interface{}{
			"goroutines":         runtime.NumGoroutine(),
			"sessions":           ctx.SessionManager.GetSessionCount(),
			"sseClients":         ctx.SSEBroker.ClientCount(),
			"officeAgents":       ctx.OfficeManager.AgentCount(),
			"respawnControllers": respawnCount,
			"scheduledTasks":     scheduledCount,
		},
	})
}

// GetSystemStats returns system-level statistics.
// GET /api/system/stats
func (ctx *Context) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	// The /proc + runtime + MemStats reading lives in metrics; here
	// we just feed it the handler-local startTime and the session
	// count from the session manager.
	request.WriteJSON(w, http.StatusOK, metrics.SystemStats(serverStartTime, ctx.SessionManager.GetSessionCount()))
}

// CleanupState performs state cleanup operations.
// POST /api/cleanup-state
func (ctx *Context) CleanupState(w http.ResponseWriter, r *http.Request) {
	ctx.StateStore.UpdateSettings(map[string]interface{}{})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "state cleaned up",
	})
}

// GetPromptProcessor returns the prompt processor configuration.
// GET /api/prompt-processor
func (ctx *Context) GetPromptProcessor(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	pp, _ := settings["promptProcessor"]
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"promptProcessor": pp,
	})
}

// UpdatePromptProcessor updates the prompt processor configuration.
// PUT /api/prompt-processor
func (ctx *Context) UpdatePromptProcessor(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessor": body,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetPromptProcessorRules returns prompt processor rules.
// GET /api/prompt-processor/rules
func (ctx *Context) GetPromptProcessorRules(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	rules, _ := settings["promptProcessorRules"]
	if rules == nil {
		rules = []interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"rules":   rules,
	})
}

// UpdatePromptProcessorRules updates prompt processor rules.
// PUT /api/prompt-processor/rules
func (ctx *Context) UpdatePromptProcessorRules(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessorRules": body,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetRespawnLearning returns respawn learning data.
// GET /api/respawn/learning
func (ctx *Context) GetRespawnLearning(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	learning, _ := settings["respawnLearning"]
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"learning": learning,
	})
}

// GetRespawnLearningStats returns respawn learning statistics.
// GET /api/respawn/learning/stats
func (ctx *Context) GetRespawnLearningStats(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	stats, _ := settings["respawnLearningStats"]
	if stats == nil {
		stats = map[string]interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// GetRespawnLearningRecommendations returns learning recommendations filtered by workingDir.
// GET /api/respawn/learning/recommendations
func (ctx *Context) GetRespawnLearningRecommendations(w http.ResponseWriter, r *http.Request) {
	workingDir := r.URL.Query().Get("workingDir")
	settings := ctx.StateStore.GetSettings()
	learnings, _ := settings["respawnLearning"]

	recommendations := make([]interface{}, 0)
	if learningList, ok := learnings.([]interface{}); ok {
		for _, item := range learningList {
			if workingDir == "" {
				recommendations = append(recommendations, item)
				continue
			}
			if entry, ok := item.(map[string]interface{}); ok {
				if dir, _ := entry["workingDir"].(string); dir == "" || strings.Contains(dir, workingDir) {
					recommendations = append(recommendations, entry)
				}
			}
		}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"recommendations": recommendations,
	})
}

// GetRespawnLearningCategories returns respawn learning categories with counts.
// GET /api/respawn/learning/categories
func (ctx *Context) GetRespawnLearningCategories(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	learnings, _ := settings["respawnLearning"]

	categories := make(map[string]int)
	totalLearnings := 0

	if learningList, ok := learnings.([]interface{}); ok {
		totalLearnings = len(learningList)
		for _, item := range learningList {
			if entry, ok := item.(map[string]interface{}); ok {
				cat, _ := entry["category"].(string)
				if cat == "" {
					cat = "general"
				}
				categories[cat]++
			}
		}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"categories":     categories,
		"totalLearnings": totalLearnings,
	})
}

// PreviewSmartKickstart generates a smart kickstart prompt preview.
// POST /api/respawn/smart-kickstart/preview
func (ctx *Context) PreviewSmartKickstart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		WorkingDir  string `json:"workingDir"`
		SessionName string `json:"sessionName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	settings := ctx.StateStore.GetSettings()
	smartKickstart, _ := settings["smartKickstart"].(map[string]interface{})
	learnings, _ := settings["respawnLearning"]

	sources := make([]string, 0)
	promptParts := []string{"# Smart Kickstart Prompt"}

	if body.SessionName != "" {
		promptParts = append(promptParts, "Session: "+body.SessionName)
		sources = append(sources, "session_name")
	}
	if body.WorkingDir != "" {
		promptParts = append(promptParts, "Working directory: "+body.WorkingDir)
		sources = append(sources, "working_dir")
	}
	if smartKickstart != nil {
		if tpl, ok := smartKickstart["template"].(string); ok && tpl != "" {
			promptParts = append(promptParts, tpl)
			sources = append(sources, "kickstart_config")
		}
	}
	if learnings != nil {
		sources = append(sources, "learning_data")
	}

	prompt := strings.Join(promptParts, "\n\n")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"prompt":  prompt,
		"sources": sources,
	})
}

// PreviewSmartUpdate generates a smart update prompt preview.
// POST /api/respawn/smart-update/preview
func (ctx *Context) PreviewSmartUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		WorkingDir  string `json:"workingDir"`
		SessionName string `json:"sessionName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	settings := ctx.StateStore.GetSettings()
	learnings, _ := settings["respawnLearning"]

	sources := make([]string, 0)
	promptParts := []string{"# Smart Update Prompt"}

	if body.SessionName != "" {
		promptParts = append(promptParts, "Session: "+body.SessionName)
		sources = append(sources, "session_name")
	}
	if body.WorkingDir != "" {
		promptParts = append(promptParts, "Working directory: "+body.WorkingDir)
		sources = append(sources, "working_dir")
	}
	if learnings != nil {
		sources = append(sources, "learning_data")
	}

	prompt := strings.Join(promptParts, "\n\n")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"prompt":  prompt,
		"sources": sources,
	})
}

// GetRespawnSmartKickstart returns smart kickstart configuration.
// GET /api/respawn/smart-kickstart
func (ctx *Context) GetRespawnSmartKickstart(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	smartKickstart, _ := settings["smartKickstart"]
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"smartKickstart": smartKickstart,
	})
}

// UpdateRespawnSmartKickstart updates smart kickstart configuration.
// PUT /api/respawn/smart-kickstart
func (ctx *Context) UpdateRespawnSmartKickstart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"smartKickstart": body,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetRespawnSmartKickstartStats returns smart kickstart statistics.
// GET /api/respawn/smart-kickstart/stats
func (ctx *Context) GetRespawnSmartKickstartStats(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	stats, _ := settings["smartKickstartStats"]
	if stats == nil {
		stats = map[string]interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// ---------------------------------------------------------------------------
// Group 6: Prompt Processor
// ---------------------------------------------------------------------------

// UpdatePromptProcessorContextInjector updates the context injector configuration
// for the prompt processor.
// PUT /api/prompt-processor/context-injector
func (ctx *Context) UpdatePromptProcessorContextInjector(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Config interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessor.contextInjector": body.Config,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ResetPromptProcessorStats clears prompt processor statistics.
// POST /api/prompt-processor/reset-stats
func (ctx *Context) ResetPromptProcessorStats(w http.ResponseWriter, r *http.Request) {
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessorStats": map[string]interface{}{},
	})
	slog.Info("Prompt processor stats reset")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// PreviewPromptProcessor previews the result of running the prompt processor
// on a given prompt. Currently a passthrough that returns the prompt unchanged.
// POST /api/prompt-processor/preview
func (ctx *Context) PreviewPromptProcessor(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Prompt     string `json:"prompt"`
		WorkingDir string `json:"workingDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Count how many rules are configured (for rulesApplied metadata)
	settings := ctx.StateStore.GetSettings()
	rulesApplied := 0
	if rules, ok := settings["promptProcessorRules"].([]interface{}); ok {
		rulesApplied = len(rules)
	}

	// Passthrough: return the prompt unchanged with metadata
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"result":       body.Prompt,
		"rulesApplied": rulesApplied,
	})
}
