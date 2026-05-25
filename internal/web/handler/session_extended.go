package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/respawn"
	"github.com/claudeman/jarvis-go/internal/secpath"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// resolveSessionFilePath maps a query-supplied workdir-relative path
// to an absolute path inside the session's working directory, writing
// the right HTTP error and returning ok=false when validation fails.
// Centralises the three near-identical path-traversal checks the
// session-file handlers used to inline (audit caught them drifting
// out of sync — GetSessionTailFile had been missing the absolute-path
// rejection that the other two enforce).
func resolveSessionFilePath(w http.ResponseWriter, session *pty.Session, filePath string) (string, bool) {
	abs, err := secpath.ResolveUnderRoot(session.WorkingDir, filePath)
	switch {
	case err == nil:
		return abs, true
	case errors.Is(err, secpath.ErrPathAbsolute):
		request.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, secpath.ErrInvalidPath):
		request.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, secpath.ErrRootUnresolvable):
		request.WriteError(w, http.StatusInternalServerError, err.Error())
	default:
		request.WriteError(w, http.StatusForbidden, err.Error())
	}
	return "", false
}

// maxBulkItems caps the number of session IDs per bulk operation to prevent
// accidental DoS of the state store on enormous requests.
const maxBulkItems = 500

// atomicWriteFile writes data to path via a temp-file + rename so a
// crash mid-write can't leave a half-written plan file. The plan-file
// family below (@fix_plan.md, initprompt.md, @fix_plan.vN.md) all
// carry user-authored content; the append-task handler in particular
// does a read-modify-write that would wipe the entire plan if the
// direct WriteFile was interrupted. Same pattern round 4 applied to
// the template + memory stores.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// BulkDeleteSessions stops and removes multiple sessions in one call.
// POST /api/sessions/bulk-delete  { sessionIds: [...] }
// Returns { deleted: [...], failed: [{id, error}] } so clients can show
// partial-success feedback.
func (ctx *Context) BulkDeleteSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionIDs []string `json:"sessionIds"`
	}
	if !request.DecodeAndValidate(w, r, &req) {
		return
	}
	if len(req.SessionIDs) == 0 {
		request.WriteError(w, http.StatusBadRequest, "sessionIds is required")
		return
	}
	if len(req.SessionIDs) > maxBulkItems {
		request.WriteError(w, http.StatusBadRequest, "too many sessionIds (max 500)")
		return
	}

	deleted := make([]string, 0, len(req.SessionIDs))
	failed := make([]map[string]string, 0)
	for _, id := range req.SessionIDs {
		if err := ctx.SessionManager.StopSession(r.Context(), id); err != nil {
			failed = append(failed, map[string]string{"id": id, "error": err.Error()})
			continue
		}
		ctx.SSEBroker.Broadcast("session:deleted", map[string]string{"id": id})
		ctx.StateStore.RemoveSession(id)
		ctx.cleanupSessionResources(id)
		deleted = append(deleted, id)
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": deleted,
		"failed":  failed,
	})
}

// BulkRenameSessions renames multiple sessions in one call.
// POST /api/sessions/bulk-rename  { renames: { "<id>": "<newName>", ... } }
func (ctx *Context) BulkRenameSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Renames map[string]string `json:"renames"`
	}
	if !request.DecodeAndValidate(w, r, &req) {
		return
	}
	if len(req.Renames) == 0 {
		request.WriteError(w, http.StatusBadRequest, "renames is required")
		return
	}
	if len(req.Renames) > maxBulkItems {
		request.WriteError(w, http.StatusBadRequest, "too many renames (max 500)")
		return
	}

	renamed := make([]string, 0, len(req.Renames))
	failed := make([]map[string]string, 0)
	for id, newName := range req.Renames {
		if strings.TrimSpace(newName) == "" {
			failed = append(failed, map[string]string{"id": id, "error": "name cannot be empty"})
			continue
		}
		session, ok := ctx.SessionManager.GetSession(id)
		if !ok {
			failed = append(failed, map[string]string{"id": id, "error": "session not found"})
			continue
		}
		session.SetName(newName)
		renamed = append(renamed, id)
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"renamed": renamed,
		"failed":  failed,
	})
}

