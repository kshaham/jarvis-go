package handler

// Spawn-agent helpers extracted from office.go to keep the latter
// scannable. No behavior change.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/kshaham/jarvis/internal/codexcfg"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

type spawnAgentSpec struct {
	PerAgentCwd          string
	HooksEnabled         bool
	PreassignedSessionID string
	// Override carries the same memberOverride pointer that
	// spawnAgentSession() receives so the hooks branch can resolve
	// R1.3 session tuning (effort, budget, fallback model). Nil when
	// the operator didn't override anything for this member.
	Override *memberOverride
}

// spawnAgentSession creates either an SDK or PTY session for a team member.
// Returns the sessionID and mode used, or empty string on failure.
// R23 · The optional `ov` carries per-member MaxTurns from the spawn
// modal. R24 removed the MaxCostUSD counterpart. When nil, defaults
// apply (defaultMaxTurnsPerSession).
func spawnAgentSession(ctx *Context, member teamtemplates.TeamTemplateMember, req spawnTeamRequest, envVars map[string]string, teamConfigs []*office.TeamMemberConfig, perpetual bool, trySDK bool, ov *memberOverride, provider, model string, spec spawnAgentSpec) (string, string) {
	// Codex harness short-circuit. When the resolved provider is an OpenAI
	// provider (openai-codex / openai-custom) the whole agent runs on the
	// Codex CLI instead of Claude Code — a different backend (ModeCodexExec),
	// config (CODEX_HOME/config.toml), and kickstart. None of the Claude SDK /
	// PTY / hooks branches below apply, so route here first.
	if office.HarnessFor(provider) == "codex" {
		return ctx.spawnCodexAgentSession(member, req, envVars, teamConfigs, perpetual, ov, provider, model)
	}
	// Per-team Dynamic Workflows kill switch. Workflows are ON by default
	// (Claude Code paid-plan default); when the operator opts out we inject
	// CLAUDE_CODE_DISABLE_WORKFLOWS=1, honored on every surface (SDK,
	// headless, interactive). Done before the hooks short-circuit so all
	// three spawn paths (SDK / PTY / hooks) inherit it.
	if req.DisableWorkflows {
		if envVars == nil {
			envVars = map[string]string{}
		}
		envVars["CLAUDE_CODE_DISABLE_WORKFLOWS"] = "1"
	}
	// hooks-mode short-circuits the SDK path entirely: ModeClaude+hooks
	// runs the interactive `claude` CLI in a PTY (billed on the
	// interactive subscription pool, NOT the new SDK credit pool) and
	// receives structured events via HTTP hooks instead of stream-json.
	if spec.HooksEnabled {
		// Propagate the override pointer so the hooks branch resolves
		// the same R1.3 tuning the SDK / PTY branches would.
		spec.Override = ov
		return spawnAgentSessionWithHooks(ctx, member, req, envVars, spec)
	}
	if trySDK {
		agentMemory := office.LoadAgentMemory(member.ID, req.WorkingDir)
		kickstart := office.BuildKickstartPrompt(
			&office.TeamMemberConfig{ID: member.ID, Name: member.Name, Role: member.Role, Tier: member.Tier, Color: member.Color},
			req.WorkingDir, teamConfigs, req.Mission, agentMemory, perpetual,
		)
		// Pre-generate session ID so we can inject it into MCP env and subprocess env.
		sessionID := uuid.New().String()
		// Agent token authorizes /api/office/agent/* calls from `cm` and jarvis-mcp.
		// When per-member env comes from getProviderEnvVarsFor (Z.AI rate-limit
		// allocation) the token is dropped, so we re-inject it here. Without
		// this every `cm skill read`, `cm task list`, etc. gets 401.
		agentToken := ""
		if ctx.AgentTokenMgr != nil {
			agentToken = ctx.AgentTokenMgr.Token()
		}
		mcpServers := ctx.buildOfficeMCPServers(req, sessionID, agentToken)
		// Also inject Jarvis env into subprocess so `cm` bash commands work.
		agentEnv := make(map[string]string, len(envVars)+3)
		for k, v := range envVars {
			agentEnv[k] = v
		}
		agentEnv["JARVIS_API_URL"] = "http://localhost:3555"
		agentEnv["JARVIS_SESSION_ID"] = sessionID
		if agentToken != "" {
			agentEnv["JARVIS_AGENT_TOKEN"] = agentToken
		}
		// R23 · Apply per-member turn cap if the operator set it.
		// R24 removed the per-member cost cap — subscription plans
		// (Z.AI, Claude sub) make dollar caps meaningless.
		maxTurns := defaultMaxTurnsPerSession
		if ov != nil && ov.MaxTurns > 0 {
			maxTurns = ov.MaxTurns
		}
		// Per-member model override. Priority: spawn-modal override >
		// template-pinned member.Model > empty (SDK default). Previously
		// this value was dropped on the floor for claude-subscription /
		// claude-api providers — only Z.AI honored it via env vars —
		// which is why picking "Opus 4.7" for the lead had no effect.
		memberModel := member.Model
		if ov != nil && ov.Model != "" {
			memberModel = ov.Model
		}
		// R31 · Stable cross-agent prefix rides on the system prompt so
		// Anthropic's ephemeral cache (5-min TTL, 90% off cache hits)
		// can discount it across every teammate + every turn. Content
		// must be byte-for-byte identical per tier so cache lookups
		// hit; BuildStableSystemPrompt is deterministic for a given
		// (tier, mission) pair.
		stablePrefix := office.BuildStableSystemPrompt(member.Tier, req.Mission)
		// R2.4 · Write the prefix to the worktree once per spawn and
		// pass it via --append-system-prompt-file. File-backed prompts
		// cache more reliably than inline strings (same byte content +
		// stable path = better cache-key reuse across the team). The
		// inline AppendSystemPrompt stays set as a fallback for the
		// PTY path when no worktree was created.
		appendPromptFile := ""
		if pf, perr := office.WriteAppendSystemPromptFile(req.WorkingDir, stablePrefix); perr != nil {
			slog.Warn("R2.4: WriteAppendSystemPromptFile failed; falling back to inline", "err", perr)
		} else {
			appendPromptFile = pf
		}
		tuning := resolveSessionTuning(req, member.Tier, ov)
		sdkSession, err := ctx.SessionManager.CreateSDKSession(
			context.Background(),
			pty.SessionConfig{
				ID:                                 sessionID,
				WorkingDir:                         req.WorkingDir,
				Mode:                               pty.ModeClaudeSDK,
				Name:                               member.Name,
				ProviderEnv:                        agentEnv,
				SystemPrompt:                       kickstart,
				McpServers:                         mcpServers,
				Model:                              memberModel,
				AppendSystemPrompt:                 stablePrefix,
				AppendSystemPromptFile:             appendPromptFile,
				MaxTurnsPerSession:                 maxTurns,
				Effort:                             tuning.Effort,
				FallbackModel:                      tuning.FallbackModel,
				ExcludeDynamicSystemPromptSections: tuning.ExcludeDynamicSystemPromptSections,
				StrictMcpConfig:                    tuning.StrictMcpConfig,
				ResumeSessionID:                    tuning.ResumeClaudeSessionID,
				ForkSession:                        tuning.ForkResumedSession,
				FromPRNumber:                       req.PRNumber,
				ChromeEnabled:                      req.BrowserEnabled,
				Channels:                           req.Channels,
			},
		)
		if err != nil {
			slog.Error("Failed to create SDK session, falling back to PTY", "name", member.Name, "error", err)
		} else {
			// Start event forwarding before broadcast to avoid losing early events
			go forwardSDKSessionEvents(sdkSession, ctx)
			ctx.SSEBroker.Broadcast("session:created", map[string]interface{}{
				"id": sdkSession.ID, "name": member.Name, "workingDir": req.WorkingDir,
				"mode": "claude-sdk", "status": "idle", "createdAt": sdkSession.CreatedAt.UnixMilli(),
			})
			return sdkSession.ID, "sdk"
		}
	}

	// PTY fallback
	// Ensure agent token is present so `cm` calls from the shell can
	// authenticate. See the SDK path for the full reasoning.
	ptyEnv := make(map[string]string, len(envVars)+1)
	for k, v := range envVars {
		ptyEnv[k] = v
	}
	if ctx.AgentTokenMgr != nil {
		if tok := ctx.AgentTokenMgr.Token(); tok != "" {
			ptyEnv["JARVIS_AGENT_TOKEN"] = tok
		}
	}
	tuning := resolveSessionTuning(req, member.Tier, ov)
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:                         req.WorkingDir,
			Mode:                               pty.ModeClaude,
			Name:                               member.Name,
			ProviderEnv:                        ptyEnv,
			Effort:                             tuning.Effort,
			FallbackModel:                      tuning.FallbackModel,
			ExcludeDynamicSystemPromptSections: tuning.ExcludeDynamicSystemPromptSections,
			StrictMcpConfig:                    tuning.StrictMcpConfig,
			FromPRNumber:                       req.PRNumber,
			ChromeEnabled:                      req.BrowserEnabled,
			Channels:                           req.Channels,
		},
	)
	if err != nil {
		slog.Error("Failed to create PTY session", "name", member.Name, "error", err)
		return "", ""
	}
	go forwardSessionEvents(session, ctx)
	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))
	return session.ID, "pty"
}

