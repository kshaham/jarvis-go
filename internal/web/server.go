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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/cases"
	"github.com/claudeman/jarvis-go/internal/integrations"
	"github.com/claudeman/jarvis-go/internal/metrics"
	"github.com/claudeman/jarvis-go/internal/notifications"
	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/ralph"
	"github.com/claudeman/jarvis-go/internal/respawn"
	"github.com/claudeman/jarvis-go/internal/runsummary"
	"github.com/claudeman/jarvis-go/internal/scheduler"
	"github.com/claudeman/jarvis-go/internal/state"
	"github.com/claudeman/jarvis-go/internal/subagent"
	"github.com/claudeman/jarvis-go/internal/teamtemplates"
	"github.com/claudeman/jarvis-go/internal/telegram"
	"github.com/claudeman/jarvis-go/internal/templates"
	"github.com/claudeman/jarvis-go/internal/web/auth"
	"github.com/claudeman/jarvis-go/internal/web/handler"
	mw "github.com/claudeman/jarvis-go/internal/web/middleware"
	"github.com/claudeman/jarvis-go/internal/web/sse"
	"github.com/claudeman/jarvis-go/internal/web/static"
)

// Server is the main HTTP server for Jarvis.
type Server struct {
	port            int
	isHTTPS         bool
	dataDir         string
	httpServer      *http.Server
	sessionManager  *pty.SessionManager
	sseBroker       *sse.Broker
	csrfStore       *mw.CSRFStore
	authManager     *auth.Manager
	agentTokenMgr   *auth.AgentTokenManager
	officeManager   *office.Manager
	stateStore      *state.AppState
	screenManager   *pty.ScreenManager
	subagentWatcher *subagent.Watcher
	caseManager     *cases.CaseManager
	templateStore     *templates.Store
	teamTemplateStore *teamtemplates.Store
	schedulerStore   *scheduler.Store
	schedulerRunner  *scheduler.Runner
	notificationsStore  *notifications.Store
	notificationsRouter *notifications.Router
	telegramBot       *telegram.Bot
	telegramHandler *telegram.Handler
	handlerCtx      *handler.Context
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
				"tokens": map[string]int64{"input": input, "output": output, "total": total},
			})
		}
		// Include SDK sessions in the session list
		sessionManager.ForEachSDKSession(func(id string, sdk *pty.SDKSession) {
			sessionList = append(sessionList, map[string]interface{}{
				"id": sdk.ID, "name": sdk.GetName(), "status": string(sdk.GetStatus()),
				"mode": string(sdk.Mode), "workingDir": sdk.WorkingDir,
				"pid": 0, "createdAt": sdk.CreatedAt,
				"isWorking": sdk.IsWorking(),
				"tokens": map[string]int64{"input": 0, "output": 0, "total": 0},
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

		return map[string]interface{}{
			"type":           "init",
			"sessions":       sessionList,
			"scheduledRuns":  []interface{}{},
			"respawnStatus":  respawnStatus,
			"version":        "0.2.0-go",
			"settings":       stateStore.GetSettings(),
			"config":         stateStore.GetConfig(),
			"globalStats": map[string]interface{}{
				"sessionCount": len(sessionList),
			},
		}
	})

	authMgr := auth.NewManager(dataDir, isHTTPS)
	agentTokenMgr := auth.NewAgentTokenManager(dataDir)

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
		port:            port,
		isHTTPS:         isHTTPS,
		dataDir:         dataDir,
		sessionManager:  sessionManager,
		sseBroker:       broker,
		csrfStore:       mw.NewCSRFStore(ctx),
		authManager:     authMgr,
		agentTokenMgr:   agentTokenMgr,
		officeManager:   officeMgr,
		stateStore:      stateStore,
		screenManager:   screenMgr,
		subagentWatcher: subagentW,
		caseManager:     caseMgr,
		templateStore:     tmplStore,
		teamTemplateStore: teamTmplStore,
		schedulerStore:    schedulerStore,
		notificationsStore: notificationsStore,
		telegramBot:       tgBot,
		telegramHandler: tgHandler,
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
	// Try PTY session first, then SDK session
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.WriteViaScreen(input)
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.WriteViaScreen(input)
	}
	return false
}