// BulkUpdateSessionColors assigns colors to multiple sessions in one call.
// POST /api/sessions/bulk-color  { colors: { "<id>": "<color>", ... } }
func (ctx *Context) BulkUpdateSessionColors(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Colors map[string]string `json:"colors"`
	}
	if !request.DecodeAndValidate(w, r, &req) {
		return
	}
	if len(req.Colors) == 0 {
		request.WriteError(w, http.StatusBadRequest, "colors is required")
		return
	}
	if len(req.Colors) > maxBulkItems {
		request.WriteError(w, http.StatusBadRequest, "too many colors (max 500)")
		return
	}

	updated := make([]string, 0, len(req.Colors))
	failed := make([]map[string]string, 0)
	ctx.SessionColorsMu.Lock()
	for id, color := range req.Colors {
		if _, ok := ctx.SessionManager.GetSession(id); !ok {
			failed = append(failed, map[string]string{"id": id, "error": "session not found"})
			continue
		}
		ctx.SessionColors[id] = color
		updated = append(updated, id)
	}
	ctx.SessionColorsMu.Unlock()

	// Broadcast color updates so any other open tabs reflect the change.
	for _, id := range updated {
		ctx.SSEBroker.Broadcast("session:color", map[string]string{
			"id":    id,
			"color": req.Colors[id],
		})
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"updated": updated,
		"failed":  failed,
	})
}

// RenameSession updates a session's display name.
// PUT /api/sessions/{id}/name
func (ctx *Context) RenameSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req RenameSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		request.WriteError(w, http.StatusBadRequest, "name cannot be empty")
		return
	}

	session.SetName(req.Name)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// RenameSessionRequest is the body for PUT /api/sessions/{id}/name.
type RenameSessionRequest struct {
	Name string `json:"name"`
}

// sessionInputBody is the body for endpoints that send input to a session.
type sessionInputBody struct {
	Input string `json:"input"`
}

// UpdateSessionColor updates a session's color label.
// PUT /api/sessions/{id}/color
func (ctx *Context) UpdateSessionColor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx.SessionColorsMu.Lock()
	ctx.SessionColors[id] = req.Color
	ctx.SessionColorsMu.Unlock()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"color":   req.Color,
	})
}

// DeleteAllSessions stops and removes all sessions.
// DELETE /api/sessions
func (ctx *Context) DeleteAllSessions(w http.ResponseWriter, r *http.Request) {
	count := ctx.SessionManager.GetSessionCount()
	_ = ctx.SessionManager.StopAllSessions(r.Context())
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"killed":  count,
	})
}

// GetSessionOutput returns the parsed output of a session.
// GET /api/sessions/{id}/output
func (ctx *Context) GetSessionOutput(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  session.TextOutput(),
	})
}

// GetSessionRalphState returns the Ralph AI state for a session.
// GET /api/sessions/{id}/ralph-state
func (ctx *Context) GetSessionRalphState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	ctx.RalphTrackersMu.RLock()
	tracker, hasTracker := ctx.RalphTrackers[id]
	ctx.RalphTrackersMu.RUnlock()

	if hasTracker {
		st := tracker.GetState()
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"sessionId": id,
			"state": map[string]interface{}{
				"enabled":            st.Enabled,
				"completionPhrase":   st.CompletionPhrase,
				"loopIteration":      st.LoopIteration,
				"maxIterations":      st.MaxIterations,
				"todos":              st.Todos,
				"lastActivityAt":     st.LastActivityAt,
				"completionDetected": st.CompletionDetected,
			},
		})
	} else {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"sessionId": id,
			"state": map[string]interface{}{
				"enabled":       false,
				"todos":         []interface{}{},
				"loopIteration": 0,
			},
		})
	}
}

// sessionWorkingDir returns the working directory for a session, trying
// PTY sessions first then SDK sessions. Returns ok=false if neither
// has the given id. Used by the status endpoints below so office
// agents (SDK-mode) aren't 404'd every time the dashboard asks for
// their git/build/test/hmr status -- observed on 2026-04-18 15:01:
// Ops' git-status returned 404 because GetSession only searched PTY.
func (ctx *Context) sessionWorkingDir(id string) (string, bool) {
	if s, ok := ctx.SessionManager.GetSession(id); ok {
		return s.WorkingDir, true
	}
	if sdk, ok := ctx.SessionManager.GetSDKSession(id); ok {
		return sdk.WorkingDir, true
	}
	return "", false
}

// GetSessionGitStatus returns git status for a session's working directory.
// GET /api/sessions/{id}/git-status
func (ctx *Context) GetSessionGitStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workingDir, ok := ctx.sessionWorkingDir(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	status := ctx.IntegrationMonitor.RefreshGitStatus(id, workingDir)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  status,
	})
}

