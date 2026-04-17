package handler

import "github.com/go-chi/chi/v5"

// RegisterSessionRoutes registers all session-related routes.
// Includes core CRUD, terminal, extended session state, respawn, plans, and fix-plans.
func RegisterSessionRoutes(r chi.Router, h *Context) {
	// --- Core session routes (session.go) ---
	r.Get("/api/sessions", h.ListSessions)
	r.Get("/api/sessions/query", h.QuerySessions)
	r.Get("/api/sessions/{id}", h.GetSession)
	r.Get("/api/sessions/{id}/terminal", h.GetSessionTerminal)
	r.Post("/api/sessions", h.CreateSession)
	r.Delete("/api/sessions/{id}", h.DeleteSession)
	r.Post("/api/sessions/{id}/input", h.SessionInput)

	// --- Extended session routes (session_extended.go) ---

	// Rename / color / bulk delete
	r.Put("/api/sessions/{id}/name", h.RenameSession)
	r.Put("/api/sessions/{id}/color", h.UpdateSessionColor)
	r.Delete("/api/sessions", h.DeleteAllSessions)

	// Bulk operations
	r.Post("/api/sessions/bulk-delete", h.BulkDeleteSessions)
	r.Post("/api/sessions/bulk-rename", h.BulkRenameSessions)
	r.Post("/api/sessions/bulk-color", h.BulkUpdateSessionColors)

	// Session clone + one-shot prompt
	r.Post("/api/sessions/one-shot", h.OneShotSession)
	r.Post("/api/sessions/{id}/clone", h.CloneSession)

	// Spending visibility + daily cap
	r.Get("/api/spending/status", h.GetSpendingStatus)
	r.Put("/api/spending/cap", h.SetSpendingCap)

	// Scheduler (cron-style durable jobs)
	r.Get("/api/scheduler/jobs", h.ListSchedulerJobs)
	r.Post("/api/scheduler/jobs", h.CreateSchedulerJob)
	r.Get("/api/scheduler/jobs/{id}", h.GetSchedulerJob)
	r.Put("/api/scheduler/jobs/{id}", h.UpdateSchedulerJob)
	r.Delete("/api/scheduler/jobs/{id}", h.DeleteSchedulerJob)
	r.Post("/api/scheduler/jobs/{id}/run-now", h.RunSchedulerJobNow)
	r.Get("/api/scheduler/jobs/{id}/history", h.GetSchedulerJobHistory)

	// Notification routes (SSE event fan-out to Telegram / webhook / log)
	r.Get("/api/notifications/routes", h.ListNotificationRoutes)
	r.Post("/api/notifications/routes", h.CreateNotificationRoute)
	r.Get("/api/notifications/routes/{id}", h.GetNotificationRoute)
	r.Put("/api/notifications/routes/{id}", h.UpdateNotificationRoute)
	r.Delete("/api/notifications/routes/{id}", h.DeleteNotificationRoute)
	r.Post("/api/notifications/routes/{id}/test", h.TestNotificationRoute)

	// Session data views
	r.Get("/api/sessions/{id}/output", h.GetSessionOutput)
	r.Get("/api/sessions/{id}/ralph-state", h.GetSessionRalphState)
	r.Get("/api/sessions/{id}/git-status", h.GetSessionGitStatus)
	r.Get("/api/sessions/{id}/build-status", h.GetSessionBuildStatus)
	r.Get("/api/sessions/{id}/test-status", h.GetSessionTestStatus)
	r.Get("/api/sessions/{id}/hmr-status", h.GetSessionHMRStatus)
	r.Get("/api/sessions/{id}/integrations", h.GetSessionIntegrations)
	r.Get("/api/sessions/{id}/run-summary", h.GetSessionRunSummary)
	r.Get("/api/sessions/{id}/active-tools", h.GetSessionActiveTools)
	r.Get("/api/sessions/{id}/files", h.GetSessionFiles)
	r.Get("/api/sessions/{id}/file-content", h.GetSessionFileContent)

	// Session actions
	r.Post("/api/sessions/{id}/run", h.RunSession)
	r.Post("/api/sessions/{id}/interactive", h.SessionInteractive)
	r.Post("/api/sessions/{id}/shell", h.SessionShell)
	r.Post("/api/sessions/{id}/resize", h.SessionResize)

	// Respawn
	r.Get("/api/sessions/{id}/respawn", h.GetSessionRespawn)
	r.Get("/api/sessions/{id}/respawn/config", h.GetSessionRespawnConfig)
	r.Post("/api/sessions/{id}/respawn/start", h.StartSessionRespawn)
	r.Post("/api/sessions/{id}/respawn/stop", h.StopSessionRespawn)
	r.Put("/api/sessions/{id}/respawn/config", h.UpdateSessionRespawnConfig)
	r.Post("/api/sessions/{id}/respawn/enable", h.EnableSessionRespawn)

	// Session toggles
	r.Post("/api/sessions/{id}/auto-clear", h.SessionAutoClear)
	r.Post("/api/sessions/{id}/auto-compact", h.SessionAutoCompact)
	r.Post("/api/sessions/{id}/auto-trim", h.SessionAutoTrim)
	r.Post("/api/sessions/{id}/image-watcher", h.SessionImageWatcher)
	r.Post("/api/sessions/{id}/flicker-filter", h.SessionFlickerFilter)

	// CPU limit
	r.Get("/api/sessions/{id}/cpu-limit", h.GetSessionCPULimit)
	r.Post("/api/sessions/{id}/cpu-limit", h.SetSessionCPULimit)

	// Ralph
	r.Post("/api/sessions/{id}/ralph-config", h.SetSessionRalphConfig)
	r.Post("/api/sessions/{id}/ralph-circuit-breaker/reset", h.ResetRalphCircuitBreaker)
	r.Get("/api/sessions/{id}/ralph-status", h.GetSessionRalphStatus)
	r.Post("/api/sessions/{id}/ralph-prompt/write", h.WriteSessionRalphPrompt)

	// Context
	r.Get("/api/sessions/{id}/context", h.GetSessionContext)
	r.Post("/api/sessions/{id}/context/trim", h.TrimSessionContext)

	// Interactive respawn
	r.Post("/api/sessions/{id}/interactive-respawn", h.SessionInteractiveRespawn)

	// Export / import
	r.Get("/api/sessions/{id}/export", h.ExportSession)
	r.Post("/api/sessions/import", h.ImportSession)

	// Fix plan
	r.Get("/api/sessions/{id}/fix-plan", h.GetSessionFixPlan)
	r.Post("/api/sessions/{id}/fix-plan/import", h.ImportSessionFixPlan)
	r.Post("/api/sessions/{id}/fix-plan/write", h.WriteSessionFixPlan)
	r.Post("/api/sessions/{id}/fix-plan/read", h.ReadSessionFixPlan)

	// Plan versioning
	r.Post("/api/sessions/{id}/plan/checkpoint", h.CheckpointSessionPlan)
	r.Get("/api/sessions/{id}/plan/history", h.GetSessionPlanHistory)
	r.Post("/api/sessions/{id}/plan/rollback/{version}", h.RollbackSessionPlan)
	r.Post("/api/sessions/{id}/plan/task", h.SessionPlanTask)
}

