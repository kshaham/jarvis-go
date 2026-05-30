package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/request"
	"github.com/kshaham/jarvis/internal/web/sse"
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
	// Include Codex sessions
	for _, cx := range ctx.SessionManager.GetAllCodexSessions() {
		result = append(result, sessionJSON{
			ID:         cx.ID,
			Name:       cx.GetName(),
			Status:     string(cx.GetStatus()),
			Mode:       string(cx.Mode),
			WorkingDir: cx.WorkingDir,
			PID:        0,
			CreatedAt:  cx.CreatedAt.Format(time.RFC3339),
			IsWorking:  cx.IsWorking(),
		})
	}
	request.WriteJSON(w, http.StatusOK, result)
}

// QuerySessions returns sessions with pagination, filtering, and search.
// GET /api/sessions/query?limit=N&offset=N&status=busy|idle|stopped&search=X&sort=createdAt|name&order=asc|desc
// Response: { sessions: [...], total, hasMore }.
// Unlike ListSessions (bare array), this endpoint is designed for UIs that
// need to paginate through many sessions without loading them all at once.
func (ctx *Context) QuerySessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	statusFilter := q.Get("status")
	search := strings.ToLower(strings.TrimSpace(q.Get("search")))
	sortBy := q.Get("sort")
	if sortBy == "" {
		sortBy = "createdAt"
	}
	order := q.Get("order")
	if order == "" {
		order = "desc"
	}

	// Gather all sessions (PTY + SDK) into one slice.
	all := make([]sessionJSON, 0)
	for _, s := range ctx.SessionManager.GetAllSessions() {
		all = append(all, sessionToJSON(s))
	}
	ctx.SessionManager.ForEachSDKSession(func(id string, sdk *pty.SDKSession) {
		all = append(all, sessionJSON{
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
	for _, cx := range ctx.SessionManager.GetAllCodexSessions() {
		all = append(all, sessionJSON{
			ID:         cx.ID,
			Name:       cx.GetName(),
			Status:     string(cx.GetStatus()),
			Mode:       string(cx.Mode),
			WorkingDir: cx.WorkingDir,
			PID:        0,
			CreatedAt:  cx.CreatedAt.Format(time.RFC3339),
			IsWorking:  cx.IsWorking(),
		})
	}

	// Filter.
	filtered := make([]sessionJSON, 0, len(all))
	for _, s := range all {
		if statusFilter != "" && s.Status != statusFilter {
			continue
		}
		if search != "" {
			hay := strings.ToLower(s.Name + " " + s.WorkingDir)
			if !strings.Contains(hay, search) {
				continue
			}
		}
		filtered = append(filtered, s)
	}

	// Sort.
	lessFunc := func(a, b sessionJSON) bool {
		switch sortBy {
		case "name":
			return a.Name < b.Name
		default: // createdAt
			return a.CreatedAt < b.CreatedAt
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if order == "asc" {
			return lessFunc(filtered[i], filtered[j])
		}
		return lessFunc(filtered[j], filtered[i])
	})

	total := len(filtered)
	if offset >= total {
		filtered = nil
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": filtered,
		"total":    total,
		"hasMore":  offset+len(filtered) < total,
		"offset":   offset,
		"limit":    limit,
	})
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

	// Resolve symlinks and enforce the allowlist. Prevents `/tmp/link-to-/etc`
	// from starting a session at the filesystem root.
	resolvedDir, err := ctx.ResolveWorkingDir(workingDir)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid working directory: "+err.Error())
		return
	}
	workingDir = resolvedDir

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
			WorkingDir:          workingDir,
			Mode:                mode,
			Name:                req.Name,
			ProviderEnv:         providerEnv,
			OmitSkipPermissions: req.OmitSkipPermissions,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start event forwarding BEFORE broadcasting session:created to avoid
	// losing events emitted between broadcast and goroutine start.
	go forwardSessionEvents(session, ctx)

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

	ctx.initSessionTrackers(session.ID)

	// Initialize respawn controller for this session with a session writer
	rc := ctx.GetOrCreateRespawnController(session.ID)
	rc.SetSessionWriter(&respawnSessionWriter{session: session})

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
// Supports both PTY and SDK sessions. Accepts optional ?tail=N query param
// to return only the last N bytes (frontend sends ?tail=262144).
func (ctx *Context) GetSessionTerminal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Parse optional tail parameter
	tailBytes := 0
	if t := r.URL.Query().Get("tail"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			tailBytes = n
		}
	}

	// Try PTY session first, then SDK, then Codex
	var buf string
	if session, ok := ctx.SessionManager.GetSession(id); ok {
		buf = session.TerminalBuffer()
	} else if sdkSession, ok := ctx.SessionManager.GetSDKSession(id); ok {
		buf = sdkSession.GetTerminalBuffer()
	} else if cx, ok := ctx.SessionManager.GetCodexSession(id); ok {
		buf = cx.GetTerminalBuffer()
	} else {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	truncated := false
	if tailBytes > 0 && tailBytes < len(buf) {
		buf = buf[len(buf)-tailBytes:]
		truncated = true
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"terminalBuffer": buf,
		"truncated":      truncated,
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in PTY forwarder", "sessionId", sessionID, "panic", r, "stack", string(debug.Stack()))
				}
			}()
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
				// Record dollar cost + token delta in daily stats so the
				// informational spending widget has current data. R24 · The
				// checkSpendingThresholds enforcement was removed; daily
				// stats are kept purely for the header readout.
				ctx.StateStore.RecordDailyStats(e.Input, e.Output, e.Model)
				// Per-agent accumulation: if this session is attached to an
				// office agent, record the turn so the agent card + insights
				// panel can render cost/token totals without a new endpoint.
				if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
					agent.RecordTurnUsage(e.Input, e.Output, e.Cost)
					if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
						broker.Broadcast("office:memberUpdated", payload)
					}
				}
				// Feed run summary tracker with token data for milestone tracking
				if rs := getRunSummary(); rs != nil {
					rs.RecordTokens(e.Total)
				}
			}
		}()
	}
}