// GetSessionBuildStatus returns build status for a session.
// GET /api/sessions/{id}/build-status
func (ctx *Context) GetSessionBuildStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := ctx.sessionWorkingDir(id); !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("build-status-%s", id)
	settings := ctx.StateStore.GetSettings()
	status := settings[key]

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  status,
	})
}

// GetSessionTestStatus returns test runner status for a session.
// GET /api/sessions/{id}/test-status
func (ctx *Context) GetSessionTestStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := ctx.sessionWorkingDir(id); !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("test-status-%s", id)
	settings := ctx.StateStore.GetSettings()
	status := settings[key]

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  status,
	})
}

// GetSessionHMRStatus returns hot-module-reload status for a session.
// GET /api/sessions/{id}/hmr-status
func (ctx *Context) GetSessionHMRStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := ctx.sessionWorkingDir(id); !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("hmr-status-%s", id)
	settings := ctx.StateStore.GetSettings()
	status := settings[key]

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  status,
	})
}

// GetSessionIntegrations returns configured integrations for a session.
// GET /api/sessions/{id}/integrations
func (ctx *Context) GetSessionIntegrations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"integrations": ctx.IntegrationMonitor.GetStatus(id),
		"sessionId":    id,
	})
}

// GetSessionRunSummary returns the run summary for a session.
// GET /api/sessions/{id}/run-summary
func (ctx *Context) GetSessionRunSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	ctx.RunSummariesMu.RLock()
	rsTracker, hasRS := ctx.RunSummaries[id]
	ctx.RunSummariesMu.RUnlock()

	if hasRS {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"summary": rsTracker.GetSummary(),
		})
	} else {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"summary": map[string]interface{}{
				"sessionId": id,
				"events":    []interface{}{},
			},
		})
	}
}

// GetSessionActiveTools returns currently active tools for a session.
// GET /api/sessions/{id}/active-tools
func (ctx *Context) GetSessionActiveTools(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}
	activeTools := session.GetActiveTools()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"tools":     activeTools,
		"sessionId": id,
	})
}

// GetSessionFiles returns the file list for a session's working directory.
// GET /api/sessions/{id}/files
func (ctx *Context) GetSessionFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	entries, err := os.ReadDir(session.WorkingDir)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"files":      []interface{}{},
			"workingDir": session.WorkingDir,
			"error":      "failed to read directory: " + err.Error(),
		})
		return
	}

	limit := 100
	if len(entries) > limit {
		entries = entries[:limit]
	}

	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		var size int64
		if infoErr == nil {
			size = info.Size()
		}
		files = append(files, map[string]interface{}{
			"name":  entry.Name(),
			"isDir": entry.IsDir(),
			"size":  size,
		})
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"files":      files,
		"workingDir": session.WorkingDir,
	})
}

// RunSession sends a run command to a session.
// POST /api/sessions/{id}/run
func (ctx *Context) RunSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req sessionInputBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sent := session.WriteViaScreen(req.Input + "\r")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": sent,
	})
}

// SessionInteractive sends interactive input to a session.
// POST /api/sessions/{id}/interactive
func (ctx *Context) SessionInteractive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req sessionInputBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sent := session.WriteViaScreen(req.Input + "\r")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": sent,
	})
}

// SessionShell sends a shell command to a session.
// POST /api/sessions/{id}/shell
func (ctx *Context) SessionShell(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req sessionInputBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sent := session.WriteViaScreen(req.Input + "\r")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": sent,
	})
}

// SessionResize resizes a session's terminal.
// POST /api/sessions/{id}/resize
func (ctx *Context) SessionResize(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		// SDK sessions have no PTY to resize — xterm.js fit-addon firing
		// on window resize would otherwise 404 and clutter logs. Return
		// 200 no-op so the frontend stays quiet; genuine typos on an
		// unknown session ID still get 404.
		if _, sdk := ctx.SessionManager.GetSDKSession(id); sdk {
			request.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"noop":    "sdk session has no pty to resize",
			})
			return
		}
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Cols <= 0 || req.Rows <= 0 {
		request.WriteError(w, http.StatusBadRequest, "cols and rows must be positive")
		return
	}

	if err := session.Resize(uint16(req.Cols), uint16(req.Rows)); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "resize failed: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetSessionRespawn returns the respawn state for a session.
// GET /api/sessions/{id}/respawn
func (ctx *Context) GetSessionRespawn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"state":      rc.State().String(),
		"confidence": rc.Confidence(),
		"cycleCount": rc.CycleCount(),
		"timers":     rc.ActiveTimers(),
	})
}