// RegisterMonitoringRoutes registers health, auth, screens, and subagent routes.
func RegisterMonitoringRoutes(r chi.Router, h *Context) {
	// --- Core monitoring routes (monitoring.go) ---
	r.Get("/api/health", h.Health)
	r.Get("/api/ready", h.Ready)
	r.Get("/api/status", h.Status)
	r.Get("/api/csrf-token", h.CSRFToken)

	// --- Extended monitoring routes (monitoring_extended.go) ---

	// Auth
	r.Get("/api/auth/status", h.GetAuthStatus)
	r.Post("/api/auth/login", h.AuthLogin)
	r.Post("/api/auth/logout", h.AuthLogout)
	r.Get("/api/claude/auth-status", h.GetClaudeAuthStatus)

	// Screens
	r.Get("/api/screens", h.GetScreens)
	r.Delete("/api/screens/{sessionId}", h.DeleteScreen)
	r.Post("/api/screens/reconcile", h.ReconcileScreens)

	// Subagents
	r.Get("/api/subagents", h.GetSubagents)
	r.Get("/api/subagents/{agentId}", h.GetSubagent)
	r.Delete("/api/subagents/{agentId}", h.DeleteSubagent)

	// Subagent window states
	r.Get("/api/subagent-window-states", h.GetSubagentWindowStates)
	r.Put("/api/subagent-window-states", h.UpdateSubagentWindowStates)

	// Subagent parents
	r.Get("/api/subagent-parents", h.GetSubagentParents)
	r.Put("/api/subagent-parents", h.UpdateSubagentParents)
}

