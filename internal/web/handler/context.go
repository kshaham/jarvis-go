package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/claudehooks"
	"github.com/kshaham/jarvis/internal/integrations"
	"github.com/kshaham/jarvis/internal/notifications"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/scheduler"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/telegram"
	"github.com/kshaham/jarvis/internal/templates"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// Context holds shared dependencies injected into all handlers.
type Context struct {
	SessionManager      *pty.SessionManager
	SSEBroker           *sse.Broker
	CSRFStore           *middleware.CSRFStore
	AuthManager         *auth.Manager
	OfficeManager       *office.Manager
	StateStore          *state.AppState
	ScreenManager       *pty.ScreenManager
	SubagentWatcher     *subagent.Watcher
	CaseManager         *cases.CaseManager
	RespawnControllers  map[string]*respawn.Controller // sessionID -> controller
	RespawnMu           sync.RWMutex
	TemplateStore       *templates.Store
	TeamTemplateStore   *teamtemplates.Store
	AgentTokenMgr       *auth.AgentTokenManager
	MobileTokenMgr      *auth.MobileTokenManager
	ToolPolicy          *claudehooks.ToolPolicyStore
	Scheduler           *scheduler.Store
	SchedulerRunner     *scheduler.Runner
	Notifications       *notifications.Store
	NotificationsRouter *notifications.Router
	TelegramBot         *telegram.Bot
	TelegramHandler     *telegram.Handler
	IntegrationMonitor  *integrations.Monitor
	ScheduledTasks      map[string]interface{}
	ScheduledTasksMu    sync.RWMutex
	SessionColors       map[string]string // sessionID -> color label
	SessionColorsMu     sync.RWMutex
	RalphTrackers       map[string]*ralph.Tracker // sessionID -> tracker
	RalphTrackersMu     sync.RWMutex
	RunSummaries        map[string]*runsummary.Tracker // sessionID -> tracker
	RunSummariesMu      sync.RWMutex

	// TailFileStreams tracks active SSE file-tail streams for cleanup.
	TailFileStreams   map[string]func() // streamID -> cancel func
	TailFileStreamsMu sync.RWMutex

	// ActivePlanOrchestrators tracks active plan generation session IDs.
	ActivePlanOrchestrators   map[string]string // orchestratorID -> sessionID
	ActivePlanOrchestratorsMu sync.RWMutex
}

// GetOrCreateRespawnController returns the respawn controller for a session,
// creating one with default config if it does not yet exist.
func (ctx *Context) GetOrCreateRespawnController(sessionID string) *respawn.Controller {
	ctx.RespawnMu.RLock()
	rc, ok := ctx.RespawnControllers[sessionID]
	ctx.RespawnMu.RUnlock()
	if ok {
		return rc
	}
	ctx.RespawnMu.Lock()
	defer ctx.RespawnMu.Unlock()
	// Double-check after write lock
	if rc, ok = ctx.RespawnControllers[sessionID]; ok {
		return rc
	}
	rc = respawn.NewController(sessionID, respawn.DefaultRespawnConfig())
	ctx.RespawnControllers[sessionID] = rc
	return rc
}

// cleanupSessionResources removes all tracked resources for a session, stopping
// goroutines (Ralph trackers, respawn controllers) and freeing memory (run
// summaries, session colors). Call after stopping a session.
func (ctx *Context) cleanupSessionResources(sessionID string) {
	ctx.RalphTrackersMu.Lock()
	if rt, ok := ctx.RalphTrackers[sessionID]; ok {
		rt.Stop()
		delete(ctx.RalphTrackers, sessionID)
	}
	ctx.RalphTrackersMu.Unlock()

	ctx.RunSummariesMu.Lock()
	delete(ctx.RunSummaries, sessionID)
	ctx.RunSummariesMu.Unlock()

	ctx.RespawnMu.Lock()
	if rc, ok := ctx.RespawnControllers[sessionID]; ok {
		rc.Stop()
		delete(ctx.RespawnControllers, sessionID)
	}
	ctx.RespawnMu.Unlock()

	ctx.SessionColorsMu.Lock()
	delete(ctx.SessionColors, sessionID)
	ctx.SessionColorsMu.Unlock()

	ctx.ScheduledTasksMu.Lock()
	delete(ctx.ScheduledTasks, sessionID)
	ctx.ScheduledTasksMu.Unlock()

	ctx.ActivePlanOrchestratorsMu.Lock()
	for orchID, sid := range ctx.ActivePlanOrchestrators {
		if sid == sessionID {
			delete(ctx.ActivePlanOrchestrators, orchID)
		}
	}
	ctx.ActivePlanOrchestratorsMu.Unlock()
}