// GetSessionRespawnConfig returns the respawn configuration.
// GET /api/sessions/{id}/respawn/config
func (ctx *Context) GetSessionRespawnConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"config":  rc.Config(),
	})
}

// StartSessionRespawn starts the respawn loop for a session.
// POST /api/sessions/{id}/respawn/start
func (ctx *Context) StartSessionRespawn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)
	if err := rc.Start(context.Background()); err != nil {
		request.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// StopSessionRespawn stops the respawn loop for a session.
// POST /api/sessions/{id}/respawn/stop
func (ctx *Context) StopSessionRespawn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)
	rc.Stop()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// UpdateSessionRespawnConfig updates the respawn configuration.
// PUT /api/sessions/{id}/respawn/config
func (ctx *Context) UpdateSessionRespawnConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var cfg respawn.RespawnConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	rc.UpdateConfig(cfg)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// EnableSessionRespawn enables respawn for a session.
// POST /api/sessions/{id}/respawn/enable
func (ctx *Context) EnableSessionRespawn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc := ctx.GetOrCreateRespawnController(id)
	if err := rc.Start(context.Background()); err != nil {
		request.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// SessionAutoClear toggles auto-clear for a session.
// When enabled with threshold 0, immediately sends /clear to the session.
// Otherwise, the threshold-based auto-triggering happens via the session
// output monitor (Workstream 7).
// POST /api/sessions/{id}/auto-clear
func (ctx *Context) SessionAutoClear(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled   bool `json:"enabled"`
		Threshold int  `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("auto-clear-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		key: map[string]interface{}{
			"enabled":   req.Enabled,
			"threshold": req.Threshold,
		},
	})

	// If enabled with threshold 0 or current tokens exceed threshold,
	// send /clear immediately.
	if req.Enabled && req.Threshold == 0 {
		session.WriteViaScreen("/clear\r")
	} else if req.Enabled && req.Threshold > 0 {
		_, _, totalTokens := session.Tokens()
		if totalTokens > 0 && int(totalTokens) >= req.Threshold {
			session.WriteViaScreen("/clear\r")
		}
	}

	ctx.SSEBroker.Broadcast("session:autoClear", map[string]interface{}{
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})
}

// SessionAutoCompact toggles auto-compact for a session.
// When enabled with threshold 0, immediately sends /compact to the session.
// Otherwise, the threshold-based auto-triggering happens via the session
// output monitor (Workstream 7).
// POST /api/sessions/{id}/auto-compact
func (ctx *Context) SessionAutoCompact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled   bool `json:"enabled"`
		Threshold int  `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("auto-compact-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		key: map[string]interface{}{
			"enabled":   req.Enabled,
			"threshold": req.Threshold,
		},
	})

	// If enabled with threshold 0 or current tokens exceed threshold,
	// send /compact immediately.
	if req.Enabled && req.Threshold == 0 {
		session.WriteViaScreen("/compact\r")
	} else if req.Enabled && req.Threshold > 0 {
		_, _, totalTokens := session.Tokens()
		if totalTokens > 0 && int(totalTokens) >= req.Threshold {
			session.WriteViaScreen("/compact\r")
		}
	}

	ctx.SSEBroker.Broadcast("session:autoCompact", map[string]interface{}{
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})
}

// SessionAutoTrim toggles auto-trim for a session.
// When enabled with threshold 0, immediately sends /compact to the session.
// Otherwise, the threshold-based auto-triggering happens via the session
// output monitor (Workstream 7).
// POST /api/sessions/{id}/auto-trim
func (ctx *Context) SessionAutoTrim(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled   bool `json:"enabled"`
		Threshold int  `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("auto-trim-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		key: map[string]interface{}{
			"enabled":   req.Enabled,
			"threshold": req.Threshold,
		},
	})

	// If enabled with threshold 0 or current tokens exceed threshold,
	// send /compact immediately as a trim action.
	if req.Enabled && req.Threshold == 0 {
		session.WriteViaScreen("/compact\r")
	} else if req.Enabled && req.Threshold > 0 {
		_, _, totalTokens := session.Tokens()
		if totalTokens > 0 && int(totalTokens) >= req.Threshold {
			session.WriteViaScreen("/compact\r")
		}
	}

	ctx.SSEBroker.Broadcast("session:autoTrim", map[string]interface{}{
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"enabled":   req.Enabled,
		"threshold": req.Threshold,
	})
}

// SessionImageWatcher toggles the image watcher for a session.
// POST /api/sessions/{id}/image-watcher
func (ctx *Context) SessionImageWatcher(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("image-watcher-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{key: req.Enabled})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"enabled":   req.Enabled,
	})
}

// SessionFlickerFilter toggles the flicker filter for a session.
// POST /api/sessions/{id}/flicker-filter
func (ctx *Context) SessionFlickerFilter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("flicker-filter-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{key: req.Enabled})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"enabled":   req.Enabled,
	})
}

// GetSessionCPULimit returns the CPU limit for a session.
// GET /api/sessions/{id}/cpu-limit
func (ctx *Context) GetSessionCPULimit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("cpu-limit-%s", id)
	settings := ctx.StateStore.GetSettings()
	limit := settings[key]

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"limit":     limit,
		"sessionId": id,
	})
}

// SetSessionCPULimit sets the CPU limit for a session.
// POST /api/sessions/{id}/cpu-limit
func (ctx *Context) SetSessionCPULimit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Limit interface{} `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := fmt.Sprintf("cpu-limit-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{key: req.Limit})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sessionId": id,
		"limit":     req.Limit,
	})
}

