package handler

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/web/auth"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// GetAuthStatus returns the current authentication status.
// GET /api/auth/status
func (ctx *Context) GetAuthStatus(w http.ResponseWriter, r *http.Request) {
	passwordConfigured := ctx.AuthManager.IsPasswordConfigured()
	authenticated := false
	if passwordConfigured {
		authenticated = ctx.AuthManager.ValidateSessionFromRequest(r)
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"passwordConfigured": passwordConfigured,
		"authenticated":      authenticated,
	})
}

// AuthSetup handles initial password setup.
// POST /api/auth/setup
func (ctx *Context) AuthSetup(w http.ResponseWriter, r *http.Request) {
	if ctx.AuthManager.IsPasswordConfigured() {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Password already configured",
		})
		return
	}

	if !ctx.AuthManager.AcquireSetupLock() {
		request.WriteJSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "Setup already in progress",
		})
		return
	}
	defer ctx.AuthManager.ReleaseSetupLock()

	// Double-check after acquiring lock
	if ctx.AuthManager.IsPasswordConfigured() {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Password already configured",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16) // 64KB
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if validationErr := ctx.AuthManager.ValidatePasswordInput(body.Password); validationErr != "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   validationErr,
		})
		return
	}

	if err := ctx.AuthManager.SetPassword(body.Password); err != nil {
		slog.Error("Failed to set password", "error", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to set password")
		return
	}

	token := ctx.AuthManager.CreateSession()
	http.SetCookie(w, ctx.AuthManager.BuildSessionCookie(token, r))
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// AuthLogin handles login requests.
// POST /api/auth/login
func (ctx *Context) AuthLogin(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)

	blocked, retryAfterMs := ctx.AuthManager.IsRateLimited(ip)
	if blocked {
		request.WriteJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"success":      false,
			"error":        "Too many failed attempts",
			"retryAfterMs": retryAfterMs,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Password == "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Password is required",
		})
		return
	}
	if len(body.Password) > 128 {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid password",
		})
		return
	}

	valid, err := ctx.AuthManager.VerifyPassword(body.Password)
	if err != nil {
		slog.Error("Password verification failed", "error", err)
		request.WriteError(w, http.StatusInternalServerError, "authentication error")
		return
	}

	if !valid {
		rateLimited, retryMs := ctx.AuthManager.RecordFailedAttempt(ip)
		if rateLimited {
			request.WriteJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"success":      false,
				"error":        "Too many failed attempts",
				"retryAfterMs": retryMs,
			})
			return
		}
		request.WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Invalid password",
		})
		return
	}

	ctx.AuthManager.ResetRateLimit(ip)
	token := ctx.AuthManager.CreateSession()
	http.SetCookie(w, ctx.AuthManager.BuildSessionCookie(token, r))
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// AuthLogout handles logout requests.
// POST /api/auth/logout
func (ctx *Context) AuthLogout(w http.ResponseWriter, r *http.Request) {
	token := ctx.AuthManager.ParseSessionCookie(r)
	if token != "" {
		ctx.AuthManager.DestroySession(token)
	}
	http.SetCookie(w, ctx.AuthManager.BuildClearCookie(r))
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// AuthChangePassword handles password change requests.
// POST /api/auth/change-password
func (ctx *Context) AuthChangePassword(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)

	blocked, retryAfterMs := ctx.AuthManager.IsRateLimited(ip)
	if blocked {
		request.WriteJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"success":      false,
			"error":        "Too many failed attempts",
			"retryAfterMs": retryAfterMs,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.CurrentPassword == "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Current and new password are required",
		})
		return
	}
	if len(body.CurrentPassword) > 128 {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid current password",
		})
		return
	}

	if validationErr := ctx.AuthManager.ValidatePasswordInput(body.NewPassword); validationErr != "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   validationErr,
		})
		return
	}

	changed, err := ctx.AuthManager.ChangePassword(body.CurrentPassword, body.NewPassword)
	if err != nil {
		slog.Error("Password change failed", "error", err)
		request.WriteError(w, http.StatusInternalServerError, "password change error")
		return
	}
	if !changed {
		ctx.AuthManager.RecordFailedAttempt(ip)
		request.WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Current password is incorrect",
		})
		return
	}

	ctx.AuthManager.ResetRateLimit(ip)
	token := ctx.AuthManager.CreateSession()
	http.SetCookie(w, ctx.AuthManager.BuildSessionCookie(token, r))
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetClaudeAuthStatus returns the Claude API authentication status.
// Reads ~/.claude/.credentials.json to check if Claude CLI is authenticated.
// Also handles credential symlink persistence for Docker volume survival.
// GET /api/claude/auth-status
func (ctx *Context) GetClaudeAuthStatus(w http.ResponseWriter, r *http.Request) {
	homeDir, _ := os.UserHomeDir()
	credPath := filepath.Join(homeDir, ".claude", ".credentials.json")
	symlinkTarget := filepath.Join(homeDir, ".claude-auth", ".credentials.json")

	// Find the credentials file
	pathToRead := ""
	if fileExists(credPath) {
		pathToRead = credPath
	} else if fileExists(symlinkTarget) {
		pathToRead = symlinkTarget
	} else {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
			"reason":        "no_credentials",
		})
		return
	}

	data, err := os.ReadFile(pathToRead)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
			"reason":        "empty_credentials",
		})
		return
	}

	// Credential sync: if the credentials file is a real file (not symlink),
	// Claude Code replaced the symlink with atomic write. Persist to volume.
	info, err := os.Lstat(credPath)
	if err == nil && info.Mode()&os.ModeSymlink == 0 {
		// Regular file, not symlink -- persist and restore symlink
		os.MkdirAll(filepath.Dir(symlinkTarget), 0755)
		if copyErr := copyFile(credPath, symlinkTarget); copyErr == nil {
			os.Remove(credPath)
			os.Symlink(symlinkTarget, credPath)
			slog.Info("Persisted Claude credentials to volume and restored symlink")
		}
	}

	// Parse credentials JSON
	var creds map[string]interface{}
	if err := json.Unmarshal(data, &creds); err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
			"reason":        "invalid_credentials",
		})
		return
	}

	// Check claudeAiOauth
	if oauth, ok := creds["claudeAiOauth"].(map[string]interface{}); ok {
		if token, ok := oauth["accessToken"].(string); ok && token != "" {
			// Check expiry
			if expiresAt, ok := oauth["expiresAt"].(float64); ok && time.Now().UnixMilli() > int64(expiresAt) {
				request.WriteJSON(w, http.StatusOK, map[string]interface{}{
					"authenticated": false,
					"reason":        "token_expired",
					"expiresAt":     expiresAt,
				})
				return
			}
			subType, _ := oauth["subscriptionType"].(string)
			if subType == "" {
				subType = "unknown"
			}
			request.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated":    true,
				"method":           "subscription",
				"subscriptionType": subType,
				"expiresAt":        oauth["expiresAt"],
			})
			return
		}
	}

	// Check oauthAccount
	if account, ok := creds["oauthAccount"].(map[string]interface{}); ok {
		token, _ := account["accessToken"].(string)
		token2, _ := account["token"].(string)
		if token != "" || token2 != "" {
			if expiresAt, ok := account["expiresAt"].(float64); ok && time.Now().UnixMilli() > int64(expiresAt) {
				request.WriteJSON(w, http.StatusOK, map[string]interface{}{
					"authenticated": false,
					"reason":        "token_expired",
					"expiresAt":     expiresAt,
				})
				return
			}
			subType, _ := account["subscriptionType"].(string)
			if subType == "" {
				subType = "unknown"
			}
			request.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated":    true,
				"method":           "subscription",
				"subscriptionType": subType,
				"expiresAt":        account["expiresAt"],
			})
			return
		}
	}

	// Check apiKey
	if apiKey, ok := creds["apiKey"].(string); ok && apiKey != "" {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": true,
			"method":        "api_key",
		})
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": false,
		"reason":        "no_valid_token",
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

