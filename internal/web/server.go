package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/claudehooks"
	"github.com/kshaham/jarvis/internal/deliverables"
	"github.com/kshaham/jarvis/internal/integrations"
	"github.com/kshaham/jarvis/internal/metrics"
	"github.com/kshaham/jarvis/internal/mockups"
	"github.com/kshaham/jarvis/internal/notifications"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/peers"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/quality"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/scheduler"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/telegram"
	"github.com/kshaham/jarvis/internal/templates"
	"github.com/kshaham/jarvis/internal/tts"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/handler"
	mw "github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
	"github.com/kshaham/jarvis/internal/web/static"
)

// officeV2PlaceholderHTML is served by /office-v2 when the React bundle
// hasn't been built yet. Points operators at the build command. P13.5.
const officeV2PlaceholderHTML = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Jarvis · Office (React) — not built</title>
<style>
  :root { color-scheme: dark; }
  body { font-family: -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; background:#06080C; color:#E2E8F0; margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { max-width:560px; background:rgba(255,255,255,0.025); border:1px solid rgba(255,255,255,0.08); border-radius:16px; padding:32px; }
  h1 { margin:0 0 12px; font-size:20px; font-weight:600; }
  p  { margin:8px 0; line-height:1.5; color:#94A3B8; font-size:14px; }
  pre { background:rgba(255,255,255,0.04); border:1px solid rgba(255,255,255,0.08); padding:12px 14px; border-radius:8px; font-family:ui-monospace,SFMono-Regular,monospace; font-size:13px; color:#E2E8F0; overflow:auto; }
  a  { color:#60A5FA; text-decoration:none; } a:hover { text-decoration:underline; }
</style></head><body>
<div class="card">
  <h1>/office-v2 React bundle not built</h1>
  <p>This route serves the new React island for the office dashboard. The build output (<code>internal/web/static/public/react/</code>) isn't on disk yet.</p>
  <p>Build it once:</p>
  <pre>cd internal/web/spa
npm install
npm run build</pre>
  <p>Then reload this page. In the meantime, the classic UI is at <a href="/office-classic">/office-classic</a>.</p>
  <p style="margin-top:16px;font-size:12px;color:#64748B">See <code>internal/web/spa/README.md</code> for the full dev workflow.</p>
</div>
</body></html>`

// Server is the main HTTP server for Jarvis.
type Server struct {
	rootCtx context.Context
	port    int
	isHTTPS bool
	dataDir string
	// srvMu guards the fields assigned inside Start()/setupRouter()
	// (httpServer, httpsServer, schedulerRunner, notificationsRouter) against
	// the concurrent reads in Stop(). main.go runs Start() in a goroutine and
	// Stop() on the main goroutine, so without this they race (and a SIGINT
	// before httpServer is assigned would nil-deref in Stop).
	srvMu               sync.Mutex
	httpServer          *http.Server
	httpsServer         *http.Server
	sessionManager      *pty.SessionManager
	sseBroker           *sse.Broker
	csrfStore           *mw.CSRFStore
	authManager         *auth.Manager
	agentTokenMgr       *auth.AgentTokenManager
	mobileTokenMgr      *auth.MobileTokenManager
	toolPolicy          *claudehooks.ToolPolicyStore
	officeManager       *office.Manager
	stateStore          *state.AppState
	screenManager       *pty.ScreenManager
	subagentWatcher     *subagent.Watcher
	caseManager         *cases.CaseManager
	templateStore       *templates.Store
	teamTemplateStore   *teamtemplates.Store
	schedulerStore      *scheduler.Store
	schedulerRunner     *scheduler.Runner
	notificationsStore  *notifications.Store
	notificationsRouter *notifications.Router
	telegramBot         *telegram.Bot
	telegramHandler     *telegram.Handler
	handlerCtx          *handler.Context
	peers               *handler.Peers
}

// NewServer creates a new web server.
func NewServer(ctx context.Context, port int, isHTTPS bool, sessionManager *pty.SessionManager) *Server {
	broker := sse.NewBroker(100, 10)
	officeMgr := office.NewManager(
		&sessionStoreAdapter{sm: sessionManager},
		broker.Broadcast,
		nil, // use default config
	)

	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".jarvis")
	stateStore := state.NewStore(filepath.Join(dataDir, "state.json"))
	screenMgr := pty.NewScreenManager(dataDir)

	casesDir := filepath.Join(homeDir, "jarvis-cases")
	caseMgr := cases.NewCaseManager(casesDir)

	tmplStore := templates.NewStore(filepath.Join(dataDir, "templates"))
	teamTmplStore := teamtemplates.NewStore(filepath.Join(dataDir, "team-templates"))
	schedulerStore := scheduler.NewStore(filepath.Join(dataDir, "scheduler.json"))
	notificationsStore := notifications.NewStore(filepath.Join(dataDir, "notification-routes.json"))

	// Create Telegram bot and handler.
	tgBot := telegram.New()
	tgSettingsStore, tgStoreErr := telegram.NewSettingsStore(dataDir, stateStore.GetSettings())
	if tgStoreErr != nil {
		slog.Warn("Failed to load telegram settings, using defaults", "error", tgStoreErr)
		tgSettingsStore, _ = telegram.NewSettingsStore(dataDir, nil)
	}
	{
		tgSettings := tgSettingsStore.Get()
		tgBot.Configure(tgSettings.BotToken, tgSettings.ChatID)
	}
	tgBridge := newTelegramBridge(sessionManager)
	tgHandler := telegram.NewHandler(tgBot, tgSettingsStore, tgBridge)

	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	subagentW := subagent.NewWatcher(ctx, projectsDir)

	// P7.2.7 · Install the daemon-lifetime context as the parent for
	// office-managed background goroutines (codegraph init, etc.) so
	// a SIGTERM cancels in-flight subprocesses instead of orphaning
	// them with their own internal timeouts.
	office.SetRootContext(ctx)

	// P11.3.10 · ECC embed visibility. Surfaced at boot so operators
	// chasing a missing agent/command have an immediate answer
	// ("ecc: 4 agents, 3 commands embedded" should match what they
	// expect from docs/ECC.md).
	slog.Info(office.EmbedSummaryForLogging())

	subagentW.Start()

	// Forward subagent events to SSE (panic-safe per-event loop body).
	go func() {
		for event := range subagentW.Events() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("panic in subagent forwarder", "panic", r, "stack", string(debug.Stack()))
					}
				}()
				switch e := event.(type) {
				case subagent.SubagentDiscovered:
					broker.Broadcast("subagent:discovered", e.Info)
				case subagent.SubagentUpdated:
					broker.Broadcast("subagent:updated", e.Info)
				case subagent.SubagentCompleted:
					broker.Broadcast("subagent:completed", e.Info)
				}
			}()
		}
	}()

	// Forward office events to SSE (panic-safe per-event loop body).
	go func() {
		for event := range officeMgr.Events() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("panic in office forwarder", "panic", r, "stack", string(debug.Stack()))
					}
				}()
				switch e := event.(type) {
				case office.MemberJoinedEvent:
					broker.Broadcast("office:memberJoined", map[string]interface{}{
						"configId": e.ConfigID, "sessionId": e.SessionID, "name": e.Name,
					})
				case office.MemberLeftEvent:
					broker.Broadcast("office:memberLeft", map[string]interface{}{
						"configId": e.ConfigID, "sessionId": e.SessionID,
					})
				case office.StandupStartedEvent:
					broker.Broadcast("office:standupStarted", map[string]interface{}{
						"participants": e.Participants,
					})
				case office.StandupEndedEvent:
					broker.Broadcast("office:standupEnded", map[string]interface{}{
						"summary": e.Summary,
					})
				case office.TaskAssignedEvent:
					broker.Broadcast("office:taskAssigned", map[string]interface{}{
						"sessionId": e.SessionID, "taskId": e.TaskID, "title": e.Title,
					})
				}
			}()
		}
	}()

	// serverRef captures the server pointer for the init data closure.
	// It's set after NewServer returns (the closure runs lazily on SSE connect).
	var serverRef **Server = new(*Server)

	// Set SSE init data to include real session state
	broker.SetInitDataFunc(func() interface{} {
		allSessions := sessionManager.GetAllSessions()
		sessionList := make([]map[string]interface{}, 0, len(allSessions))
		for _, sess := range allSessions {
			input, output, total := sess.Tokens()
			sessionList = append(sessionList, map[string]interface{}{
				"id": sess.ID, "name": sess.Name(), "status": string(sess.Status()),
				"mode": string(sess.Mode), "workingDir": sess.WorkingDir,
				"pid": sess.PID(), "createdAt": sess.CreatedAt,
				"isWorking": sess.IsWorking(),
				"tokens":    map[string]int64{"input": input, "output": output, "total": total},
			})
		}
		// Include SDK sessions in the session list
		sessionManager.ForEachSDKSession(func(id string, sdk *pty.SDKSession) {
			sessionList = append(sessionList, map[string]interface{}{
				"id": sdk.ID, "name": sdk.GetName(), "status": string(sdk.GetStatus()),
				"mode": string(sdk.Mode), "workingDir": sdk.WorkingDir,
				"pid": 0, "createdAt": sdk.CreatedAt,
				"isWorking": sdk.IsWorking(),
				"tokens":    map[string]int64{"input": 0, "output": 0, "total": 0},
			})
		})

		// Collect respawn status from handler context (nil-safe)
		respawnStatus := make(map[string]interface{})
		if srv := *serverRef; srv != nil && srv.handlerCtx != nil {
			srv.handlerCtx.RespawnMu.RLock()
			for sid, rc := range srv.handlerCtx.RespawnControllers {
				respawnStatus[sid] = rc.GetStatus()
			}
			srv.handlerCtx.RespawnMu.RUnlock()
		}

		// R26 security review: the SSE init payload is received by every
		// authenticated client AND by spawned agent sessions (via
		// AuthWithAgentToken). Feeding them the raw settings map leaked
		// claudeApiKey / zaiApiKey / telegram.token. Mask via the same
		// helper GET /api/settings uses.
		return map[string]interface{}{
			"type":          "init",
			"sessions":      sessionList,
			"scheduledRuns": []interface{}{},
			"respawnStatus": respawnStatus,
			"version":       "0.2.0-go",
			"settings":      handler.MaskSensitiveSettings(stateStore.GetSettings()),
			"config":        stateStore.GetConfig(),
			"globalStats": map[string]interface{}{
				"sessionCount": len(sessionList),
			},
		}
	})

	authMgr, authErr := auth.NewManager(dataDir, isHTTPS)
	if authErr != nil {
		// Fatal: the session-cookie signing key can't be initialised.
		// Proceeding would leave the Manager with a nil HMAC key, which
		// hmac.New accepts silently -- every signed cookie would be
		// trivially forgeable. Fail fast at startup instead.
		slog.Error("Auth manager initialisation failed; refusing to start", "error", authErr)
		os.Exit(1)
	}
	agentTokenMgr := auth.NewAgentTokenManager(dataDir)
	mobileTokenMgr := auth.NewMobileTokenManager(dataDir)

	// Configure trusted-proxy CIDRs for X-Forwarded-For handling. Reads the
	// `trustedProxyCIDRs` setting (string or []string); default empty means
	// the X-Forwarded-For header is ignored on every request -- safe when
	// no reverse proxy is deployed.
	{
		settings := stateStore.GetSettings()
		var cidrs []string
		if v, ok := settings["trustedProxyCIDRs"].([]interface{}); ok {
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					cidrs = append(cidrs, s)
				}
			}
		} else if s, ok := settings["trustedProxyCIDRs"].(string); ok && s != "" {
			for _, piece := range strings.Split(s, ",") {
				if piece = strings.TrimSpace(piece); piece != "" {
					cidrs = append(cidrs, piece)
				}
			}
		}
		auth.SetTrustedProxyCIDRs(cidrs)
	}

	srv := &Server{
		rootCtx:            ctx,
		port:               port,
		isHTTPS:            isHTTPS,
		dataDir:            dataDir,
		sessionManager:     sessionManager,
		sseBroker:          broker,
		csrfStore:          mw.NewCSRFStore(ctx),
		authManager:        authMgr,
		agentTokenMgr:      agentTokenMgr,
		mobileTokenMgr:     mobileTokenMgr,
		officeManager:      officeMgr,
		stateStore:         stateStore,
		screenManager:      screenMgr,
		subagentWatcher:    subagentW,
		caseManager:        caseMgr,
		templateStore:      tmplStore,
		teamTemplateStore:  teamTmplStore,
		schedulerStore:     schedulerStore,
		notificationsStore: notificationsStore,
		telegramBot:        tgBot,
		telegramHandler:    tgHandler,
	}
	// Set serverRef so the init data closure can access handlerCtx
	*serverRef = srv
	return srv
}

// sessionStoreAdapter adapts pty.SessionManager to the office.SessionStore interface.
type sessionStoreAdapter struct {
	sm *pty.SessionManager
}

func (a *sessionStoreAdapter) WriteViaScreen(sessionID, input string) bool {
	// Try PTY session first, then SDK, then Codex
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.WriteViaScreen(input)
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.WriteViaScreen(input)
	}
	if cx, ok := a.sm.GetCodexSession(sessionID); ok {
		return cx.WriteViaScreen(input)
	}
	return false
}

func (a *sessionStoreAdapter) WriteInkPrompt(sessionID, prompt string) bool {
	// Try PTY session first, then SDK, then Codex
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.WriteInkPrompt(prompt)
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.WriteInkPrompt(prompt)
	}
	if cx, ok := a.sm.GetCodexSession(sessionID); ok {
		return cx.WriteInkPrompt(prompt)
	}
	return false
}

func (a *sessionStoreAdapter) GetSessionIDs() []string {
	return a.sm.GetAllSessionIDs() // atomic snapshot of both PTY and SDK sessions
}

func (a *sessionStoreAdapter) IsSessionAlive(sessionID string) bool {
	if _, ok := a.sm.GetSession(sessionID); ok {
		return true
	}
	if _, ok := a.sm.GetSDKSession(sessionID); ok {
		return true
	}
	// Codex-harness agents live in the codex session map. Without this the
	// recovery monitor (IsSessionAlive == false) reaps every Codex agent a
	// few seconds after spawn via HandleAgentDeath.
	if _, ok := a.sm.GetCodexSession(sessionID); ok {
		return true
	}
	return false
}

func (a *sessionStoreAdapter) IsSessionWorking(sessionID string) bool {
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.IsWorking()
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.IsWorking()
	}
	if cx, ok := a.sm.GetCodexSession(sessionID); ok {
		return cx.IsWorking()
	}
	return false
}

// StopSession terminates a session by ID, preferring SDK over PTY
// since all office agents are SDK. Mirrors the R18 OfficeAgentKill
// path so the office manager can trigger the same cleanup that an
// operator-initiated kill does.
func (a *sessionStoreAdapter) StopSession(sessionID string) error {
	if _, ok := a.sm.GetSDKSession(sessionID); ok {
		return a.sm.StopSDKSession(sessionID)
	}
	if _, ok := a.sm.GetCodexSession(sessionID); ok {
		return a.sm.StopCodexSession(sessionID)
	}
	if _, ok := a.sm.GetSession(sessionID); ok {
		return a.sm.StopSession(context.Background(), sessionID)
	}
	return fmt.Errorf("session not found: %s", sessionID)
}

// Start begins listening for HTTP connections.
//
// When --https is enabled, Jarvis listens on BOTH:
//   - the configured HTTP port (e.g. 3555) -- for internal services
//     (cm CLI, jarvis-mcp) that call http://localhost:PORT
//   - port+1 HTTPS (e.g. 3556) -- for browsers needing a secure context
//     to enable voice (getUserMedia requires https://)
//
// This keeps backwards compatibility while enabling browser voice.
func (s *Server) Start() error {
	// Build the router (which assigns schedulerRunner, notificationsRouter,
	// handlerCtx, peers) and the http.Server while holding srvMu so a
	// concurrent Stop() either waits for construction to finish then snapshots
	// the real values, or runs entirely before and sees nils -- never a torn
	// read or a nil-deref.
	s.srvMu.Lock()
	router := s.setupRouter()
	s.httpServer = &http.Server{
		Addr:           fmt.Sprintf(":%d", s.port),
		Handler:        router,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0, // SSE needs unlimited write timeout
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 2 << 20, // 2 MB; defends against header-bomb DoS
	}
	httpSrv := s.httpServer
	schedRunner := s.schedulerRunner
	notifRouter := s.notificationsRouter
	s.srvMu.Unlock()

	// Start Telegram handler (polling/webhook)
	s.telegramHandler.Start()

	// Start the scheduler runner: loads persisted jobs, schedules them, and
	// fires any that were due while the server was offline.
	if schedRunner != nil {
		schedRunner.Start(context.Background())
	}

	// Start the notifications router worker pool. Pre-hook registration in
	// setupRouter has already wired it into the SSE broker, so once workers
	// start they immediately begin draining any queued work.
	if notifRouter != nil {
		notifRouter.Start(context.Background())
	}

	slog.Info("Starting Jarvis Go server", "port", s.port, "https", s.isHTTPS)

	// Optionally start a second HTTPS listener alongside HTTP.
	if s.isHTTPS {
		tlsCfg, err := ensureSelfSignedCert(s.dataDir)
		if err != nil {
			return fmt.Errorf("load self-signed cert: %w", err)
		}
		httpsPort := s.port + 1
		tlsServer := &http.Server{
			Addr:           fmt.Sprintf(":%d", httpsPort),
			Handler:        router,
			TLSConfig:      tlsCfg,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   0,
			IdleTimeout:    120 * time.Second,
			MaxHeaderBytes: 2 << 20,
		}
		// Store on the struct (under srvMu) so Stop() can drain it too;
		// previously it was a local and the HTTPS listener was never gracefully
		// shut down (in-flight TLS/voice requests killed at process exit).
		s.srvMu.Lock()
		s.httpsServer = tlsServer
		s.srvMu.Unlock()
		slog.Info("Starting HTTPS listener", "port", httpsPort, "tls_dir", filepath.Join(s.dataDir, "tls"))
		go func() {
			if err := tlsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTPS listener failed", "error", err)
			}
		}()
	}

	return httpSrv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	slog.Info("Shutting down server...")

	// Snapshot the Start()-assigned fields under srvMu (paired with the write
	// side in Start) so this read can't race construction or nil-deref.
	s.srvMu.Lock()
	httpSrv := s.httpServer
	httpsSrv := s.httpsServer
	schedRunner := s.schedulerRunner
	notifRouter := s.notificationsRouter
	hctx := s.handlerCtx
	s.srvMu.Unlock()

	// Close SSE clients FIRST so their in-flight /events handlers return; an
	// http.Server.Shutdown otherwise blocks for the entire ctx budget waiting
	// on never-returning SSE handlers whenever a dashboard tab is open.
	if s.sseBroker != nil {
		s.sseBroker.CloseAllClients()
	}

	if schedRunner != nil {
		schedRunner.Stop()
	}
	if notifRouter != nil {
		notifRouter.Stop()
	}
	s.telegramHandler.Stop()
	// Stop the office's idle-monitoring ticker so the goroutine winds
	// down with the daemon. Without this, the ticker leaks until
	// process exit — usually harmless (the OS reclaims everything)
	// but goroutine leaks show up in pprof and slow down graceful
	// shutdown waits in tests + restart loops. Idempotent: the
	// method early-returns when no monitor is running.
	if s.officeManager != nil {
		s.officeManager.StopIdleMonitoring()
	}
	// AppState.save() debounces writes through a 500ms timer. Without
	// an explicit Flush on shutdown, any setting / token-stats update
	// from the last half-second is lost on a clean SIGTERM. Flush is
	// safe to call multiple times and no-op when the debounce is idle.
	if s.stateStore != nil {
		s.stateStore.Flush()
	}

	// Drain both listeners (HTTP + the optional HTTPS one) so in-flight
	// requests on each finish symmetrically.
	if httpsSrv != nil {
		_ = httpsSrv.Shutdown(ctx)
	}
	var shutdownErr error
	if httpSrv != nil {
		shutdownErr = httpSrv.Shutdown(ctx)
	}

	// Drain in-flight standup-audio jobs after the listeners stop accepting new
	// ones. main.go cancels the root context before Stop, so each job's OpenAI
	// calls abort and the goroutines finish promptly; this avoids leaking a job
	// goroutine mid-write and orphaning a .mp3.tmp. Bound the wait by ctx so a
	// stuck job can never push shutdown past the budget (defense in depth in
	// case the root context wasn't cancelled on some path).
	if hctx != nil && hctx.AudioJobs != nil {
		done := make(chan struct{})
		go func() { hctx.AudioJobs.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			slog.Warn("server: audio jobs did not drain within shutdown budget")
		}
	}

	// Stop the subagent watcher AFTER the listeners are drained. Stopping it
	// closes its events channel; the /claude/hooks handlers send on that channel
	// (OnHookStart/OnHookStop), so closing it while a hook request is still
	// in-flight would panic on send-on-closed. Shutdown has now returned, so no
	// handler is running. Closing the channel also lets the NewServer forwarder
	// goroutine (`for range Events()`) return instead of leaking.
	if s.subagentWatcher != nil {
		s.subagentWatcher.Stop()
	}
	return shutdownErr
}

// SSEBroker returns the SSE broker for external event broadcasting.
// daemonHooksURL returns the base URL spawned `claude` processes use
// to reach this daemon. Always 127.0.0.1 — Claude Code runs locally
// in the same host as Jarvis, and the loopback address bypasses any
// network ACLs the operator might have on the bind interface.
// Returns empty string if no port is configured (server hasn't
// finished init yet); office spawns gracefully fall back to legacy
// ModeClaude in that case.
func (s *Server) daemonHooksURL() string {
	if s.port == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

func (s *Server) SSEBroker() *sse.Broker {
	return s.sseBroker
}

// registerAgentMutationRoutes registers the mutating agent endpoints shared by
// the /office/agent mount (where the cm CLI + jarvis-mcp clients POST) and the
// /claude/hooks mount (hooks-mode clients). Declared once -- and at package
// scope so it's unit-testable -- so the two mounts cannot drift: previously
// these lived ONLY under /claude/hooks, so every cm / jarvis-mcp call to
// /api/office/agent/task-* (and file-lock, skill-create, memory, handoff, etc.)
// 404'd. Both mounts apply the same agent-token middleware.
func registerAgentMutationRoutes(r chi.Router, h *handler.Context) {
	r.Post("/task-start", h.OfficeAgentTaskStart)
	r.Post("/task-complete", h.OfficeAgentTaskComplete)
	r.Post("/task-ack", h.OfficeAgentTaskAck)
	r.Post("/task-progress", h.OfficeAgentTaskProgress)
	r.Post("/task/self-add", h.OfficeAgentTaskSelfAdd)
	r.Post("/task/triage", h.OfficeAgentTaskTriage)
	r.Post("/task/handoff", h.OfficeAgentHandoffInitiate)
	r.Post("/task/handoff/accept", h.OfficeAgentHandoffAccept)
	r.Post("/task/handoff/reject", h.OfficeAgentHandoffReject)
	r.Post("/task/handoff/cancel", h.OfficeAgentHandoffCancel)
	r.Post("/task/screenshot", h.OfficeAgentTaskScreenshot)
	r.Post("/task/labels", h.OfficeAgentTaskLabels)
	r.Post("/task/criteria", h.OfficeAgentTaskCriteria)
	r.Post("/task/phase", h.OfficeAgentTaskPhase)
	r.Post("/brainstorm/response", h.OfficeAgentBrainstormResponse)
	r.Post("/leader-tasks/response", h.OfficeAgentLeaderTasksResponse)
	r.Post("/reflection/response", h.OfficeAgentReflectionResponse)
	r.Post("/curiosity/discovery", h.OfficeAgentCuriosityDiscovery)
	r.Post("/file-lock", h.OfficeAgentFileLock)
	r.Post("/skill-create", h.OfficeAgentSkillCreate)
	r.Delete("/skills/{name}", h.OfficeAgentDeleteSkill)
	r.Post("/memory", h.OfficeAgentPostMemory)
	r.Post("/memory/append", h.OfficeAgentPostMemoryAppend)
	r.Post("/memory/section", h.OfficeAgentPostMemorySection)
	r.Post("/telegram-config", h.OfficeAgentPostTelegramConfig)
	r.Post("/dev-instance", h.OfficeAgentPostDevInstance)
	r.Delete("/dev-instance", h.OfficeAgentDeleteDevInstance)
	r.Post("/dev-credentials", h.OfficeAgentPostDevCredentials)
	r.Delete("/dev-credentials", h.OfficeAgentDeleteDevCredentials)
}

func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	// Global middleware. RequestID runs first so every subsequent log line
	// and the response header carry a correlation ID.
	r.Use(mw.Recovery)
	r.Use(mw.RequestID)
	r.Use(mw.RequestLogger)
	r.Use(mw.SecurityHeaders(s.isHTTPS))
	r.Use(mw.BodyLimit(mw.DefaultMaxBodyBytes))

	intMon := integrations.NewMonitor()

	// Wire up the Claude Code hooks dispatcher BEFORE building the
	// handler context (we don't strictly need to, but keeping it
	// near the OfficeManager init makes the dependency graph easy
	// to follow). The OfficeManager satisfies AgentRegistry via the
	// adapter file in internal/office/. Tool-loop observation hooks
	// into the office's existing per-agent tool-loop detector path.
	claudeHooksDispatcher := claudehooks.NewDispatcher(
		s.officeManager,
		// Adapt the (bool-returning) method to the (void) callback the
		// dispatcher expects. Loss of the bool is intentional -- the
		// SSE/dashboard signal is fired inside the method itself.
		func(sessionID, name, resultHash string) {
			_ = s.officeManager.ObserveAgentToolInvocation(sessionID, name, resultHash)
		},
	)
	// Tool-policy store backs the PreToolUse deny filter. Loaded from
	// ~/.jarvis/tool-policy.json (defaults applied when missing).
	// Operators mutate it via /api/office/tool-policy.
	s.toolPolicy = claudehooks.NewToolPolicyStore(filepath.Join(s.dataDir, "tool-policy.json"))
	claudeHooksDispatcher.SetToolPolicy(s.toolPolicy)
	// R3.2 · Subagent hooks drive the watcher so the dashboard sees
	// lifecycle changes immediately instead of waiting for the
	// 10-second filesystem-scan tick. Filesystem polling stays as
	// the fallback for SDK-mode sessions without hooks.
	if s.subagentWatcher != nil {
		claudeHooksDispatcher.SetSubagentObservers(
			s.subagentWatcher.OnHookStart,
			s.subagentWatcher.OnHookStop,
		)
	}
	// Bake the daemon's bind URL into the OfficeManager so hooks-mode
	// spawns know where to point each agent's settings.local.json.
	// http://127.0.0.1:<port> is reachable from any process spawned on
	// this host (PTY / shell / SDK alike).
	s.officeManager.SetDaemonURL(s.daemonHooksURL())

	// Handler context with shared dependencies
	h := &handler.Context{
		SessionManager:          s.sessionManager,
		SSEBroker:               s.sseBroker,
		CSRFStore:               s.csrfStore,
		AuthManager:             s.authManager,
		OfficeManager:           s.officeManager,
		StateStore:              s.stateStore,
		Projects:                projects.New(s.stateStore),
		Quality:                 quality.NewManager(s.rootCtx, projects.New(s.stateStore), s.stateStore, s.sseBroker),
		Mockups:                 mockups.NewManager(s.stateStore, s.sseBroker),
		Deliverables:            deliverables.NewManager(s.stateStore, s.sseBroker),
		ScreenManager:           s.screenManager,
		SubagentWatcher:         s.subagentWatcher,
		CaseManager:             s.caseManager,
		RespawnControllers:      make(map[string]*respawn.Controller),
		TemplateStore:           s.templateStore,
		TeamTemplateStore:       s.teamTemplateStore,
		AgentTokenMgr:           s.agentTokenMgr,
		MobileTokenMgr:          s.mobileTokenMgr,
		ToolPolicy:              s.toolPolicy,
		TelegramBot:             s.telegramBot,
		TelegramHandler:         s.telegramHandler,
		IntegrationMonitor:      intMon,
		ScheduledTasks:          make(map[string]interface{}),
		SessionColors:           make(map[string]string),
		RalphTrackers:           make(map[string]*ralph.Tracker),
		RalphConsumerStop:       make(map[string]chan struct{}),
		RunSummaries:            make(map[string]*runsummary.Tracker),
		TailFileStreams:         make(map[string]func()),
		ActivePlanOrchestrators: make(map[string]string),
	}
	// R24 · Spending enforcement was removed (subscription plans don't
	// benefit from dollar caps). Daily stats are still recorded for
	// the informational cost widget, but no threshold tracker init
	// is needed.

	// Build the scheduler runner with an executor closure that has access
	// to the fully-initialized handler context (spawn flow, spending checks,
	// workingDir resolver).
	s.schedulerRunner = scheduler.NewRunner(s.schedulerStore, handler.BuildSchedulerExecutor(h))
	h.Scheduler = s.schedulerStore
	h.SchedulerRunner = s.schedulerRunner

	// Notifications: build the router with the existing telegram handler as
	// its Telegram sender (via a thin adapter that matches our interface
	// without leaking the telegram package's InlineKeyboardMarkup type),
	// then hook it into the SSE broker so every broadcast is evaluated
	// against persisted routes.
	s.notificationsRouter = notifications.NewRouter(s.notificationsStore, &telegramNotifyAdapter{s.telegramHandler})
	s.sseBroker.AddPostBroadcastHook(func(msg sse.EventMessage) {
		s.notificationsRouter.OnEvent(msg.Event, msg.Data)
	})
	h.Notifications = s.notificationsStore
	h.NotificationsRouter = s.notificationsRouter

	// Standup audio job manager: resolves the OpenAI key from settings on
	// each job start so key changes take effect without a restart, and
	// delegates voice selection to the office manager's per-agent config.
	voiceFor := func(sid string, i int) string {
		if a := s.officeManager.GetAgent(sid); a != nil {
			if v := a.GetVoice(); tts.IsValidVoice(v) {
				return v
			}
		}
		return tts.DefaultVoiceForIndex(i)
	}
	newSynth := func() (handler.Synthesizer, error) {
		settings := s.stateStore.GetSettings()
		key, _ := settings["openaiApiKey"].(string)
		if key == "" {
			key = os.Getenv("OPENAI_API_KEY")
		}
		if key == "" {
			return nil, tts.ErrNoAPIKey
		}
		baseURL, _ := settings["openaiBaseUrl"].(string)
		return tts.NewClient(key, baseURL), nil
	}
	h.AudioJobs = handler.NewAudioJobManager(s.rootCtx, s.sseBroker.Broadcast, newSynth, voiceFor)

	s.handlerCtx = h

	// Inter-team relay infrastructure. Identity is generated on first
	// use (~/.jarvis/identity.json), registry lives at peers.json,
	// relay messages persist under ~/.jarvis/relay/. The Service binds
	// the office manager as the live-team message injector so inbound
	// relay messages reach running agents next turn.
	idStore := peers.NewIdentityStore(peers.DefaultIdentityPath())
	peerReg, peersErr := peers.NewRegistry(peers.DefaultRegistryPath())
	if peersErr != nil {
		slog.Warn("peers: registry load failed, continuing with empty in-memory registry", "err", peersErr)
		peerReg, _ = peers.NewRegistry("")
	}
	relayStore := peers.NewStore(peers.DefaultRelayDir())
	relaySvc := &peers.Service{
		Identity:  idStore,
		Registry:  peerReg,
		Store:     relayStore,
		Client:    peers.NewClient(),
		Injector:  s.officeManager,
		Broadcast: s.sseBroker.Broadcast,
	}
	s.peers = &handler.Peers{
		Identity: idStore,
		Registry: peerReg,
		Relay:    relaySvc,
	}

	// Wire office <-> telegram integration
	s.telegramHandler.SetOfficeBridge(s.officeManager)
	{
		tgSettings := s.telegramHandler.Settings().Get()
		s.officeManager.SetTelegramNotifier(&officeTelegramAdapter{
			handler: s.telegramHandler,
			chatID:  tgSettings.ChatID,
		})
	}

	// Prometheus metrics at the standard /metrics path (outside /api so
	// scrape configs don't need a custom path). R26 security review:
	// previously unauthenticated — deployments without a reverse proxy
	// leaked goroutine / heap / session counts to the public internet.
	// Now behind the same auth middleware as /debug/pprof. Prometheus
	// scrapers can hit it with a session cookie or an agent token;
	// per-scrape auth is a single HMAC check so cost is negligible.
	r.Route("/metrics", func(r chi.Router) {
		r.Use(mw.AuthWithTokens(s.authManager, s.agentTokenMgr, s.mobileTokenMgr))
		r.Handle("/", metrics.Handler())
	})

	// pprof mounted inside an authenticated subtree so heap/goroutine
	// profiles only flow to logged-in operators. Idle endpoints are
	// zero-cost; profiling only spins up when actually collecting.
	r.Route("/debug/pprof", func(r chi.Router) {
		r.Use(mw.Auth(s.authManager))
		r.Get("/", pprof.Index)
		r.Get("/cmdline", pprof.Cmdline)
		r.Get("/profile", pprof.Profile)
		r.Post("/symbol", pprof.Symbol)
		r.Get("/symbol", pprof.Symbol)
		r.Get("/trace", pprof.Trace)
		r.Get("/allocs", pprof.Handler("allocs").ServeHTTP)
		r.Get("/block", pprof.Handler("block").ServeHTTP)
		r.Get("/goroutine", pprof.Handler("goroutine").ServeHTTP)
		r.Get("/heap", pprof.Handler("heap").ServeHTTP)
		r.Get("/mutex", pprof.Handler("mutex").ServeHTTP)
		r.Get("/threadcreate", pprof.Handler("threadcreate").ServeHTTP)
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Auth-exempt routes (no session required)
		r.Group(func(r chi.Router) {
			r.Get("/health", h.Health)
			r.Get("/system/stats", h.GetSystemStats) // public: CPU/memory stats, no sensitive data
			r.Get("/auth/status", h.GetAuthStatus)
			r.Post("/auth/setup", h.AuthSetup)
			r.Post("/auth/login", h.AuthLogin)
			// Mobile-app login (issues a per-device bearer token). Same
			// IP rate limiting as /auth/login. Logout + list + revoke
			// require an authenticated caller and live in the protected
			// group below.
			r.Post("/auth/mobile/login", h.AuthMobileLogin)
			// R26 security review: /auth/logout and /auth/change-password
			// used to live here. Both are mutating, state-changing POSTs
			// that need CSRF protection — moved below to the authenticated
			// CSRF-protected group.
			// Telegram webhook (auth via secret in URL path)
			r.Post("/telegram/webhook/{secret}", s.telegramHandler.WebhookHandler)

			// Public peer identity probe — other jarvis instances hit
			// this during paste-token registration to verify the URL
			// resolves to a real jarvis. Returns peerId + displayName
			// only; never the local pair tokens.
			r.Get("/identity", s.peers.IdentityShow)
			// Inbound relay messages from peer jarvis instances. Auth
			// happens inside the handler via the Bearer pair token —
			// the operator's session cookie isn't applicable here.
			r.Post("/relay/inbox", s.peers.RelayInboxIn)

			// Agent endpoints called by the `cm` CLI and jarvis-mcp inside
			// spawned sessions. Protected by an HMAC-grade process token
			// piped into sessions via JARVIS_AGENT_TOKEN. External callers
			// without the token get 401 even when they know a session ID.
			r.Route("/office/agent", func(r chi.Router) {
				r.Use(mw.AgentTokenAuth(s.agentTokenMgr))
				// Read-only
				r.Get("/team-roster", h.OfficeAgentTeamRoster)
				r.Get("/tasks", h.OfficeAgentGetTasks)
				r.Get("/telegram-config", h.OfficeAgentGetTelegramConfig)
				r.Get("/file-locks", h.OfficeAgentGetFileLocks)
				r.Get("/skills", h.OfficeAgentGetSkills)
				// R2.3 · per-session statusline fed by the worktree's
				// .claude/statusline.sh, rendered at the bottom of the
				// agent's own terminal.
				r.Get("/statusline", h.OfficeAgentStatusLine)
				r.Get("/skills/search", h.OfficeAgentSearchSkills)
				r.Get("/skills/{name}", h.OfficeAgentGetSkillByName)
				r.Get("/memory", h.OfficeAgentGetMemory)
				r.Get("/memory/section", h.OfficeAgentGetMemorySection)
				r.Get("/memory/query", h.OfficeAgentGetMemoryQuery)
				// R31 · lazy-load onboarding topics (protocols, browser, etc).
				r.Get("/onboarding", h.OfficeAgentOnboarding)
				r.Get("/dev-instance", h.OfficeAgentGetDevInstance)
				r.Get("/dev-instance/all", h.OfficeAgentGetDevInstanceAll)
				r.Get("/dev-credentials", h.OfficeAgentGetDevCredentials)
				// R17 · unified team knowledge search.
				r.Get("/knowledge/search", h.OfficeAgentKnowledgeSearch)
				r.Get("/knowledge/tail", h.OfficeAgentKnowledgeTail)
				// Mutating
				r.Post("/standup/response", h.OfficeAgentStandupResponse)
				r.Post("/message", h.OfficeAgentMessage)
				// Q&A primitive (cm ask). Ask blocks for up to the
				// caller's --wait while the manager rotates through
				// the lead and a fallback teammate.
				r.Post("/ask", h.OfficeAgentAsk)
				r.Post("/ask/answer", h.OfficeAgentAskAnswer)
				r.Get("/ask/list", h.OfficeAgentListQuestions)
				// The mutating endpoints cm + jarvis-mcp actually POST to.
				registerAgentMutationRoutes(r, h)
			})

			// Claude Code hooks dispatcher. Spawned `claude` processes
			// in hooks mode POST structured JSON events here per the
			// .claude/settings.local.json injected at spawn time.
			// Same agent-token middleware as /office/agent so a
			// rogue process can't impersonate Claude.
			r.Route("/claude/hooks", func(r chi.Router) {
				r.Use(mw.AgentTokenAuth(s.agentTokenMgr))
				r.Post("/{sessionId}/{event}", claudeHooksDispatcher.HandleEvent)
				// Same mutating endpoints as /office/agent (shared helper so the
				// two mounts can't drift).
				registerAgentMutationRoutes(r, h)
				// Hooks-mount only: agent uploads a generated UI mockup
				// (Mockups page) / a team-authored deliverable (Deliverables page).
				r.Post("/mockup-upload", h.MockupsUpload)
				r.Post("/deliverable-upload", h.DeliverablesUpload)
			})
		})

		// Authenticated routes. Accept either a valid browser session cookie
		// OR a valid agent token -- jarvis-mcp running inside spawned sessions
		// calls /api/status and /api/config from tools, and those calls should
		// succeed as long as the in-session agent token is attached.
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthWithTokens(s.authManager, s.agentTokenMgr, s.mobileTokenMgr))

			// SSE endpoint (long-lived, no CSRF)
			r.Get("/events", s.sseBroker.Subscribe)

			// Mobile-app token management. Logout revokes the caller's
			// own token; list + revoke let operators (web or mobile)
			// see and prune registered devices. Bearer-only paths so
			// they skip CSRF, consistent with the agent-token routes.
			r.Post("/auth/mobile/logout", h.AuthMobileLogout)
			r.Get("/auth/mobile/tokens", h.AuthMobileListTokens)
			r.Delete("/auth/mobile/tokens/{deviceId}", h.AuthMobileRevokeToken)

			// Read-only session routes
			r.Get("/status", h.Status)
			r.Get("/csrf-token", h.CSRFToken)
			r.Get("/sessions", h.ListSessions)
			r.Get("/sessions/{id}", h.GetSession)
			r.Get("/sessions/{id}/terminal", h.GetSessionTerminal)
			r.Get("/sessions/{id}/output", h.GetSessionOutput)
			r.Get("/sessions/{id}/ralph-state", h.GetSessionRalphState)
			r.Get("/sessions/{id}/git-status", h.GetSessionGitStatus)
			r.Get("/sessions/{id}/build-status", h.GetSessionBuildStatus)
			r.Get("/sessions/{id}/test-status", h.GetSessionTestStatus)
			r.Get("/sessions/{id}/hmr-status", h.GetSessionHMRStatus)
			r.Get("/sessions/{id}/integrations", h.GetSessionIntegrations)
			r.Get("/sessions/{id}/run-summary", h.GetSessionRunSummary)
			r.Get("/sessions/{id}/active-tools", h.GetSessionActiveTools)
			r.Get("/sessions/{id}/files", h.GetSessionFiles)
			r.Get("/sessions/{id}/file-content", h.GetSessionFileContent)
			r.Get("/sessions/{id}/respawn", h.GetSessionRespawn)
			r.Get("/sessions/{id}/respawn/config", h.GetSessionRespawnConfig)
			r.Get("/sessions/{id}/ralph-status", h.GetSessionRalphStatus)
			r.Get("/sessions/{id}/context", h.GetSessionContext)
			r.Get("/sessions/{id}/fix-plan", h.GetSessionFixPlan)
			r.Get("/sessions/{id}/plan/history", h.GetSessionPlanHistory)
			r.Get("/sessions/{id}/export", h.ExportSession)
			r.Get("/sessions/{id}/cpu-limit", h.GetSessionCPULimit)
			r.Get("/sessions/{id}/file-raw", h.GetSessionFileRaw)
			r.Get("/sessions/{id}/tail-file", h.GetSessionTailFile)

			// Read-only monitoring routes
			r.Get("/claude/auth-status", h.GetClaudeAuthStatus)
			r.Get("/screens", h.GetScreens)
			r.Get("/subagents", h.GetSubagents)
			r.Get("/subagents/{agentId}", h.GetSubagent)
			r.Get("/subagents/{agentId}/transcript", h.GetSubagentTranscript)
			r.Get("/subagent-window-states", h.GetSubagentWindowStates)
			r.Get("/subagent-parents", h.GetSubagentParents)
			r.Get("/sessions/{id}/subagents", h.GetSessionSubagents)

			// Read-only config routes
			r.Get("/stats", h.GetStats)
			r.Get("/token-stats", h.GetTokenStats)
			r.Get("/config", h.GetConfig)
			r.Get("/settings", h.GetSettings)
			r.Get("/settings/accounts", h.GetSettingsAccounts)
			r.Get("/projects", h.ListProjects)
			r.Get("/tests/results", h.TestsGetResults)
			r.Get("/mockups/results", h.MockupsGetResults)
			r.Get("/mockups/image/{projectId}/{kind}/{filename}", h.MockupsServe)
			r.Get("/deliverables/results", h.DeliverablesGetResults)
			r.Get("/deliverables/doc", h.DeliverablesGetDoc)
			r.Get("/debug/memory", h.GetDebugMemory)
			r.Get("/prompt-processor/config", h.GetPromptProcessor)
			r.Get("/prompt-processor/stats", h.GetPromptProcessorRules)
			r.Get("/respawn/learning/analytics", h.GetRespawnLearning)
			r.Get("/respawn/learning/profile", h.GetRespawnLearningStats)
			r.Get("/respawn/learning/recommendations", h.GetRespawnLearningRecommendations)
			r.Get("/respawn/learning/categories", h.GetRespawnLearningCategories)
			r.Get("/respawn/smart-kickstart/config", h.GetRespawnSmartKickstart)
			r.Get("/respawn/{sessionId}/health", h.GetRespawnSmartKickstartStats)
			r.Get("/settings/telegram", h.GetTelegramStatus)

			// Read-only misc routes
			r.Get("/templates", h.ListTemplates)
			r.Get("/templates/{id}", h.GetTemplate)
			r.Get("/templates/{id}/export", h.ExportTemplate)
			r.Get("/cases", h.ListCases)
			r.Get("/cases/{name}", h.GetCase)
			r.Get("/cases/{name}/fix-plan", h.GetCaseFixPlan)
			r.Get("/cases/{caseName}/ralph-wizard/files", h.GetRalphWizardFiles)
			r.Get("/cases/{caseName}/ralph-wizard/file/*", h.GetRalphWizardFile)
			r.Get("/scheduled", h.ListScheduledTasks)
			r.Get("/scheduled/{id}", h.GetScheduledTask)
			r.Get("/task-gate/{sessionId}", h.GetTaskGate)
			r.Get("/hook-config", h.GetHookConfig)

			// Spending status + daily cap readout (header cost widget polls /status)
			r.Get("/spending/status", h.GetSpendingStatus)

			// Scheduler read-only (cron-style durable jobs)
			r.Get("/scheduler/jobs", h.ListSchedulerJobs)
			r.Get("/scheduler/jobs/{id}", h.GetSchedulerJob)
			r.Get("/scheduler/jobs/{id}/history", h.GetSchedulerJobHistory)

			// Notification routes (SSE event fan-out read views)
			r.Get("/notifications/routes", h.ListNotificationRoutes)
			r.Get("/notifications/routes/{id}", h.GetNotificationRoute)

			// Knowledge-library read views (PageIndex-backed
			// self-improvement loop, see internal/library).
			r.Get("/knowledge/stats", h.GetKnowledgeStats)
			r.Get("/knowledge/promotions", h.GetKnowledgePromotions)
			r.Get("/knowledge/library", h.GetKnowledgeLibrary)

			// Inter-team relay — operator-facing read views.
			r.Get("/identity/me", s.peers.IdentityShowDetailed)
			r.Get("/peers", s.peers.PeersList)
			r.Get("/relay/messages", s.peers.RelayList)

			// Mutating routes (CSRF required)
			r.Group(func(r chi.Router) {
				r.Use(mw.CSRF(s.csrfStore))

				// R26 · Auth lifecycle mutations require CSRF just like
				// any other state-changing POST. A cross-site request
				// without the token cannot forcibly log the operator
				// out or bounce their password.
				r.Post("/auth/logout", h.AuthLogout)
				r.Post("/auth/change-password", h.AuthChangePassword)

				// Session CRUD
				r.Post("/sessions", h.CreateSession)
				r.Delete("/sessions/{id}", h.DeleteSession)
				r.Delete("/sessions", h.DeleteAllSessions)
				r.Put("/sessions/{id}/name", h.RenameSession)
				r.Put("/sessions/{id}/color", h.UpdateSessionColor)
				r.Post("/sessions/{id}/input", h.SessionInput)
				r.Post("/sessions/{id}/run", h.RunSession)
				r.Post("/sessions/{id}/interactive", h.SessionInteractive)
				r.Post("/sessions/{id}/shell", h.SessionShell)
				r.Post("/sessions/{id}/resize", h.SessionResize)

				// Respawn
				r.Post("/sessions/{id}/respawn/start", h.StartSessionRespawn)
				r.Post("/sessions/{id}/respawn/stop", h.StopSessionRespawn)
				r.Put("/sessions/{id}/respawn/config", h.UpdateSessionRespawnConfig)
				r.Post("/sessions/{id}/respawn/enable", h.EnableSessionRespawn)
				r.Post("/sessions/{id}/interactive-respawn", h.SessionInteractiveRespawn)

				// Session toggles
				r.Post("/sessions/{id}/auto-clear", h.SessionAutoClear)
				r.Post("/sessions/{id}/auto-compact", h.SessionAutoCompact)
				r.Post("/sessions/{id}/auto-trim", h.SessionAutoTrim)
				r.Post("/sessions/{id}/image-watcher", h.SessionImageWatcher)
				r.Post("/sessions/{id}/flicker-filter", h.SessionFlickerFilter)
				r.Post("/sessions/{id}/cpu-limit", h.SetSessionCPULimit)

				// Ralph
				r.Post("/sessions/{id}/ralph-config", h.SetSessionRalphConfig)
				r.Post("/sessions/{id}/ralph-circuit-breaker/reset", h.ResetRalphCircuitBreaker)
				r.Post("/sessions/{id}/ralph-prompt/write", h.WriteSessionRalphPrompt)

				// Context + plans
				r.Post("/sessions/{id}/context/trim", h.TrimSessionContext)
				r.Post("/sessions/{id}/fix-plan/import", h.ImportSessionFixPlan)
				r.Post("/sessions/{id}/fix-plan/write", h.WriteSessionFixPlan)
				r.Post("/sessions/{id}/fix-plan/read", h.ReadSessionFixPlan)
				r.Post("/sessions/{id}/plan/checkpoint", h.CheckpointSessionPlan)
				r.Post("/sessions/{id}/plan/rollback/{version}", h.RollbackSessionPlan)
				r.Post("/sessions/{id}/plan/task", h.SessionPlanTask)
				r.Patch("/sessions/{id}/plan/task/{taskId}", h.PatchSessionPlanTask)
				r.Delete("/sessions/{id}/tail-file/{streamId}", h.DeleteSessionTailFile)

				// Import/export
				r.Post("/sessions/import", h.ImportSession)

				// Monitoring mutating
				r.Delete("/screens/{sessionId}", h.DeleteScreen)
				r.Post("/screens/reconcile", h.ReconcileScreens)
				r.Post("/screens/stats/start", h.StartScreenStats)
				r.Post("/screens/stats/stop", h.StopScreenStats)
				r.Delete("/subagents/{agentId}", h.DeleteSubagent)
				r.Delete("/subagents", h.ClearAllSubagents)
				r.Post("/subagents/cleanup", h.CleanupSubagents)
				r.Put("/subagent-window-states", h.UpdateSubagentWindowStates)
				r.Put("/subagent-parents", h.UpdateSubagentParents)

				// Config mutating
				r.Put("/config", h.UpdateConfig)
				r.Put("/settings", h.UpdateSettings)
				r.Post("/settings/accounts", h.CreateSettingsAccount)
				r.Delete("/projects/{id}", h.DeleteProject)
				r.Post("/tests/coverage", h.TestsRunCoverage)
				r.Post("/tests/scan", h.TestsRunScan)
				r.Post("/tests/react", h.TestsRunReact)
				r.Post("/mockups/generate", h.MockupsGenerate)
				r.Post("/deliverables/generate", h.DeliverablesGenerate)
				r.Post("/cleanup-state", h.CleanupState)
				r.Put("/prompt-processor/config", h.UpdatePromptProcessor)
				r.Put("/prompt-processor/enhancer", h.UpdatePromptProcessorRules)
				r.Put("/prompt-processor/context-injector", h.UpdatePromptProcessorContextInjector)
				r.Post("/prompt-processor/reset-stats", h.ResetPromptProcessorStats)
				r.Post("/prompt-processor/preview", h.PreviewPromptProcessor)
				r.Put("/respawn/smart-kickstart/config", h.UpdateRespawnSmartKickstart)
				r.Post("/respawn/smart-kickstart/preview", h.PreviewSmartKickstart)
				r.Post("/respawn/smart-update/preview", h.PreviewSmartUpdate)

				// Templates
				r.Post("/templates", h.CreateTemplate)
				r.Put("/templates/{id}", h.UpdateTemplate)
				r.Delete("/templates/{id}", h.DeleteTemplate)
				r.Post("/templates/{id}/duplicate", h.DuplicateTemplate)
				r.Post("/templates/{id}/render", h.ExportTemplate)
				r.Post("/templates/import", h.ImportTemplate)
				r.Post("/sessions/{sessionId}/run-template/{templateId}", h.ImportTemplate)

				// Telegram
				r.Post("/settings/telegram", h.UpdateTelegramConfig)
				r.Post("/telegram/test", h.SendTelegramTest)
				r.Post("/telegram/send-test", h.ConnectTelegram)
				r.Post("/telegram/disconnect", h.DisconnectTelegram)

				// Cases
				r.Post("/cases", h.CreateCase)
				r.Post("/cases/link", h.LinkCase)
				r.Put("/cases/{name}", h.UpdateCase)
				r.Delete("/cases/{name}", h.DeleteCase)

				// Quick start + run
				r.Post("/quick-start", h.QuickStart)
				r.Post("/run", h.Run)
				r.Post("/generate-plan", h.GeneratePlan)
				r.Post("/generate-plan-detailed", h.GeneratePlanDetailed)
				r.Post("/cancel-plan-generation", h.CancelPlanGeneration)

				// Scheduled
				r.Post("/scheduled", h.CreateScheduledTask)
				r.Put("/scheduled/{id}", h.UpdateScheduledTask)
				r.Delete("/scheduled/{id}", h.DeleteScheduledTask)

				// Code printer
				r.Post("/code-printer/estimate", h.CodePrinterPrint)
				r.Post("/code-printer/save-images", h.CodePrinterPreview)

				// R24 · PUT /spending/cap removed. Cost caps are not
				// a useful signal on subscription plans. The GET
				// /spending/status route remains in the read-only
				// group for the informational header widget.

				// Scheduler mutations.
				r.Post("/scheduler/jobs", h.CreateSchedulerJob)
				r.Put("/scheduler/jobs/{id}", h.UpdateSchedulerJob)
				r.Delete("/scheduler/jobs/{id}", h.DeleteSchedulerJob)
				r.Post("/scheduler/jobs/{id}/run-now", h.RunSchedulerJobNow)

				// Notification route mutations.
				r.Post("/notifications/routes", h.CreateNotificationRoute)
				r.Put("/notifications/routes/{id}", h.UpdateNotificationRoute)
				r.Delete("/notifications/routes/{id}", h.DeleteNotificationRoute)

				// Inter-team relay — operator-driven mutations.
				r.Put("/identity/me", s.peers.IdentityUpdate)
				r.Post("/identity/token", s.peers.IdentityToken)
				r.Post("/peers", s.peers.PeersAdd)
				r.Delete("/peers/{peerID}", s.peers.PeersDelete)
				r.Post("/peers/{peerID}/ping", s.peers.PeersPing)
				r.Post("/relay/send", s.peers.RelaySend)
				r.Post("/relay/messages/{id}/read", s.peers.RelayMarkRead)
				// Mobile push device registration. POSTing this creates one
				// notifications route per default event pattern so the
				// existing dispatcher fans push events out via Expo.
				r.Post("/notifications/devices", h.RegisterPushDevice)
				r.Delete("/notifications/devices/{deviceId}", h.UnregisterPushDevice)
				r.Post("/notifications/routes/{id}/test", h.TestNotificationRoute)

				// Knowledge-library operator override (revoke an
				// auto-promoted skill). 24h cool-down before
				// re-promotion is enforced in library.RunPromotion.
				r.Post("/knowledge/revoke", h.PostKnowledgeRevoke)

				// Session one-shot + clone.
				r.Post("/sessions/one-shot", h.OneShotSession)
				r.Post("/sessions/{id}/clone", h.CloneSession)

				// Bulk session ops.
				r.Post("/sessions/bulk-delete", h.BulkDeleteSessions)
				r.Post("/sessions/bulk-rename", h.BulkRenameSessions)
				r.Post("/sessions/bulk-color", h.BulkUpdateSessionColors)
			})

			// Hook event (exempt from CSRF - called by Claude CLI)
			r.Post("/hook-event", h.HandleHookEvent)

			// Office routes
			r.Route("/office", func(r chi.Router) {
				// Read-only office endpoints (no CSRF)
				r.Get("/status", h.OfficeStatus)
				r.Get("/templates", h.OfficeTemplates)
				r.Get("/snapshots", h.OfficeListSnapshots) // R25 · read-only snapshot list
				r.Get("/health", h.OfficeHealth)
				r.Get("/settings", h.OfficeGetSettings)
				r.Get("/auto-standup", h.OfficeGetAutoStandup)
				r.Get("/whiteboard", h.OfficeGetWhiteboard)
				r.Get("/task-queues", h.OfficeGetTaskQueues)
				r.Get("/task-queues/{sessionId}", h.OfficeGetTaskQueueBySession)
				// R30 · dependency DAG for the Tasks tab graph view.
				r.Get("/task-graph", h.OfficeGetTaskGraph)
				r.Get("/bug-tasks", h.OfficeGetBugTasks)
				r.Get("/file-locks", h.OfficeGetFileLocks)
				r.Get("/messages", h.OfficeGetMessages)
				r.Get("/metrics", h.OfficeGetMetrics)
				r.Get("/experiments", h.OfficeGetExperiments)
				r.Get("/escalations", h.OfficeGetEscalations)
				r.Get("/scores", h.OfficeGetScores)
				r.Get("/audit-log", h.OfficeGetAuditLog)
				r.Get("/meetings", h.OfficeGetMeetings)
				r.Get("/timeline", h.OfficeTimeline)
				r.Get("/learnings", h.OfficeGetLearnings)
				r.Get("/learnings/{configId}", h.OfficeGetLearningsByConfig)
				r.Get("/code-graph", h.OfficeGetCodeGraph)
				r.Get("/wrap-up", h.OfficeGetWrapUp)
				r.Get("/standup", h.OfficeGetStandup)
				r.Get("/standup/history", h.OfficeGetStandupHistory)
				r.Get("/standup/{standupId}", h.OfficeGetStandupByID)
				r.Get("/standup/{standupId}/audio", h.OfficeGetStandupAudio)
				r.Get("/memory/{sessionId}", h.OfficeGetMemberMemory)
				r.Get("/member/{sessionId}/personality", h.OfficeGetMemberPersonality)
				r.Get("/telegram-config", h.OfficeGetTelegramConfig)
				r.Get("/dev-instances", h.OfficeGetDevInstances)
				// R29 · handoff visibility (read-only)
				r.Get("/handoffs", h.OfficeListHandoffs)
				// R33 · wrap-up / post-mortem reports (read-only).
				r.Get("/reports", h.OfficeListReports)
				r.Get("/reports/{filename}", h.OfficeGetReport)
				// R34 · headless-shell sibling container health probe.
				r.Get("/browser/health", h.OfficeBrowserHealth)
				// R36 · task labels + cost-by-feature rollups (read-only).
				r.Get("/labels", h.OfficeGetLabels)
				r.Get("/labels/rollups", h.OfficeGetLabelRollups)
				// R34 · task screenshot attachments (operator-visible
				// evidence for UI-keyword tasks).
				r.Get("/task-screenshots/{filename}", h.OfficeGetTaskScreenshot)
				// Project invariants (non-negotiable rules injected into agent prompts).
				r.Get("/project/invariants", h.OfficeGetProjectInvariants)
				// PreToolUse safety policy (operator-editable denylist).
				r.Get("/tool-policy", h.GetToolPolicy)
				// R3.6 · map a PR number back to its linked team (and
				// the underlying session IDs). 404 when no team is
				// currently working on the PR.
				r.Get("/pr/{number}", h.OfficeLookupPR)
				// R4.4 · `claude logs` / `claude attach` operator helpers.
				// Read-only; stop is under the mutating group below.
				r.Get("/claude-sessions/{sessionId}/logs", h.OfficeClaudeLogs)
				r.Get("/claude-sessions/{sessionId}/attach-command", h.OfficeClaudeAttachCommand)
				// P13.7 · per-agent recent-turn cost series for the
				// sparkline widget on the agent card.
				r.Get("/agent/{sessionId}/usage-series", h.OfficeAgentUsageSeries)
				// P13.8 · team-wide search across messages + tasks
				// (transcripts deferred to follow-up). POST because
				// the query body can be large; behaviour is otherwise
				// idempotent and cacheable.
				r.Post("/search", h.OfficeSearch)

				// Mutating office endpoints (CSRF required)
				r.Group(func(r chi.Router) {
					r.Use(mw.CSRF(s.csrfStore))
					// Team template CRUD
					r.Post("/templates", h.OfficeCreateTeamTemplate)
					r.Put("/templates/{id}", h.OfficeUpdateTeamTemplate)
					r.Delete("/templates/{id}", h.OfficeDeleteTeamTemplate)
					r.Post("/templates/{id}/duplicate", h.OfficeDuplicateTeamTemplate)
					r.Post("/templates/from-team", h.OfficeTemplateFromTeam)
					r.Post("/spawn-team", h.OfficeSpawnTeam)
					// Upload a .zip codebase to seed a new project before
					// spawning (multipart; streamed + extracted server-side).
					r.Post("/upload-codebase", h.OfficeUploadCodebase)
					// One-click "Demo Jarvis" — spawns a tiny, cost-bounded,
					// real demo team into a sandbox project. Reuses the
					// spawn-team guard (409 if a team is already live).
					r.Post("/demo-spawn", h.OfficeDemoSpawn)
					r.Post("/shutdown-team", h.OfficeShutdownTeam)
					r.Put("/settings", h.OfficePutSettings)
					r.Put("/auto-standup", h.OfficePutAutoStandup)
					r.Post("/whiteboard", h.OfficePostWhiteboard)
					r.Post("/bug-tasks", h.OfficePostBugTask)
					r.Put("/bug-tasks/{id}", h.OfficePutBugTask)
					r.Delete("/file-locks/*", h.OfficeDeleteFileLock)
					r.Post("/message", h.OfficePostMessage)
					r.Post("/code-graph/build", h.OfficePostCodeGraphBuild)
					r.Post("/code-graph/impact", h.OfficePostCodeGraphImpact)
					r.Post("/wrap-up", h.OfficePostWrapUp)
					r.Post("/wrap-up/cancel", h.OfficePostWrapUpCancel)
					// R33 · on-demand wrap-up report generation.
					r.Post("/reports/generate", h.OfficeGenerateReport)
					// R18 · surgical agent controls for operators
					r.Post("/agent/kill", h.OfficeAgentKill)
					r.Post("/agent/inject", h.OfficeAgentInjectPrompt)
					// R29 · operator force-resolve stuck handoffs when
					// the receiver is unresponsive.
					r.Post("/handoffs/force-accept", h.OfficeHandoffForceAccept)
					r.Post("/handoffs/force-reject", h.OfficeHandoffForceReject)
					// R25 · lifecycle controls
					r.Post("/agent/workdir", h.OfficeAgentBumpWorkdir)
					r.Post("/agent/restart", h.OfficeAgentRestart)
					r.Post("/pause", h.OfficePauseTeam)
					r.Post("/resume", h.OfficeResumeTeam)
					// R25 · snapshot + restore
					r.Post("/snapshots", h.OfficeCreateSnapshot)
					r.Delete("/snapshots/{name}", h.OfficeDeleteSnapshot)
					r.Post("/snapshots/{name}/restore", h.OfficeRestoreSnapshot)
					r.Post("/standup", h.OfficePostStandup)
					r.Post("/standup/user-message", h.OfficePostStandupUserMessage)
					r.Post("/standup/{standupId}/audio", h.OfficePostStandupAudio)
					r.Put("/member/{sessionId}/personality", h.OfficePutMemberPersonality)
					r.Put("/member/{sessionId}/voice", h.OfficePutMemberVoice)
					r.Post("/brainstorm", h.OfficeBrainstorm)
					r.Put("/telegram-config", h.OfficePutTelegramConfig)
					// Operator-facing task mutations. Same handlers as the
					// agent-token routes under /api/claude/hooks/*, exposed
					// here so the mobile app (bearer auth, session cookie)
					// can mutate tasks too. Body still carries sessionId so
					// one handler serves both callers.
					r.Post("/task/labels", h.OfficeAgentTaskLabels)
					r.Post("/task/criteria", h.OfficeAgentTaskCriteria)
					r.Post("/task/phase", h.OfficeAgentTaskPhase)
					r.Post("/task/self-add", h.OfficeAgentTaskSelfAdd)
					r.Post("/task/triage", h.OfficeAgentTaskTriage)
					r.Post("/task/start", h.OfficeAgentTaskStart)
					r.Post("/task/complete", h.OfficeAgentTaskComplete)
					r.Post("/task/progress", h.OfficeAgentTaskProgress)
					r.Post("/task/ack", h.OfficeAgentTaskAck)
					// Project invariants mutations.
					r.Put("/project/invariants", h.OfficePutProjectInvariants)
					r.Post("/project/invariants", h.OfficePostProjectInvariant)
					r.Delete("/project/invariants/{index}", h.OfficeDeleteProjectInvariant)
					// PreToolUse safety policy replace (operator-editable).
					r.Put("/tool-policy", h.PutToolPolicy)
					// R4.2 · mobile -> claude.ai web session bridge.
					r.Post("/spawn-remote", h.OfficeSpawnRemote)
					// R4.4 · `claude stop` shellout (mutating; under CSRF).
					r.Post("/claude-sessions/{sessionId}/stop", h.OfficeClaudeStop)
				})

			})

			// Concierge endpoints (Gemini voice assistant).
			r.Route("/concierge", func(r chi.Router) {
				r.Get("/config", h.ConciergeConfig)
				r.Get("/templates", h.ConciergeTemplates)
				// Concierge spawn uses the same session cookie auth; CSRF inside the
				// CSRF subgroup.
				r.Group(func(r chi.Router) {
					r.Use(mw.CSRF(s.csrfStore))
					r.Post("/spawn-team", h.ConciergeSpawnTeam)
				})
			})
		})
	})

	// Phase 5 of the SPA refresh: /office is now the canonical URL of
	// the React island. Deep-linked tabs / hash anchors on /office#...
	// resolve to the SPA's root view (which has its own router), so
	// stale bookmarks land somewhere sensible rather than 404.
	//
	// The legacy vanilla shell remains reachable at /office-classic
	// for any feature gaps the SPA hasn't covered yet.
	r.Get("/office", func(w http.ResponseWriter, r *http.Request) {
		target := "/office-v2"
		if r.URL.RawQuery != "" {
			target = target + "?" + r.URL.RawQuery
		}
		if r.URL.Fragment != "" {
			target = target + "#" + r.URL.Fragment
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
	r.Get("/office-classic", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/office.html", http.StatusFound)
	})

	// P13.5 · React island. Serves the built SPA shell at /office-v2 and
	// rewrites any sub-route (e.g. /office-v2/agents/:id) onto the same
	// index.html so client-side routing works. When the bundle hasn't
	// been built yet, serves a placeholder page pointing at the README.
	// Static asset root. OFFICE_SPA_DIR (a mounted volume in prod) overrides
	// the repo-relative dev default so the SPA can be deployed independently
	// of this binary; static.Handler/FileExists are disk-first with an
	// embedded-FS fallback, so an unset or missing dir degrades to the
	// embedded build. Shared by the /office-v2 handler and the catch-all
	// static handler below.
	publicDir := static.PublicDirFromEnv("internal/web/static/public")
	officeV2 := func(w http.ResponseWriter, r *http.Request) {
		if !static.FileExists(publicDir, "react/index.html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(officeV2PlaceholderHTML))
			return
		}
		// Route to the React dir (trailing slash). FileServer would 301
		// "/react/index.html" to "./" to canonicalize, breaking the page
		// behind the auth wall; "/react/" makes it serve index.html in
		// place with no redirect.
		r.URL.Path = "/react/"
		static.Handler(publicDir).ServeHTTP(w, r)
	}
	r.Get("/office-v2", officeV2)
	r.Get("/office-v2/*", officeV2)

	// Static files (frontend) — served from the resolved publicDir above
	// (OFFICE_SPA_DIR volume in prod, repo-relative dir in dev).
	// Static files with auth-aware redirect for HTML pages
	staticHandler := static.Handler(publicDir)
	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		// If auth is configured and user is not authenticated,
		// redirect HTML page requests to login.html
		if s.authManager.IsPasswordConfigured() && !s.authManager.ValidateSessionFromRequest(r) {
			path := r.URL.Path
			// Allow login.html, setup.html, and all static assets (JS/CSS/images)
			if path == "/login.html" || path == "/setup.html" ||
				strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") ||
				strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".svg") ||
				strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, ".woff2") {
				staticHandler.ServeHTTP(w, r)
				return
			}
			// Redirect HTML pages (/, /office.html, etc.) to login
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})

	return r
}