// initSessionTrackers installs the Ralph + RunSummary trackers for
// a freshly-created session. Symmetric with cleanupSessionResources
// — every CreateSession callsite must invoke this so the dashboard's
// /api/sessions/{id}/ralph + /run-summary endpoints have populated
// state to serve.
//
// Pre-extraction the same five lines were inlined at 5 handler call
// sites (QuickStart, GeneratePlanDetailed, ImportSession, two clone
// paths, the bare create-session). One site dropped the RunSummary
// install at some point and silently broke the run-summary panel
// until the next refactor pass; centralising removes that footgun.
func (ctx *Context) initSessionTrackers(sessionID string) {
	ctx.RalphTrackersMu.Lock()
	ctx.RalphTrackers[sessionID] = ralph.NewTracker()
	ctx.RalphTrackersMu.Unlock()

	ctx.RunSummariesMu.Lock()
	ctx.RunSummaries[sessionID] = runsummary.New(sessionID)
	ctx.RunSummariesMu.Unlock()
}

// ResolveWorkingDir reads the configured allowlist from settings (or falls
// back to $HOME only) and validates the user-supplied path against it via
// pty.ResolveAndValidateWorkingDir. Returns the canonical path or an error
// suitable for an HTTP 400.
func (ctx *Context) ResolveWorkingDir(raw string) (string, error) {
	settings := ctx.StateStore.GetSettings()
	var allow []string
	if v, ok := settings["workingDirAllowlist"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				allow = append(allow, s)
			}
		}
	}
	if len(allow) == 0 {
		allow = pty.DefaultWorkingDirAllowlist()
	}
	return pty.ResolveAndValidateWorkingDir(raw, allow)
}

// getProviderEnvVars reads account settings and returns env vars for Claude CLI sessions.
// Supports: claude-subscription (OAuth from credentials file), claude-api (ANTHROPIC_API_KEY),
// zai-glm (z.ai provider vars), and custom providers.
func (ctx *Context) getProviderEnvVars() map[string]string {
	env := make(map[string]string)
	// Every spawned session receives the agent token so `cm` can authenticate
	// to /api/office/agent/* endpoints. The token is regenerated only if the
	// operator manually deletes the file on disk.
	if ctx.AgentTokenMgr != nil {
		if tok := ctx.AgentTokenMgr.Token(); tok != "" {
			env["JARVIS_AGENT_TOKEN"] = tok
		}
	}
	settings := ctx.StateStore.GetSettings()

	provider, _ := settings["provider"].(string)

	// Make the bare `opus` alias resolve to Opus 4.8 (the model that authors
	// Dynamic Workflows) so it never silently downgrades to an older Opus on
	// any provider. Not an auth var — safe for subscription mode. The zai-glm
	// case below overwrites it with the GLM mapping; Claude providers keep it.
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "claude-opus-4-8"

	switch provider {
	case "claude-subscription", "":
		// Don't set any auth env vars for subscription mode.
		// The user authenticates via `claude auth login` inside the container,
		// which stores credentials in Claude's internal credential store.
		// Setting ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY would override
		// the subscription auth and force API billing mode.
	case "claude-api":
		if key, ok := settings["claudeApiKey"].(string); ok && key != "" {
			env["ANTHROPIC_API_KEY"] = key
		}
	case "zai-glm":
		if key, ok := settings["zaiApiKey"].(string); ok && key != "" {
			env["ANTHROPIC_AUTH_TOKEN"] = key
			// Opus model (complex tasks, premium -- uses 3x quota during peak)
			opusModel, _ := settings["zaiModel"].(string)
			if opusModel == "" {
				opusModel = "glm-5.1"
			}
			// Sonnet model (routine tasks, budget-friendly)
			sonnetModel, _ := settings["zaiSonnetModel"].(string)
			if sonnetModel == "" {
				sonnetModel = "glm-4.7"
			}
			// Haiku model (fast tasks, cheapest)
			haikuModel, _ := settings["zaiHaikuModel"].(string)
			if haikuModel == "" {
				haikuModel = "glm-4.5-air"
			}
			// Z.AI Coding Plan uses ANTHROPIC_DEFAULT_*_MODEL vars.
			// Claude Code maps Haiku/Sonnet/Opus internally to these defaults.
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = opusModel
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = sonnetModel
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = haikuModel
			env["API_TIMEOUT_MS"] = "3000000"
			baseURL, _ := settings["zaiBaseUrl"].(string)
			if baseURL == "" {
				baseURL = "https://api.z.ai/api/anthropic"
			}
			env["ANTHROPIC_BASE_URL"] = baseURL
		}
	}

	return env
}