// spawnAgentSessionWithHooks is the office spawn path for
// officeSessionMode = "hooks". Same shape as the PTY fallback in
// spawnAgentSession, but with three differences:
//  1. ID is preassigned (so the hook URLs in the per-agent
//     .claude/settings.local.json -- written before this call by
//     SetupWorktreesWithHooks -- match the agent we're about to
//     register.
//  2. WorkingDir is the per-agent worktree path, not the team's
//     shared workingDir.
//  3. HooksEnabled flag flows through SessionConfig so the office
//     payload builder can render the "via hooks" badge and so the
//     SDK forwarder bypasses tear-down for hooks-driven AgentState
//     updates.
func spawnAgentSessionWithHooks(ctx *Context, member teamtemplates.TeamTemplateMember, req spawnTeamRequest, envVars map[string]string, spec spawnAgentSpec) (string, string) {
	ptyEnv := make(map[string]string, len(envVars)+1)
	for k, v := range envVars {
		ptyEnv[k] = v
	}
	if ctx.AgentTokenMgr != nil {
		if tok := ctx.AgentTokenMgr.Token(); tok != "" {
			ptyEnv["JARVIS_AGENT_TOKEN"] = tok
		}
	}
	if spec.PreassignedSessionID != "" {
		ptyEnv["JARVIS_SESSION_ID"] = spec.PreassignedSessionID
	}

	cwd := spec.PerAgentCwd
	if cwd == "" {
		cwd = req.WorkingDir
	}
	tuning := resolveSessionTuning(req, member.Tier, spec.Override)
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			ID:                                 spec.PreassignedSessionID, // empty -> uuid; non-empty -> we use it
			WorkingDir:                         cwd,
			Mode:                               pty.ModeClaude,
			Name:                               member.Name,
			ProviderEnv:                        ptyEnv,
			HooksEnabled:                       true,
			Effort:                             tuning.Effort,
			FallbackModel:                      tuning.FallbackModel,
			ExcludeDynamicSystemPromptSections: tuning.ExcludeDynamicSystemPromptSections,
			StrictMcpConfig:                    tuning.StrictMcpConfig,
			FromPRNumber:                       req.PRNumber,
			ChromeEnabled:                      req.BrowserEnabled,
			Channels:                           req.Channels,
		},
	)
	if err != nil {
		slog.Error("Failed to create hooks-mode PTY session", "name", member.Name, "error", err)
		return "", ""
	}
	go forwardSessionEvents(session, ctx)
	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))
	return session.ID, "hooks"
}