// forwardSDKSessionEvents reads events from an SDK session and broadcasts them via SSE.
// Simpler than the PTY version -- no ANSI parsing, no terminal batching.
func forwardSDKSessionEvents(session *pty.SDKSession, ctx *Context) {
	forwardAgentEvents(session.ID, session.Events(), session.DroppedEvents, ctx)
}

// forwardCodexSessionEvents bridges a Codex (codex exec --json) session's
// events to SSE. Codex emits the same pty.Event* set as the SDK backend, so
// the body is shared via forwardAgentEvents.
func forwardCodexSessionEvents(session *pty.CodexSession, ctx *Context) {
	forwardAgentEvents(session.ID, session.Events(), session.DroppedEvents, ctx)
}

// forwardAgentEvents is the backend-agnostic SSE forwarder body shared by the
// SDK and Codex sessions. dropped reports the session's dropped-event count
// (a method value off the concrete session) so per-agent UI can surface it.
func forwardAgentEvents(sessionID string, events <-chan pty.SessionEvent, dropped func() uint64, ctx *Context) {
	broker := ctx.SSEBroker
	slog.Info("Starting agent event forwarding for session", "sessionId", sessionID)

	sawExit := false
	for event := range events {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in SDK forwarder", "sessionId", sessionID, "panic", r, "stack", string(debug.Stack()))
				}
			}()
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
				// R20 · Feed into the output-repeat loop detector.
				// Advisory-only; fires office:agentPossibleLoop once per
				// streak. Detector self-resets when the streak breaks
				// and is cleared on Idle (turn boundary) below.
				if ctx.OfficeManager != nil {
					ctx.OfficeManager.ObserveAgentOutput(sessionID, e.Data)
					// R35 · Buffer per-turn assistant text so the
					// forwarder can dispatch it at the turn boundary
					// (EventIdle below) for Telegram lead-reply forwarding.
					if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
						agent.AppendTurnText(e.Data)
					}
				}
			case pty.EventIdle:
				broker.Broadcast("session:idle", map[string]interface{}{"id": sessionID})
				broker.Broadcast("session:updated", map[string]interface{}{
					"id": sessionID, "status": "idle", "isWorking": false,
				})
				if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
					agent.SetSDKWorking(false)
					agent.SetEventsDropped(dropped())
					// R35 · Flush the turn-text buffer and dispatch to
					// any registered lead-reply callback (Telegram
					// handler). No-op when the callback is nil or the
					// sessionID has no pending reply waiting.
					if text := agent.FlushTurnText(); text != "" {
						ctx.OfficeManager.DispatchLeadReply(sessionID, text)
					}
					// R20 · Loop detection is scoped to a single turn —
					// legitimate multi-turn work often rebuilds/re-runs
					// the same commands, which would otherwise look like
					// a loop. Reset on turn boundary.
					ctx.OfficeManager.ResetAgentLoopDetector(sessionID)
					// R21 · Same reasoning for the tool-level detector.
					ctx.OfficeManager.ResetAgentToolLoopDetector(sessionID)
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
					agent.SetEventsDropped(dropped())
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
					"cost": e.Cost,
				})
				ctx.StateStore.RecordDailyStats(e.Input, e.Output, e.Model)
				// Per-agent accumulation for office UI (cost bars, sparklines).
				if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
					agent.RecordTurnUsage(e.Input, e.Output, e.Cost)
					agent.SetEventsDropped(dropped())
					if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
						broker.Broadcast("office:memberUpdated", payload)
					}
				}
			case pty.EventToolInvocation:
				// R21 · Feed into the tool-level loop detector. Higher
				// fidelity than the text-level advisory — a matched
				// (tool, resultHash) triplet means the agent re-ran the
				// same tool with the same output 3 times inside one turn.
				if ctx.OfficeManager != nil {
					ctx.OfficeManager.ObserveAgentToolInvocation(sessionID, e.Name, e.ResultHash)
				}
			case pty.EventTurnCapReached:
				// R18 · Bridge the per-session turn cap into an office SSE
				// event so the dashboard agent card can flash + flag the
				// runaway agent before operators notice in the logs.
				agentName := sessionID
				if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil && agent.Config != nil {
					agentName = agent.Config.Name
				}
				broker.Broadcast("office:agentTurnCapReached", map[string]interface{}{
					"sessionId": sessionID,
					"agentName": agentName,
					"turns":     e.Turns,
					"cap":       e.Cap,
					"cost":      e.Cost,
				})
			case pty.EventTurnError:
				// Bridge SDK turn errors (provider 4xx/5xx, rate limits,
				// "Network error" from the Z.AI gateway) into an office
				// SSE event so the dashboard can flag the agent instead
				// of letting them silently fall idle after a failed run.
				agentName := sessionID
				if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil && agent.Config != nil {
					agentName = agent.Config.Name
				}
				broker.Broadcast("office:agentTurnError", map[string]interface{}{
					"sessionId":  sessionID,
					"agentName":  agentName,
					"subtype":    e.Subtype,
					"stopReason": e.StopReason,
					"result":     e.Result,
					"numTurns":   e.NumTurns,
				})
			case pty.EventExit:
				sawExit = true
				broker.Broadcast("session:stopped", map[string]interface{}{"id": sessionID, "exitCode": e.ExitCode})
				broker.Broadcast("session:exit", map[string]interface{}{"id": sessionID, "exitCode": e.ExitCode})
				broker.Broadcast("session:updated", map[string]interface{}{"id": sessionID, "status": "stopped", "isWorking": false})
				if ctx.OfficeManager.GetAgent(sessionID) != nil {
					ctx.OfficeManager.HandleAgentDeath(sessionID)
				}
				ctx.cleanupSessionResources(sessionID)
			}
		}()
	}

	// The events channel closed -> the session's worker goroutine exited, so the
	// session is dead. We do NOT run HandleAgentDeath here on a bare close: a
	// close WITHOUT an EventExit means the session was stopped DELIBERATELY by a
	// caller (operator restart, kill, team shutdown) which owns its own cleanup.
	// Running death cleanup here was a regression — it spuriously reassigned a
	// restarting agent's tasks and could race ReassignSessionID. Real deaths
	// (subprocess crash/normal exit, and the turn-cap path) all emit EventExit,
	// which is handled in the loop above.
	//
	// Always drop the now-dead session from the manager maps so it stops counting
	// against maxSessions and IsSessionAlive returns false (the session stops
	// itself, not via the manager). Idempotent; whichever map it isn't in no-ops.
	if ctx.SessionManager != nil {
		_ = ctx.SessionManager.StopSDKSession(sessionID)
		_ = ctx.SessionManager.StopCodexSession(sessionID)
	}
	slog.Info("agent session event forwarding ended", "sessionId", sessionID, "sawExit", sawExit)
}