func (a *sessionStoreAdapter) WriteInkPrompt(sessionID, prompt string) bool {
	// Try PTY session first, then SDK session
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.WriteInkPrompt(prompt)
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.WriteInkPrompt(prompt)
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
	return false
}

func (a *sessionStoreAdapter) IsSessionWorking(sessionID string) bool {
	if s, ok := a.sm.GetSession(sessionID); ok {
		return s.IsWorking()
	}
	if sdk, ok := a.sm.GetSDKSession(sessionID); ok {
		return sdk.IsWorking()
	}
	return false
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
	router := s.setupRouter()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE needs unlimited write timeout
		IdleTimeout:  120 * time.Second,
	}

	// Start Telegram handler (polling/webhook)
	s.telegramHandler.Start()

	// Start the scheduler runner: loads persisted jobs, schedules them, and
	// fires any that were due while the server was offline.
	if s.schedulerRunner != nil {
		s.schedulerRunner.Start(context.Background())
	}

	// Start the notifications router worker pool. Pre-hook registration in
	// setupRouter has already wired it into the SSE broker, so once workers
	// start they immediately begin draining any queued work.
	if s.notificationsRouter != nil {
		s.notificationsRouter.Start(context.Background())
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
			Addr:         fmt.Sprintf(":%d", httpsPort),
			Handler:      router,
			TLSConfig:    tlsCfg,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0,
			IdleTimeout:  120 * time.Second,
		}
		slog.Info("Starting HTTPS listener", "port", httpsPort, "tls_dir", filepath.Join(s.dataDir, "tls"))
		go func() {
			if err := tlsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTPS listener failed", "error", err)
			}
		}()
	}

	return s.httpServer.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	slog.Info("Shutting down server...")
	if s.schedulerRunner != nil {
		s.schedulerRunner.Stop()
	}
	if s.notificationsRouter != nil {
		s.notificationsRouter.Stop()
	}
	s.telegramHandler.Stop()
	return s.httpServer.Shutdown(ctx)
}

// SSEBroker returns the SSE broker for external event broadcasting.
func (s *Server) SSEBroker() *sse.Broker {
	return s.sseBroker
}