// buildOfficeMCPServers builds the per-agent MCP server map shared by the
// Claude SDK and Codex spawn paths so both backends expose the SAME tool
// surface (jarvis-mcp, codegraph, context7, sequential-thinking, shadcn,
// pageindex). Returns the Claude-shaped `map[string]any`
// ({type,command,args,env}); the Codex path translates it via
// codexMCPFromMap. sessionID + agentToken are threaded into the jarvis-mcp
// env so `cm` / jarvis-mcp authenticate as this agent.
func (ctx *Context) buildOfficeMCPServers(req spawnTeamRequest, sessionID, agentToken string) map[string]any {
	mcpJarvisEnv := map[string]string{
		"JARVIS_API_URL":    "http://localhost:3555",
		"JARVIS_SESSION_ID": sessionID,
	}
	if agentToken != "" {
		mcpJarvisEnv["JARVIS_AGENT_TOKEN"] = agentToken
	}
	mcpServers := map[string]any{
		"jarvis": map[string]any{
			"type":    "stdio",
			"command": "jarvis-mcp",
			"env":     mcpJarvisEnv,
		},
		"codegraphcontext": map[string]any{
			"type":    "stdio",
			"command": "cgc",
			"args":    []string{"mcp", "start"},
		},
	}
	// P6.3 · codegraph MCP. Gated on (a) the binary being on PATH
	// and (b) the operator not opting out via DisableCodegraph.
	// The actual .codegraph/ index is initialised once per
	// workingDir by spawnTeamFromRequest before this loop runs
	// (background goroutine; doesn't block spawn).
	if !codegraphOptOut(req) && office.CodegraphBinaryAvailable() {
		mcpServers["codegraph"] = map[string]any{
			"type":    "stdio",
			"command": "codegraph",
			"args":    []string{"serve", "--mcp", "--path", req.WorkingDir},
		}
	}
	// P10.4 · ECC MCPs (Context7 + Sequential Thinking). Both run
	// via `npx -y <package>@<version>` so we additionally gate on
	// NpxAvailable() — a missing Node toolchain silently drops the
	// entries instead of poisoning the spawn with a dead MCP that
	// would fail mid-handshake. Disable via spawn request's
	// disableEcc field (same posture as disableCodegraph).
	//
	// P11.1.1 · Versions are pinned to prevent supply-chain
	// auto-upgrade if either package is compromised on the npm
	// registry. Refresh workflow: run
	//   npm view @upstash/context7-mcp version
	//   npm view @modelcontextprotocol/server-sequential-thinking version
	//   npm view @jpisnice/shadcn-ui-mcp-server version
	// to find the current latest, update all pins below, regenerate
	// skills/ecc/CHECKSUMS.sha256, and document the bump in
	// docs/ECC.md. Last refreshed: 2026-05-24.
	const (
		context7Version           = "3.0.0"
		sequentialThinkingVersion = "2025.12.18"
		shadcnUIVersion           = "2.0.0" // P13.1
	)
	if !eccOptOut(req) && office.NpxAvailable() {
		mcpServers["context7"] = map[string]any{
			"type":    "stdio",
			"command": "npx",
			"args":    []string{"-y", "@upstash/context7-mcp@" + context7Version},
		}
		mcpServers["sequential-thinking"] = map[string]any{
			"type":    "stdio",
			"command": "npx",
			"args":    []string{"-y", "@modelcontextprotocol/server-sequential-thinking@" + sequentialThinkingVersion},
		}
		// P13.1 · shadcn-ui MCP. Metadata-only — gives the agent
		// list/search/view tools against the shadcn registries plus
		// install commands + accessibility audit checklists. Useful
		// any time an agent is touching UI work (web dashboard
		// redesign, future React island, mobile RN inspiration).
		mcpServers["shadcn"] = map[string]any{
			"type":    "stdio",
			"command": "npx",
			"args":    []string{"-y", "@jpisnice/shadcn-ui-mcp-server@" + shadcnUIVersion},
		}
	}
	// Knowledge library MCP (PageIndex). Gives every agent
	// mcp__pageindex__get_document_structure +
	// mcp__pageindex__get_page_content tools pointed at the
	// per-jarvis markdown library that captures past experiences,
	// workflows, standups, and wrap-up reports. Agents can
	// navigate prior runs via LLM-driven tree search instead of
	// reinventing approaches every time.
	//
	// Gated on PageIndexAvailable() (which itself gates on
	// NpxAvailable + a one-shot @pageindex/mcp warm-cache probe).
	// Library dir comes from office.LibraryDir() which sets up
	// ~/.jarvis/office-memory/library/ on first call.
	if office.PageIndexAvailable() {
		if cfg := office.PageIndexMcpConfig(office.LibraryDir()); cfg != nil {
			mcpServers["pageindex"] = cfg
		}
	}
	return mcpServers
}