// GetScreens returns all active screen sessions.
// GET /api/screens
func (ctx *Context) GetScreens(w http.ResponseWriter, r *http.Request) {
	if ctx.ScreenManager == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"screens": []interface{}{},
		})
		return
	}
	screens := ctx.ScreenManager.GetScreens()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"screens": screens,
	})
}

// DeleteScreen removes a screen session.
// DELETE /api/screens/{sessionId}
func (ctx *Context) DeleteScreen(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if ctx.ScreenManager == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "screen manager not available")
		return
	}
	ok, err := ctx.ScreenManager.KillScreen(sessionID)
	if err != nil {
		slog.Warn("Failed to kill screen", "sessionId", sessionID, "error", err)
		request.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": ok,
	})
}

// ReconcileScreens reconciles screen sessions with internal state.
// POST /api/screens/reconcile
func (ctx *Context) ReconcileScreens(w http.ResponseWriter, r *http.Request) {
	if ctx.ScreenManager == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"screens": []interface{}{},
		})
		return
	}
	screens := ctx.ScreenManager.GetScreens()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"screens": screens,
	})
}

// GetSubagents returns all subagents.
// GET /api/subagents
func (ctx *Context) GetSubagents(w http.ResponseWriter, r *http.Request) {
	if ctx.SubagentWatcher == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"agents":  []interface{}{},
		})
		return
	}
	agents := ctx.SubagentWatcher.GetAgents()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"agents":  agents,
	})
}

// GetSubagent returns a single subagent by ID.
// GET /api/subagents/{agentId}
func (ctx *Context) GetSubagent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentId")
	if ctx.SubagentWatcher == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	agents := ctx.SubagentWatcher.GetAgents()
	for _, agent := range agents {
		if agent.AgentID == agentID {
			request.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"agent":   agent,
			})
			return
		}
	}
	request.WriteError(w, http.StatusNotFound, "agent not found: "+agentID)
}