// AllocateZAIModels was moved to internal/office.AllocateZAIModels.
// This var keeps the existing local-name call sites compiling without
// churn during the migration; the canonical implementation lives in
// internal/office/zai_allocation.go.
var AllocateZAIModels = office.AllocateZAIModels

// getProviderEnvVarsFor returns env vars for a specific provider/model combination.
// Used for per-member overrides in office team spawning.
// sonnetOverride allows per-agent Sonnet model assignment for rate limit distribution.
func (ctx *Context) getProviderEnvVarsFor(provider, model, sonnetOverride string) map[string]string {
	env := make(map[string]string)
	settings := ctx.StateStore.GetSettings()

	// Pin the `opus` alias to Opus 4.8 (zai-glm case overwrites below).
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "claude-opus-4-8"

	switch provider {
	case "claude-subscription", "":
		// No env vars needed -- uses container's Claude auth
	case "claude-api":
		if key, ok := settings["claudeApiKey"].(string); ok && key != "" {
			env["ANTHROPIC_API_KEY"] = key
		}
	case "zai-glm":
		if key, ok := settings["zaiApiKey"].(string); ok && key != "" {
			env["ANTHROPIC_AUTH_TOKEN"] = key
			if model == "" {
				model = "glm-5.1"
			}
			sonnetModel := sonnetOverride
			if sonnetModel == "" {
				sonnetModel, _ = settings["zaiSonnetModel"].(string)
				if sonnetModel == "" {
					sonnetModel = "glm-4.7"
				}
			}
			haikuModel, _ := settings["zaiHaikuModel"].(string)
			if haikuModel == "" {
				haikuModel = "glm-4.5-air"
			}
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = sonnetModel
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = haikuModel
			env["API_TIMEOUT_MS"] = "3000000"
			baseURL, _ := settings["zaiBaseUrl"].(string)
			if baseURL == "" {
				baseURL = "https://api.z.ai/api/anthropic"
			}
			env["ANTHROPIC_BASE_URL"] = baseURL
		}
	}

	return env
}

// readClaudeOAuthToken reads the OAuth access token from ~/.claude/.credentials.json.
// Returns empty string if file doesn't exist or token is expired.
func readClaudeOAuthToken() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	credPath := filepath.Join(homeDir, ".claude", ".credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		// Also try the auth volume path (Docker container setup)
		credPath = filepath.Join(homeDir, ".claude-auth", ".credentials.json")
		data, err = os.ReadFile(credPath)
		if err != nil {
			return ""
		}
	}

	var creds map[string]interface{}
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}

	// Try claudeAiOauth first, then oauthAccount
	oauth, _ := creds["claudeAiOauth"].(map[string]interface{})
	if oauth == nil {
		oauth, _ = creds["oauthAccount"].(map[string]interface{})
	}
	if oauth == nil {
		return ""
	}

	token, _ := oauth["accessToken"].(string)
	if token == "" {
		token, _ = oauth["token"].(string)
	}

	return token
}