// SetSessionRalphConfig updates the Ralph configuration for a session.
// POST /api/sessions/{id}/ralph-config
func (ctx *Context) SetSessionRalphConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Persist to state store
	key := fmt.Sprintf("ralph-config-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{key: body})

	// Also configure the live Ralph tracker
	ctx.RalphTrackersMu.RLock()
	tracker, hasTracker := ctx.RalphTrackers[id]
	ctx.RalphTrackersMu.RUnlock()
	if hasTracker {
		phrase, _ := body["completionPhrase"].(string)
		maxIter := 0
		if v, ok := body["maxIterations"].(float64); ok {
			maxIter = int(v)
		}
		tracker.Configure(phrase, maxIter)
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ResetRalphCircuitBreaker resets the Ralph circuit breaker for a session.
// POST /api/sessions/{id}/ralph-circuit-breaker/reset
func (ctx *Context) ResetRalphCircuitBreaker(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("ralph-cb-reset-%s", id)
	ctx.StateStore.UpdateSettings(map[string]interface{}{key: fmt.Sprintf("%d", time.Now().UnixMilli())})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetSessionRalphStatus returns the Ralph status for a session.
// GET /api/sessions/{id}/ralph-status
func (ctx *Context) GetSessionRalphStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	key := fmt.Sprintf("ralph-config-%s", id)
	settings := ctx.StateStore.GetSettings()
	config := settings[key]
	if config == nil {
		config = map[string]interface{}{
			"enabled": false,
		}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  config,
	})
}

// GetSessionContext returns context information for a session.
// GET /api/sessions/{id}/context
func (ctx *Context) GetSessionContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}
	_, _, total := session.Tokens()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"tokenCount": total,
	})
}

// TrimSessionContext trims the context for a session.
// POST /api/sessions/{id}/context/trim
func (ctx *Context) TrimSessionContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	sent := session.WriteViaScreen("/compact\r")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": sent,
	})
}

// SessionInteractiveRespawn performs an interactive respawn for a session.
// POST /api/sessions/{id}/interactive-respawn
func (ctx *Context) SessionInteractiveRespawn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	rc := ctx.GetOrCreateRespawnController(id)
	rc.Stop()
	if err := rc.Start(context.Background()); err != nil {
		request.WriteError(w, http.StatusConflict, "failed to restart respawn: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ExportSession exports a session's data.
// GET /api/sessions/{id}/export
func (ctx *Context) ExportSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}
	inputTokens, outputTokens, totalTokens := session.Tokens()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"session": map[string]interface{}{
			"id":           session.ID,
			"name":         session.Name(),
			"workingDir":   session.WorkingDir,
			"status":       session.Status(),
			"createdAt":    session.CreatedAt,
			"isWorking":    session.IsWorking(),
			"pid":          session.PID(),
			"inputTokens":  inputTokens,
			"outputTokens": outputTokens,
			"totalTokens":  totalTokens,
			"output":       session.TextOutput(),
		},
	})
}

