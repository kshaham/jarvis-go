package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/ralph"
	"github.com/claudeman/jarvis-go/internal/respawn"
	"github.com/claudeman/jarvis-go/internal/runsummary"
	"github.com/claudeman/jarvis-go/internal/state"
	"github.com/claudeman/jarvis-go/internal/web/request"
	"github.com/claudeman/jarvis-go/internal/web/sse"
)

// respawnSessionWriter adapts a pty.Session to the respawn.SessionWriter interface.
type respawnSessionWriter struct {
	session *pty.Session
}

func (w *respawnSessionWriter) WriteToSession(_ string, text string) bool {
	return w.session.WriteViaScreen(text)
}

// Compile-time check that respawnSessionWriter implements respawn.SessionWriter.
var _ respawn.SessionWriter = (*respawnSessionWriter)(nil)

type sessionJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Mode       string `json:"mode"`
	WorkingDir string `json:"workingDir"`
	PID        int    `json:"pid"`
	CreatedAt  string `json:"createdAt"`
	IsWorking  bool   `json:"isWorking"`
}

func sessionToJSON(s *pty.Session) sessionJSON {
	return sessionJSON{
		ID:         s.ID,
		Name:       s.Name(),
		Status:     string(s.Status()),
		Mode:       string(s.Mode),
		WorkingDir: s.WorkingDir,
		PID:        s.PID(),
		CreatedAt:  s.CreatedAt.Format(time.RFC3339),
		IsWorking:  s.IsWorking(),
	}
}

// ListSessions returns all sessions as a bare array.
// The frontend calls sessions.find() directly on the response, so it must be an array.
func (ctx *Context) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := ctx.SessionManager.GetAllSessions()
	result := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, sessionToJSON(s))
	}
	// Include SDK sessions
	ctx.SessionManager.ForEachSDKSession(func(id string, sdk *pty.SDKSession) {
		result = append(result, sessionJSON{
			ID:         sdk.ID,
			Name:       sdk.GetName(),
			Status:     string(sdk.GetStatus()),
			Mode:       string(sdk.Mode),
			WorkingDir: sdk.WorkingDir,
			PID:        0,
			CreatedAt:  sdk.CreatedAt.Format(time.RFC3339),
			IsWorking:  sdk.IsWorking(),
		})
	})
	request.WriteJSON(w, http.StatusOK, result)
}

// GetSession returns a single session.
func (ctx *Context) GetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	input, output, total := session.Tokens()
	data := sessionToJSON(session)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"session": data,
		"tokens": map[string]int64{
			"input":  input,
			"output": output,
			"total":  total,
		},
	})
}

// CreateSession creates a new session.
func (ctx *Context) CreateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	var req request.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Apply default working dir from settings -- overrides request if set.
	// Check both main settings AND account settings (accounts.js saves to /api/settings/accounts)
	workingDir := req.WorkingDir
	settings := ctx.StateStore.GetSettings()
	defaultDir, _ := settings["defaultWorkingDir"].(string)

	// Also check account settings (saved separately by accounts.js)
	if defaultDir == "" {
		if accounts, ok := settings["accounts"]; ok {
			// accounts could be an array or a map -- check the last saved account settings
			if acctList, ok := accounts.([]interface{}); ok {
				for _, acct := range acctList {
					if acctMap, ok := acct.(map[string]interface{}); ok {
						if dir, ok := acctMap["defaultWorkingDir"].(string); ok && dir != "" {
							defaultDir = dir
						}
					}
				}
			}
		}
		// Also check if it was saved as a top-level key from POST /api/settings/accounts
		if defaultDir == "" {
			if dir, ok := settings["accountDefaultWorkingDir"].(string); ok && dir != "" {
				defaultDir = dir
			}
		}
	}

	slog.Info("CreateSession", "reqWorkingDir", req.WorkingDir, "defaultWorkingDir", defaultDir)
	if defaultDir != "" {
		if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
			workingDir = defaultDir
			slog.Info("CreateSession: using defaultWorkingDir", "dir", defaultDir)
		} else {
			slog.Warn("CreateSession: defaultWorkingDir not found on disk", "dir", defaultDir, "err", err)
		}
	}
	if workingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required")
		return
	}

	mode := pty.ModeShell
	if req.Mode == "claude" {
		mode = pty.ModeClaude
	}

	// Build provider env vars from account settings
	providerEnv := ctx.getProviderEnvVars()

	// Use Background context, NOT r.Context() -- sessions outlive the HTTP request.
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:  workingDir,
			Mode:        mode,
			Name:        req.Name,
			ProviderEnv: providerEnv,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))

	// Notify Telegram
	if ctx.TelegramHandler != nil {
		ctx.TelegramHandler.NotifySessionStart(session.ID, session.Name(), session.WorkingDir)
	}

	// Persist session state
	ctx.StateStore.SaveSession(&state.SessionState{
		ID:         session.ID,
		Name:       session.Name(),
		WorkingDir: session.WorkingDir,
		Mode:       string(session.Mode),
		CreatedAt:  session.CreatedAt.Format(time.RFC3339),
		Status:     string(session.Status()),
	})

	// Create Ralph tracker for this session
	tracker := ralph.NewTracker()
	ctx.RalphTrackersMu.Lock()
	ctx.RalphTrackers[session.ID] = tracker
	ctx.RalphTrackersMu.Unlock()

	// Create run summary tracker for this session
	runTracker := runsummary.New(session.ID)
	ctx.RunSummariesMu.Lock()
	ctx.RunSummaries[session.ID] = runTracker
	ctx.RunSummariesMu.Unlock()

	// Initialize respawn controller for this session with a session writer
	rc := ctx.GetOrCreateRespawnController(session.ID)
	rc.SetSessionWriter(&respawnSessionWriter{session: session})

	// Forward terminal events to SSE for real-time streaming
	go forwardSessionEvents(session, ctx)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"session": sessionToJSON(session),
	})
}