// DeleteSubagent removes a subagent from tracking.
// DELETE /api/subagents/{agentId}
func (ctx *Context) DeleteSubagent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentId")
	if ctx.SubagentWatcher == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "subagent watcher not available")
		return
	}
	removed := ctx.SubagentWatcher.RemoveAgent(agentID)
	if !removed {
		slog.Info("Delete subagent: agent not found in tracking", "agentId", agentID)
	} else {
		slog.Info("Subagent removed from tracking", "agentId", agentID)
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"removed": removed,
	})
}

// GetSubagentWindowStates returns subagent window states.
// GET /api/subagent-window-states
func (ctx *Context) GetSubagentWindowStates(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	states, _ := settings["subagentWindowStates"]
	if states == nil {
		states = map[string]interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"states":  states,
	})
}

// UpdateSubagentWindowStates updates subagent window states.
// PUT /api/subagent-window-states
func (ctx *Context) UpdateSubagentWindowStates(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"subagentWindowStates": body,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetSubagentParents returns subagent parent relationships.
// GET /api/subagent-parents
func (ctx *Context) GetSubagentParents(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	parents, _ := settings["subagentParents"]
	if parents == nil {
		parents = map[string]interface{}{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"parents": parents,
	})
}

// UpdateSubagentParents updates subagent parent relationships.
// PUT /api/subagent-parents
func (ctx *Context) UpdateSubagentParents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"subagentParents": body,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ---------------------------------------------------------------------------
// Group 4: Screen Management
// ---------------------------------------------------------------------------

// StartScreenStats starts screen stats collection.
// POST /api/screens/stats/start
func (ctx *Context) StartScreenStats(w http.ResponseWriter, r *http.Request) {
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"screenStatsCollecting": true,
	})
	slog.Info("Screen stats collection started")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// StopScreenStats stops screen stats collection.
// POST /api/screens/stats/stop
func (ctx *Context) StopScreenStats(w http.ResponseWriter, r *http.Request) {
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"screenStatsCollecting": false,
	})
	slog.Info("Screen stats collection stopped")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ---------------------------------------------------------------------------
// Group 5: Subagent Management
// ---------------------------------------------------------------------------

// GetSessionSubagents returns subagents associated with a specific session by
// matching the session's working directory against subagent file paths.
// GET /api/sessions/{id}/subagents
func (ctx *Context) GetSessionSubagents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(sessionID)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found: "+sessionID)
		return
	}

	if ctx.SubagentWatcher == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"subagents": []interface{}{},
		})
		return
	}

	workingDir := session.WorkingDir
	agents := ctx.SubagentWatcher.GetAgents()
	matched := make([]interface{}, 0)
	for _, agent := range agents {
		// Match agents whose file path contains the session working dir
		if workingDir != "" && strings.Contains(agent.FilePath, workingDir) {
			matched = append(matched, agent)
		}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"subagents": matched,
	})
}

// GetSubagentTranscript returns the transcript for a specific subagent.
// GET /api/subagents/{agentId}/transcript
func (ctx *Context) GetSubagentTranscript(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentId")
	if ctx.SubagentWatcher == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}

	agent := ctx.SubagentWatcher.GetAgent(agentID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found: "+agentID)
		return
	}

	if agent.FilePath == "" {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"transcript": []interface{}{},
			"raw":        "",
		})
		return
	}

	data, err := os.ReadFile(agent.FilePath)
	if err != nil {
		slog.Warn("Failed to read subagent transcript", "agentId", agentID, "error", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to read transcript file")
		return
	}

	rawContent := string(data)

	// If format=raw, return the raw JSONL text
	if r.URL.Query().Get("format") == "raw" {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"transcript": []interface{}{},
			"raw":        rawContent,
		})
		return
	}

	// Parse each line into structured entries
	var entries []interface{}
	scanner := bufio.NewScanner(strings.NewReader(rawContent))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			// Include unparseable lines as raw text entries
			entries = append(entries, map[string]interface{}{
				"type": "raw",
				"text": line,
			})
			continue
		}
		entries = append(entries, parsed)
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"transcript": entries,
		"raw":        rawContent,
	})
}

// CleanupSubagents triggers an immediate cleanup of stale subagents.
// POST /api/subagents/cleanup
func (ctx *Context) CleanupSubagents(w http.ResponseWriter, r *http.Request) {
	if ctx.SubagentWatcher == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"cleaned": 0,
		})
		return
	}
	cleaned := ctx.SubagentWatcher.ForceCleanup()
	slog.Info("Subagent cleanup completed", "cleaned", cleaned)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"cleaned": cleaned,
	})
}

// ClearAllSubagents removes all tracked subagents from the watcher.
// DELETE /api/subagents
func (ctx *Context) ClearAllSubagents(w http.ResponseWriter, r *http.Request) {
	if ctx.SubagentWatcher == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
		return
	}
	count := ctx.SubagentWatcher.ClearAll()
	slog.Info("All subagents cleared", "count", count)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