// ImportSession imports session data by creating a new session from exported JSON.
// POST /api/sessions/import
func (ctx *Context) ImportSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		WorkingDir string `json:"workingDir"`
		Mode       string `json:"mode"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.WorkingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required")
		return
	}

	mode := pty.ModeShell
	if req.Mode == "claude" {
		mode = pty.ModeClaude
	}

	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir: req.WorkingDir,
			Mode:       mode,
			Name:       req.Name,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to create session: "+err.Error())
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

// GetSessionFileContent returns content of a file in a session's workspace.
// GET /api/sessions/{id}/file-content
func (ctx *Context) GetSessionFileContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		request.WriteError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}
	absPath, pathOK := resolveSessionFilePath(w, session, filePath)
	if !pathOK {
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, "file not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"content": string(data),
		"path":    absPath,
	})
}

// GetSessionFixPlan returns the current fix plan for a session.
// GET /api/sessions/{id}/fix-plan
func (ctx *Context) GetSessionFixPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"plan":    nil,
		})
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"plan":    string(data),
	})
}

// ImportSessionFixPlan imports a fix plan into a session.
// POST /api/sessions/{id}/fix-plan/import
func (ctx *Context) ImportSessionFixPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	if err := atomicWriteFile(planPath, []byte(req.Content), 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write fix plan: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// WriteSessionFixPlan writes a fix plan to a session.
// POST /api/sessions/{id}/fix-plan/write
func (ctx *Context) WriteSessionFixPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	if err := atomicWriteFile(planPath, []byte(req.Content), 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write fix plan: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ReadSessionFixPlan reads a fix plan from a session.
// POST /api/sessions/{id}/fix-plan/read
func (ctx *Context) ReadSessionFixPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"plan":    nil,
		})
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"plan":    string(data),
	})
}

// WriteSessionRalphPrompt writes a Ralph prompt for a session.
// POST /api/sessions/{id}/ralph-prompt/write
func (ctx *Context) WriteSessionRalphPrompt(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	promptPath := filepath.Join(session.WorkingDir, "initprompt.md")
	if err := atomicWriteFile(promptPath, []byte(req.Prompt), 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write ralph prompt: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// CheckpointSessionPlan creates a checkpoint for the session's plan.
// POST /api/sessions/{id}/plan/checkpoint
func (ctx *Context) CheckpointSessionPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "no fix plan to checkpoint")
		return
	}

	// Get and increment version counter from StateStore
	versionKey := fmt.Sprintf("plan-version-%s", id)
	settings := ctx.StateStore.GetSettings()
	var version int
	if raw, exists := settings[versionKey]; exists {
		switch v := raw.(type) {
		case float64:
			version = int(v)
		case string:
			version, _ = strconv.Atoi(v)
		}
	}
	version++
	ctx.StateStore.UpdateSettings(map[string]interface{}{versionKey: float64(version)})

	versionPath := filepath.Join(session.WorkingDir, fmt.Sprintf("@fix_plan.v%d.md", version))
	if err := atomicWriteFile(versionPath, data, 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write checkpoint: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"version": version,
	})
}

// GetSessionPlanHistory returns the plan version history for a session.
// GET /api/sessions/{id}/plan/history
func (ctx *Context) GetSessionPlanHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	entries, err := os.ReadDir(session.WorkingDir)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"versions": []interface{}{},
		})
		return
	}

	var versions []map[string]interface{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "@fix_plan.v") || !strings.HasSuffix(name, ".md") {
			continue
		}
		// Extract version number from @fix_plan.v{N}.md
		vStr := strings.TrimPrefix(name, "@fix_plan.v")
		vStr = strings.TrimSuffix(vStr, ".md")
		vNum, err := strconv.Atoi(vStr)
		if err != nil {
			continue
		}
		info, _ := entry.Info()
		var modTime interface{}
		if info != nil {
			modTime = info.ModTime()
		}
		versions = append(versions, map[string]interface{}{
			"version":  vNum,
			"filename": name,
			"modTime":  modTime,
		})
	}
	if versions == nil {
		versions = []map[string]interface{}{}
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"versions": versions,
	})
}

// RollbackSessionPlan rolls back a session's plan to a specific version.
// POST /api/sessions/{id}/plan/rollback/{version}
func (ctx *Context) RollbackSessionPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	versionStr := chi.URLParam(r, "version")
	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	versionPath := filepath.Join(session.WorkingDir, fmt.Sprintf("@fix_plan.v%d.md", versionNum))
	data, err := os.ReadFile(versionPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, fmt.Sprintf("version %d not found", versionNum))
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read version file: "+err.Error())
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	if err := atomicWriteFile(planPath, data, 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write fix plan: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"restoredVersion": versionNum,
	})
}

// SessionPlanTask creates or updates a task in the session's plan.
// POST /api/sessions/{id}/plan/task
func (ctx *Context) SessionPlanTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Task string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")

	// Read existing plan content (may not exist)
	existing, _ := os.ReadFile(planPath)
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "- [ ] " + req.Task + "\n"

	if err := atomicWriteFile(planPath, []byte(content), 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to append task: "+err.Error())
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// --- Group 1: File Operations ---

// GetSessionFileRaw serves raw file content with proper MIME type detection.
// For binary files (images, etc.), content is base64-encoded.
// For text files, content is returned as-is.
// GET /api/sessions/{id}/file-raw
func (ctx *Context) GetSessionFileRaw(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		request.WriteError(w, http.StatusBadRequest, "missing path parameter")
		return
	}
	absPath, pathOK := resolveSessionFilePath(w, session, filePath)
	if !pathOK {
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, "file not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read file: "+err.Error())
		return
	}

	// Detect content type using http.DetectContentType (sniffs first 512 bytes)
	mimeType := http.DetectContentType(data)

	// Check if the content is textual
	isText := strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/javascript"

	// Also check by file extension for common text types that sniffing may miss
	ext := strings.ToLower(filepath.Ext(absPath))
	textExtensions := map[string]string{
		".txt": "text/plain", ".md": "text/markdown", ".json": "application/json",
		".js": "application/javascript", ".ts": "text/plain", ".tsx": "text/plain",
		".jsx": "text/plain", ".go": "text/plain", ".py": "text/plain",
		".rs": "text/plain", ".rb": "text/plain", ".java": "text/plain",
		".css": "text/css", ".html": "text/html", ".xml": "application/xml",
		".yaml": "text/plain", ".yml": "text/plain", ".toml": "text/plain",
		".sh": "text/plain", ".bash": "text/plain", ".zsh": "text/plain",
		".svg": "image/svg+xml",
	}
	if extMime, found := textExtensions[ext]; found {
		mimeType = extMime
		isText = true
	}

	// Binary MIME types by extension (override sniffing for known binary types)
	binaryExtensions := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon",
		".bmp": "image/bmp", ".mp4": "video/mp4", ".webm": "video/webm",
		".mov": "video/quicktime", ".mp3": "audio/mpeg", ".wav": "audio/wav",
		".ogg": "audio/ogg", ".pdf": "application/pdf",
	}
	if binMime, found := binaryExtensions[ext]; found {
		mimeType = binMime
		isText = false
	}

	if isText {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"content":  string(data),
			"mimeType": mimeType,
			"encoding": "utf-8",
		})
	} else {
		encoded := base64.StdEncoding.EncodeToString(data)
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"content":  encoded,
			"mimeType": mimeType,
			"encoding": "base64",
		})
	}
}

// GetSessionTailFile streams file changes via SSE (Server-Sent Events).
// Opens a file and polls for new content every 500ms, sending new lines as SSE data events.
// GET /api/sessions/{id}/tail-file
func (ctx *Context) GetSessionTailFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		request.WriteError(w, http.StatusBadRequest, "missing path parameter")
		return
	}
	// Tightening note: pre-extraction this path silently accepted
	// absolute filePaths if they happened to live inside workingDir.
	// resolveSessionFilePath rejects absolute inputs across the
	// board — same behaviour the other two session-file handlers
	// already enforced. Dashboard JS only ever sends relative
	// paths, so the tightening is invisible to legitimate callers.
	absPath, pathOK := resolveSessionFilePath(w, session, filePath)
	if !pathOK {
		return
	}

	// Open the file
	f, err := os.Open(absPath)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "file not found: "+err.Error())
		return
	}

	// Seek to end
	_, err = f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		request.WriteError(w, http.StatusInternalServerError, "failed to seek file")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	// Generate stream ID
	streamID := uuid.New().String()

	// Create a cancellation context for this stream
	streamCtx, cancel := context.WithCancel(r.Context())

	// Store the cancel func for external cleanup
	ctx.TailFileStreamsMu.Lock()
	if ctx.TailFileStreams == nil {
		ctx.TailFileStreams = make(map[string]func())
	}
	ctx.TailFileStreams[streamID] = cancel
	ctx.TailFileStreamsMu.Unlock()

	// Notify client of successful connection
	connMsg, _ := json.Marshal(map[string]interface{}{
		"type": "connected", "streamId": streamID, "filePath": absPath,
	})
	fmt.Fprintf(w, "data: %s\n\n", connMsg)
	if canFlush {
		flusher.Flush()
	}

	// Poll for new content
	buf := make([]byte, 4096)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer f.Close()
	defer func() {
		ctx.TailFileStreamsMu.Lock()
		delete(ctx.TailFileStreams, streamID)
		ctx.TailFileStreamsMu.Unlock()
	}()

	for {
		select {
		case <-streamCtx.Done():
			endMsg, _ := json.Marshal(map[string]string{"type": "end"})
			fmt.Fprintf(w, "data: %s\n\n", endMsg)
			if canFlush {
				flusher.Flush()
			}
			return
		case <-ticker.C:
			n, readErr := f.Read(buf)
			if n > 0 {
				dataMsg, _ := json.Marshal(map[string]interface{}{
					"type": "data", "content": string(buf[:n]),
				})
				fmt.Fprintf(w, "data: %s\n\n", dataMsg)
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil && readErr != io.EOF {
				errMsg, _ := json.Marshal(map[string]interface{}{
					"type": "error", "error": readErr.Error(),
				})
				fmt.Fprintf(w, "data: %s\n\n", errMsg)
				if canFlush {
					flusher.Flush()
				}
				return
			}
		}
	}
}

// DeleteSessionTailFile closes an active file-tail stream.
// DELETE /api/sessions/{id}/tail-file/{streamId}
func (ctx *Context) DeleteSessionTailFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	streamID := chi.URLParam(r, "streamId")

	ctx.TailFileStreamsMu.Lock()
	cancelFn, found := ctx.TailFileStreams[streamID]
	if found {
		cancelFn()
		delete(ctx.TailFileStreams, streamID)
	}
	ctx.TailFileStreamsMu.Unlock()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": found,
	})
}

// --- Group 3: Plan Task Update ---

// PatchSessionPlanTask updates a specific plan task's status, attempts, or error.
// Reads the session's @fix_plan.md, finds the task by ID (line index), updates it, writes back.
// PATCH /api/sessions/{id}/plan/task/{taskId}
func (ctx *Context) PatchSessionPlanTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	taskIDStr := chi.URLParam(r, "taskId")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var update struct {
		Status            string `json:"status"`
		Error             string `json:"error"`
		IncrementAttempts bool   `json:"incrementAttempts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Check the Ralph tracker first
	ctx.RalphTrackersMu.RLock()
	tracker, hasTracker := ctx.RalphTrackers[id]
	ctx.RalphTrackersMu.RUnlock()

	if hasTracker && tracker != nil {
		result := tracker.UpdatePlanTask(taskIDStr, update.Status, update.Error, update.IncrementAttempts)
		if result {
			ctx.SSEBroker.Broadcast("session:planTaskUpdate", map[string]interface{}{
				"sessionId": id,
				"taskId":    taskIDStr,
				"status":    update.Status,
			})
			request.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"taskId":  taskIDStr,
			})
			return
		}
	}

	// File-based fallback: read @fix_plan.md, find task by index, update checkbox
	planPath := filepath.Join(session.WorkingDir, "@fix_plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "no fix plan found")
		return
	}

	taskIndex, err := strconv.Atoi(taskIDStr)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid task ID (must be numeric index)")
		return
	}

	lines := strings.Split(string(data), "\n")
	taskCount := 0
	updated := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [") {
			if taskCount == taskIndex {
				// Update the checkbox based on status
				switch update.Status {
				case "completed":
					lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
					lines[i] = strings.Replace(lines[i], "- [-]", "- [x]", 1)
				case "in_progress":
					lines[i] = strings.Replace(line, "- [ ]", "- [-]", 1)
					lines[i] = strings.Replace(lines[i], "- [x]", "- [-]", 1)
				case "pending":
					lines[i] = strings.Replace(line, "- [x]", "- [ ]", 1)
					lines[i] = strings.Replace(lines[i], "- [-]", "- [ ]", 1)
				case "failed":
					lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
					if update.Error != "" && !strings.Contains(lines[i], "FAILED") {
						lines[i] += " (FAILED: " + update.Error + ")"
					}
				}
				updated = true
				break
			}
			taskCount++
		}
	}

	if !updated {
		request.WriteError(w, http.StatusNotFound, "task not found at index "+taskIDStr)
		return
	}

	newContent := strings.Join(lines, "\n")
	if err := atomicWriteFile(planPath, []byte(newContent), 0644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write fix plan: "+err.Error())
		return
	}

	ctx.SSEBroker.Broadcast("session:planTaskUpdate", map[string]interface{}{
		"sessionId": id,
		"taskId":    taskIDStr,
		"status":    update.Status,
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"taskId":  taskIDStr,
	})
}