// DeleteSession stops and removes a session.
func (ctx *Context) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := ctx.SessionManager.StopSession(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			request.WriteError(w, http.StatusNotFound, err.Error())
		} else {
			request.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	ctx.SSEBroker.Broadcast("session:deleted", map[string]string{"id": id})
	ctx.StateStore.RemoveSession(id)
	ctx.cleanupSessionResources(id)

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      id,
	})
}

// SessionInput sends input to a session.
func (ctx *Context) SessionInput(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	var req request.SessionInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sent := session.WriteViaScreen(req.Input)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": sent,
	})
}

// GetSessionTerminal returns the terminal buffer as JSON.
// Frontend calls res.json() and reads terminalBuffer field.
func (ctx *Context) GetSessionTerminal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"terminalBuffer": session.TerminalBuffer(),
	})
}

// forwardSessionEvents reads from a session's event channel and broadcasts via SSE.
// This enables real-time terminal streaming to the frontend.
// Terminal events are batched (33ms / 1KB) to reduce SSE message volume.
// It also feeds terminal data to the Ralph tracker and records run summary events.
func forwardSessionEvents(session *pty.Session, ctx *Context) {
	sessionID := session.ID
	broker := ctx.SSEBroker
	slog.Info("Starting event forwarding for session", "sessionId", sessionID)

	// Create terminal batcher: 33ms interval, 1KB flush threshold
	termBatcher := sse.NewBatcher(33*time.Millisecond, 1024, func(sid string, data []byte) {
		broker.Broadcast("session:terminal", map[string]interface{}{
			"id":   sid,
			"data": string(data),
		})
	})
	defer termBatcher.Stop()

	// Helper to look up the Ralph tracker for this session.
	getRalphTracker := func() *ralph.Tracker {
		ctx.RalphTrackersMu.RLock()
		t := ctx.RalphTrackers[sessionID]
		ctx.RalphTrackersMu.RUnlock()
		return t
	}

	// Helper to look up the run summary tracker for this session.
	getRunSummary := func() *runsummary.Tracker {
		ctx.RunSummariesMu.RLock()
		t := ctx.RunSummaries[sessionID]
		ctx.RunSummariesMu.RUnlock()
		return t
	}

	eventCount := 0
	for event := range session.Events() {
		eventCount++
		switch e := event.(type) {
		case pty.EventTerminal:
			if eventCount <= 3 {
				slog.Info("Terminal data", "sessionId", sessionID, "bytes", len(e.Data), "clients", broker.ClientCount())
			}
			// Feed terminal data to the Ralph tracker
			if rt := getRalphTracker(); rt != nil {
				rt.ProcessOutput(e.Data)
			}
			termBatcher.Add(sessionID, []byte(e.Data))
		case pty.EventOutput:
			broker.Broadcast("session:output", map[string]interface{}{
				"id":   sessionID,
				"data": e.Data,
			})
		case pty.EventIdle:
			broker.Broadcast("session:idle", map[string]interface{}{"id": sessionID})
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID, "status": "idle", "isWorking": false,
			})
			if rs := getRunSummary(); rs != nil {
				rs.RecordIdle()
			}
			// Signal respawn controller about idle detection
			ctx.RespawnMu.RLock()
			rc := ctx.RespawnControllers[sessionID]
			ctx.RespawnMu.RUnlock()
			if rc != nil {
				rc.OnIdlePrompt()
			}
			// Notify office if this is a team agent (full payload)
			if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
				if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
					payload["status"] = "idle"
					broker.Broadcast("office:memberUpdated", payload)
				}
			}
		case pty.EventWorking:
			broker.Broadcast("session:working", map[string]interface{}{"id": sessionID})
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID, "status": "busy", "isWorking": true,
			})
			if rs := getRunSummary(); rs != nil {
				rs.RecordWorking()
			}
			ctx.RespawnMu.RLock()
			rc := ctx.RespawnControllers[sessionID]
			ctx.RespawnMu.RUnlock()
			if rc != nil {
				rc.OnWorkingDetected("terminal working indicator")
			}
			// Notify office if this is a team agent (full payload)
			if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
				if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
					payload["status"] = "working"
					broker.Broadcast("office:memberUpdated", payload)
				}
			}
		case pty.EventExit:
			broker.Broadcast("session:stopped", map[string]interface{}{
				"id":       sessionID,
				"exitCode": e.ExitCode,
			})
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID, "status": "stopped", "isWorking": false,
			})
			broker.Broadcast("session:exit", map[string]interface{}{
				"id":       sessionID,
				"exitCode": e.ExitCode,
			})
			if rs := getRunSummary(); rs != nil {
				rs.RecordWithSeverity(runsummary.EventExit, runsummary.SeverityInfo, map[string]interface{}{"exitCode": e.ExitCode})
			}
			// Notify Telegram of session completion
			if ctx.TelegramHandler != nil {
				duration := time.Since(session.CreatedAt)
				if e.ExitCode == 0 {
					ctx.TelegramHandler.NotifySessionComplete(sessionID, session.Name(), duration)
				} else {
					ctx.TelegramHandler.NotifySessionError(sessionID, session.Name(),
						fmt.Sprintf("exited with code %d", e.ExitCode))
				}
			}
			// Detect office agent death and clean up
			if ctx.OfficeManager.GetAgent(sessionID) != nil {
				ctx.OfficeManager.HandleAgentDeath(sessionID)
			}
			ctx.cleanupSessionResources(sessionID)
		case pty.EventTokens:
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID,
				"tokens": map[string]int64{
					"input": e.Input, "output": e.Output, "total": e.Total,
				},
			})
			// Feed run summary tracker with token data for milestone tracking
			if rs := getRunSummary(); rs != nil {
				rs.RecordTokens(e.Total)
			}
		}
	}
}