func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	// Global middleware. RequestID runs first so every subsequent log line
	// and the response header carry a correlation ID.
	r.Use(mw.Recovery)
	r.Use(mw.RequestID)
	r.Use(mw.RequestLogger)
	r.Use(mw.SecurityHeaders(s.isHTTPS))

	intMon := integrations.NewMonitor()

	// Handler context with shared dependencies
	h := &handler.Context{
		SessionManager:     s.sessionManager,
		SSEBroker:          s.sseBroker,
		CSRFStore:          s.csrfStore,
		AuthManager:        s.authManager,
		OfficeManager:      s.officeManager,
		StateStore:         s.stateStore,
		ScreenManager:      s.screenManager,
		SubagentWatcher:    s.subagentWatcher,
		CaseManager:        s.caseManager,
		RespawnControllers: make(map[string]*respawn.Controller),
		TemplateStore:      s.templateStore,
		TeamTemplateStore:  s.teamTemplateStore,
		AgentTokenMgr:      s.agentTokenMgr,
		TelegramBot:        s.telegramBot,
		TelegramHandler:    s.telegramHandler,
		IntegrationMonitor: intMon,
		ScheduledTasks:     make(map[string]interface{}),
		SessionColors:      make(map[string]string),
		RalphTrackers:           make(map[string]*ralph.Tracker),
		RunSummaries:            make(map[string]*runsummary.Tracker),
		TailFileStreams:         make(map[string]func()),
		ActivePlanOrchestrators: make(map[string]string),
	}
	// Spending threshold tracking is private to the handler package; the
	// map must be initialized before any RecordDailyStats call.
	h.InitSpendingTracker()
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
	s.handlerCtx = h

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
	// scrape configs don't need a custom path). No auth: firewall / reverse
	// proxy is expected to restrict access to internal networks.
	r.Handle("/metrics", metrics.Handler())

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
			r.Post("/auth/logout", h.AuthLogout)
			r.Post("/auth/change-password", h.AuthChangePassword)
			// Telegram webhook (auth via secret in URL path)
			r.Post("/telegram/webhook/{secret}", s.telegramHandler.WebhookHandler)

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
				r.Get("/skills/search", h.OfficeAgentSearchSkills)
				r.Get("/skills/{name}", h.OfficeAgentGetSkillByName)
				r.Get("/memory", h.OfficeAgentGetMemory)
				r.Get("/memory/section", h.OfficeAgentGetMemorySection)
				r.Get("/memory/query", h.OfficeAgentGetMemoryQuery)
				// Mutating
				r.Post("/standup/response", h.OfficeAgentStandupResponse)
				r.Post("/message", h.OfficeAgentMessage)
				r.Post("/task-start", h.OfficeAgentTaskStart)
				r.Post("/task-complete", h.OfficeAgentTaskComplete)
				r.Post("/task-ack", h.OfficeAgentTaskAck)
				r.Post("/task-progress", h.OfficeAgentTaskProgress)
				r.Post("/task/self-add", h.OfficeAgentTaskSelfAdd)
				r.Post("/task/triage", h.OfficeAgentTaskTriage)
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
			})
		})

		// Authenticated routes. Accept either a valid browser session cookie
		// OR a valid agent token -- jarvis-mcp running inside spawned sessions
		// calls /api/status and /api/config from tools, and those calls should
		// succeed as long as the in-session agent token is attached.
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthWithAgentToken(s.authManager, s.agentTokenMgr))

			// SSE endpoint (long-lived, no CSRF)
			r.Get("/events", s.sseBroker.Subscribe)

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

			// Mutating routes (CSRF required)
			r.Group(func(r chi.Router) {
				r.Use(mw.CSRF(s.csrfStore))

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

				// Spending daily cap setter (PUT; GET status is in the read-only group).
				r.Put("/spending/cap", h.SetSpendingCap)

				// Scheduler mutations.
				r.Post("/scheduler/jobs", h.CreateSchedulerJob)
				r.Put("/scheduler/jobs/{id}", h.UpdateSchedulerJob)
				r.Delete("/scheduler/jobs/{id}", h.DeleteSchedulerJob)
				r.Post("/scheduler/jobs/{id}/run-now", h.RunSchedulerJobNow)

				// Notification route mutations.
				r.Post("/notifications/routes", h.CreateNotificationRoute)
				r.Put("/notifications/routes/{id}", h.UpdateNotificationRoute)
				r.Delete("/notifications/routes/{id}", h.DeleteNotificationRoute)
				r.Post("/notifications/routes/{id}/test", h.TestNotificationRoute)

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
				r.Get("/health", h.OfficeHealth)
				r.Get("/settings", h.OfficeGetSettings)
				r.Get("/auto-standup", h.OfficeGetAutoStandup)
				r.Get("/whiteboard", h.OfficeGetWhiteboard)
				r.Get("/task-queues", h.OfficeGetTaskQueues)
				r.Get("/task-queues/{sessionId}", h.OfficeGetTaskQueueBySession)
				r.Get("/bug-tasks", h.OfficeGetBugTasks)
				r.Get("/file-locks", h.OfficeGetFileLocks)
				r.Get("/messages", h.OfficeGetMessages)
				r.Get("/metrics", h.OfficeGetMetrics)
				r.Get("/experiments", h.OfficeGetExperiments)
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
				r.Get("/memory/{sessionId}", h.OfficeGetMemberMemory)
				r.Get("/member/{sessionId}/personality", h.OfficeGetMemberPersonality)
				r.Get("/telegram-config", h.OfficeGetTelegramConfig)

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
					r.Post("/standup", h.OfficePostStandup)
					r.Post("/standup/user-message", h.OfficePostStandupUserMessage)
					r.Put("/member/{sessionId}/personality", h.OfficePutMemberPersonality)
					r.Post("/brainstorm", h.OfficeBrainstorm)
					r.Put("/telegram-config", h.OfficePutTelegramConfig)
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

	// Redirect /office to /office.html (matches Node.js behavior)
	r.Get("/office", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/office.html", http.StatusFound)
	})

	// Static files (frontend)
	publicDir := "internal/web/static/public" // dev path (relative to project root)
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
