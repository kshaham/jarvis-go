package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/claudeman/jarvis-go/internal/cases"
	"github.com/claudeman/jarvis-go/internal/integrations"
	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/ralph"
	"github.com/claudeman/jarvis-go/internal/respawn"
	"github.com/claudeman/jarvis-go/internal/runsummary"
	"github.com/claudeman/jarvis-go/internal/state"
	"github.com/claudeman/jarvis-go/internal/subagent"
	"github.com/claudeman/jarvis-go/internal/telegram"
	"github.com/claudeman/jarvis-go/internal/templates"
	"github.com/claudeman/jarvis-go/internal/web/auth"
	"github.com/claudeman/jarvis-go/internal/web/middleware"
	"github.com/claudeman/jarvis-go/internal/web/sse"
)

// Context holds shared dependencies injected into all handlers.
type Context struct {
	SessionManager     *pty.SessionManager
	SSEBroker          *sse.Broker
	CSRFStore          *middleware.CSRFStore
	AuthManager        *auth.Manager
	OfficeManager      *office.Manager
	StateStore         *state.AppState
	ScreenManager      *pty.ScreenManager
	SubagentWatcher    *subagent.Watcher
	CaseManager        *cases.CaseManager
	RespawnControllers map[string]*respawn.Controller // sessionID -> controller
	RespawnMu          sync.RWMutex
	TemplateStore      *templates.Store
	TelegramBot        *telegram.Bot
	TelegramHandler    *telegram.Handler
	IntegrationMonitor *integrations.Monitor
	ScheduledTasks     map[string]interface{}
	ScheduledTasksMu   sync.RWMutex
	SessionColors      map[string]string // sessionID -> color label
	SessionColorsMu    sync.RWMutex
	RalphTrackers        map[string]*ralph.Tracker       // sessionID -> tracker
	RalphTrackersMu      sync.RWMutex
	RunSummaries         map[string]*runsummary.Tracker   // sessionID -> tracker
	RunSummariesMu       sync.RWMutex

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
}

// getProviderEnvVars reads account settings and returns env vars for Claude CLI sessions.
// Supports: claude-subscription (OAuth from credentials file), claude-api (ANTHROPIC_API_KEY),
// zai-glm (z.ai provider vars), and custom providers.
func (ctx *Context) getProviderEnvVars() map[string]string {
	env := make(map[string]string)
	settings := ctx.StateStore.GetSettings()

	provider, _ := settings["provider"].(string)

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

// Z.AI model rate limits (max concurrent requests per model).
var zaiModelRateLimits = map[string]int{
	"glm-5.1":     2,
	"glm-5":       2,
	"glm-4.7":     3,
	"glm-4.7-flash": 5,
	"glm-4.5-air": 5,
}

// zaiModelDowngradePath defines the fallback chain when a model's slots are full.
var zaiModelDowngradePath = []string{"glm-5.1", "glm-5", "glm-4.7", "glm-4.7-flash", "glm-4.5-air"}

// AllocateZAIModels distributes models across agents to respect rate limits.
// For each agent, it tries to use the requested model; if that model's concurrent
// slots are full, it cascades down to the next model version.
// Returns a map of memberID -> allocated model (for the Sonnet/primary tier).
func AllocateZAIModels(requestedModels map[string]string, memberIDs []string) map[string]string {
	allocated := make(map[string]string)
	usage := make(map[string]int) // model -> count of agents assigned

	for _, id := range memberIDs {
		requested := requestedModels[id]
		if requested == "" {
			requested = "glm-5.1"
		}

		// Try the requested model, then cascade down
		assigned := false
		startIdx := -1
		for i, m := range zaiModelDowngradePath {
			if m == requested {
				startIdx = i
				break
			}
		}
		if startIdx < 0 {
			startIdx = 0 // unknown model, start from top
		}

		for i := startIdx; i < len(zaiModelDowngradePath); i++ {
			model := zaiModelDowngradePath[i]
			limit := zaiModelRateLimits[model]
			if limit == 0 {
				limit = 3 // default
			}
			if usage[model] < limit {
				allocated[id] = model
				usage[model]++
				assigned = true
				break
			}
		}
		if !assigned {
			// All slots full, use the lowest model as fallback
			allocated[id] = "glm-4.7"
			usage["glm-4.7"]++
		}
	}

	return allocated
}

// getProviderEnvVarsFor returns env vars for a specific provider/model combination.
// Used for per-member overrides in office team spawning.
// sonnetOverride allows per-agent Sonnet model assignment for rate limit distribution.
func (ctx *Context) getProviderEnvVarsFor(provider, model, sonnetOverride string) map[string]string {
	env := make(map[string]string)
	settings := ctx.StateStore.GetSettings()

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