// forwardSDKSessionEvents reads events from an SDK session and broadcasts them via SSE.
// Simpler than the PTY version -- no ANSI parsing, no terminal batching.
func forwardSDKSessionEvents(session *pty.SDKSession, ctx *Context) {
	sessionID := session.ID
	broker := ctx.SSEBroker
	slog.Info("Starting SDK event forwarding for session", "sessionId", sessionID)

	for event := range session.Events() {
		switch e := event.(type) {
		case pty.EventTerminal:
			broker.Broadcast("session:terminal", map[string]interface{}{
				"id":   sessionID,
				"data": e.Data,
			})
		case pty.EventOutput:
			broker.Broadcast("session:output", map[string]interface{}{
				"id":   sessionID,
				"data": e.Data,
			})
		case pty.EventIdle:
			broker.Broadcast("session:idle", map[string]interface{}{"id": sessionID})
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID, "status": "idle", "isWorking": false,
			})
			if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
				agent.SetSDKWorking(false)
				if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
					broker.Broadcast("office:memberUpdated", payload)
				}
			}
		case pty.EventWorking:
			broker.Broadcast("session:working", map[string]interface{}{"id": sessionID})
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID, "status": "busy", "isWorking": true,
			})
			if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
				agent.SetSDKWorking(true)
				if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
					broker.Broadcast("office:memberUpdated", payload)
				}
			}
		case pty.EventTokens:
			broker.Broadcast("session:updated", map[string]interface{}{
				"id": sessionID,
				"tokens": map[string]int64{
					"input": e.Input, "output": e.Output, "total": e.Total,
				},
			})
		case pty.EventExit:
			broker.Broadcast("session:stopped", map[string]interface{}{"id": sessionID, "exitCode": e.ExitCode})
			broker.Broadcast("session:exit", map[string]interface{}{"id": sessionID, "exitCode": e.ExitCode})
			broker.Broadcast("session:updated", map[string]interface{}{"id": sessionID, "status": "stopped", "isWorking": false})
			if ctx.OfficeManager.GetAgent(sessionID) != nil {
				ctx.OfficeManager.HandleAgentDeath(sessionID)
			}
			ctx.cleanupSessionResources(sessionID)
		}
	}

	// Channel closed -- final cleanup if EventExit wasn't received
	slog.Info("SDK session event forwarding ended", "sessionId", sessionID)
}