// codexMCPFromMap translates the Claude-shaped MCP map (built by
// buildOfficeMCPServers) into the codexcfg.MCPServer shape used to render
// [mcp_servers.<name>] TOML blocks. Only stdio entries with a command are
// carried over (Codex stdio servers); unknown shapes are skipped.
func codexMCPFromMap(in map[string]any) map[string]codexcfg.MCPServer {
	out := make(map[string]codexcfg.MCPServer, len(in))
	for name, raw := range in {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		command, _ := entry["command"].(string)
		if command == "" {
			continue
		}
		srv := codexcfg.MCPServer{Command: command}
		if args, ok := entry["args"].([]string); ok {
			srv.Args = args
		}
		if env, ok := entry["env"].(map[string]string); ok {
			srv.Env = env
		}
		out[name] = srv
	}
	return out
}

// spawnCodexAgentSession is the Codex-harness spawn path (ModeCodexExec). It
// writes a per-agent CODEX_HOME/config.toml (provider + model + sandbox + MCP),
// builds the Codex-flavored system prompt, starts a CodexSession, and wires its
// events to SSE. Returns (sessionID, "codex") or ("", "") on failure.
func (ctx *Context) spawnCodexAgentSession(member teamtemplates.TeamTemplateMember, req spawnTeamRequest, envVars map[string]string, teamConfigs []*office.TeamMemberConfig, perpetual bool, ov *memberOverride, provider, model string) (string, string) {
	sessionID := uuid.New().String()
	agentToken := ""
	if ctx.AgentTokenMgr != nil {
		agentToken = ctx.AgentTokenMgr.Token()
	}

	// Codex-flavored system prompt: stable prefix (no /goal, /compact, Dynamic
	// Workflows) + the provider-agnostic per-agent kickstart, folded into one
	// string (Codex has no separate append-system-prompt channel).
	agentMemory := office.LoadAgentMemory(member.ID, req.WorkingDir)
	kickstart := office.BuildKickstartPrompt(
		&office.TeamMemberConfig{ID: member.ID, Name: member.Name, Role: member.Role, Tier: member.Tier, Color: member.Color},
		req.WorkingDir, teamConfigs, req.Mission, agentMemory, perpetual,
	)
	stablePrefix := office.BuildStableSystemPromptCodex(member.Tier, req.Mission)
	systemPrompt := stablePrefix + "\n\n" + kickstart

	// Resolve model (caller passes the resolved per-member model; default to
	// the flagship if somehow empty) and reasoning effort.
	if model == "" {
		model = "gpt-5.5"
	}
	tuning := resolveSessionTuning(req, member.Tier, ov)

	// Provider block. The Phase-0 spike proved that even api.openai.com needs
	// a custom [model_providers] block with env_key + requires_openai_auth=false
	// to Bearer-auth in headless `codex exec`. So whenever we have an API key
	// (operator setting or the shared deployment key), emit the custom block.
	// Only openai-codex with NO key falls back to the built-in `openai`
	// provider (ChatGPT-subscription auth via `codex login`).
	settings := ctx.StateStore.GetSettings()
	hasKey := envVars["OPENAI_API_KEY"] != ""
	var prov *codexcfg.Provider
	switch provider {
	case "openai-custom":
		baseURL, _ := settings["openaiBaseUrl"].(string)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		prov = &codexcfg.Provider{
			ID:                 "jarvis-openai",
			Name:               "Jarvis OpenAI",
			BaseURL:            baseURL,
			EnvKey:             "OPENAI_API_KEY",
			WireAPI:            "responses",
			RequiresOpenAIAuth: false,
		}
	default: // openai-codex
		if hasKey {
			prov = &codexcfg.Provider{
				ID:                 "jarvis-openai",
				Name:               "OpenAI",
				BaseURL:            "https://api.openai.com/v1",
				EnvKey:             "OPENAI_API_KEY",
				WireAPI:            "responses",
				RequiresOpenAIAuth: false,
			}
		}
		// else: nil provider -> built-in openai (codex login / auth.json)
	}

	// Per-agent CODEX_HOME under ~/.jarvis/codex/<sessionID>/ (isolates the
	// config.toml + rollout logs from other agents and the operator's own
	// ~/.codex). Written before the session starts.
	home, herr := os.UserHomeDir()
	if herr != nil || home == "" {
		home = req.WorkingDir
	}
	codexHome := filepath.Join(home, ".jarvis", "codex", sessionID)

	cfg := codexcfg.Config{
		Model:           model,
		ReasoningEffort: codexcfg.MapReasoningEffort(tuning.Effort),
		SandboxMode:     "danger-full-access",
		ApprovalPolicy:  "never",
		Provider:        prov,
		MCPServers:      codexMCPFromMap(ctx.buildOfficeMCPServers(req, sessionID, agentToken)),
	}
	if _, werr := codexcfg.Write(codexHome, cfg); werr != nil {
		slog.Error("Failed to write Codex config.toml", "name", member.Name, "error", werr)
		return "", ""
	}

	// Subprocess env: provider env (OPENAI_API_KEY/CODEX_API_KEY) + jarvis vars
	// so `cm` works + CODEX_HOME (also set by the driver, belt-and-suspenders).
	agentEnv := make(map[string]string, len(envVars)+4)
	for k, v := range envVars {
		agentEnv[k] = v
	}
	agentEnv["JARVIS_API_URL"] = "http://localhost:3555"
	agentEnv["JARVIS_SESSION_ID"] = sessionID
	if agentToken != "" {
		agentEnv["JARVIS_AGENT_TOKEN"] = agentToken
	}
	agentEnv["CODEX_HOME"] = codexHome

	maxTurns := defaultMaxTurnsPerSession
	if ov != nil && ov.MaxTurns > 0 {
		maxTurns = ov.MaxTurns
	}

	codexSession, err := ctx.SessionManager.CreateCodexSession(
		context.Background(),
		pty.SessionConfig{
			ID:                 sessionID,
			WorkingDir:         req.WorkingDir,
			Mode:               pty.ModeCodexExec,
			Name:               member.Name,
			ProviderEnv:        agentEnv,
			SystemPrompt:       systemPrompt,
			Model:              model,
			Effort:             tuning.Effort,
			MaxTurnsPerSession: maxTurns,
			CodexHome:          codexHome,
		},
	)
	if err != nil {
		slog.Error("Failed to create Codex session", "name", member.Name, "error", err)
		return "", ""
	}
	go forwardCodexSessionEvents(codexSession, ctx)
	ctx.SSEBroker.Broadcast("session:created", map[string]interface{}{
		"id": codexSession.ID, "name": member.Name, "workingDir": req.WorkingDir,
		"mode": "codex-exec", "status": "idle", "createdAt": codexSession.CreatedAt.UnixMilli(),
	})
	return codexSession.ID, "codex"
}