// RegisterConfigRoutes registers settings, config, prompt processor, and respawn learning routes.
func RegisterConfigRoutes(r chi.Router, h *Context) {
	// Stats
	r.Get("/api/stats", h.GetStats)
	r.Get("/api/token-stats", h.GetTokenStats)

	// Config
	r.Get("/api/config", h.GetConfig)
	r.Put("/api/config", h.UpdateConfig)

	// Settings
	r.Get("/api/settings", h.GetSettings)
	r.Put("/api/settings", h.UpdateSettings)
	r.Get("/api/settings/accounts", h.GetSettingsAccounts)
	r.Post("/api/settings/accounts", h.CreateSettingsAccount)

	// Debug / system
	r.Get("/api/debug/memory", h.GetDebugMemory)
	r.Get("/api/system/stats", h.GetSystemStats)
	r.Post("/api/cleanup-state", h.CleanupState)

	// Prompt processor
	r.Get("/api/prompt-processor", h.GetPromptProcessor)
	r.Put("/api/prompt-processor", h.UpdatePromptProcessor)
	r.Get("/api/prompt-processor/rules", h.GetPromptProcessorRules)
	r.Put("/api/prompt-processor/rules", h.UpdatePromptProcessorRules)

	// Respawn learning
	r.Get("/api/respawn/learning", h.GetRespawnLearning)
	r.Get("/api/respawn/learning/stats", h.GetRespawnLearningStats)
	r.Get("/api/respawn/smart-kickstart", h.GetRespawnSmartKickstart)
	r.Put("/api/respawn/smart-kickstart", h.UpdateRespawnSmartKickstart)
	r.Get("/api/respawn/smart-kickstart/stats", h.GetRespawnSmartKickstartStats)
}

// RegisterMiscRoutes registers templates, telegram, hooks, cases, scheduled tasks,
// run, generate-plan, and code-printer routes.
func RegisterMiscRoutes(r chi.Router, h *Context) {
	// Templates
	r.Get("/api/templates", h.ListTemplates)
	r.Get("/api/templates/{id}", h.GetTemplate)
	r.Post("/api/templates", h.CreateTemplate)
	r.Put("/api/templates/{id}", h.UpdateTemplate)
	r.Delete("/api/templates/{id}", h.DeleteTemplate)
	r.Post("/api/templates/{id}/duplicate", h.DuplicateTemplate)
	r.Get("/api/templates/{id}/export", h.ExportTemplate)
	r.Post("/api/templates/import", h.ImportTemplate)

	// Telegram
	r.Get("/api/telegram/status", h.GetTelegramStatus)
	r.Post("/api/telegram/config", h.UpdateTelegramConfig)
	r.Post("/api/telegram/test", h.SendTelegramTest)
	r.Post("/api/telegram/connect", h.ConnectTelegram)
	r.Post("/api/telegram/disconnect", h.DisconnectTelegram)

	// Hooks
	r.Post("/api/hook-event", h.HandleHookEvent)
	r.Get("/api/hook-config", h.GetHookConfig)

	// Task gate
	r.Get("/api/task-gate/{sessionId}", h.GetTaskGate)

	// Cases
	r.Get("/api/cases", h.ListCases)
	r.Get("/api/cases/{name}", h.GetCase)
	r.Post("/api/cases", h.CreateCase)
	r.Put("/api/cases/{name}", h.UpdateCase)
	r.Delete("/api/cases/{name}", h.DeleteCase)

	// Quick start
	r.Post("/api/quick-start", h.QuickStart)

	// Scheduled tasks
	r.Get("/api/scheduled", h.ListScheduledTasks)
	r.Post("/api/scheduled", h.CreateScheduledTask)
	r.Get("/api/scheduled/{id}", h.GetScheduledTask)
	r.Put("/api/scheduled/{id}", h.UpdateScheduledTask)
	r.Delete("/api/scheduled/{id}", h.DeleteScheduledTask)

	// Run
	r.Post("/api/run", h.Run)

	// Generate plan
	r.Post("/api/generate-plan", h.GeneratePlan)

	// Code printer
	r.Post("/api/code-printer/print", h.CodePrinterPrint)
	r.Post("/api/code-printer/preview", h.CodePrinterPreview)
	r.Get("/api/code-printer/config", h.CodePrinterConfig)
}
