package handler

// Spawn-agent helpers extracted from office.go to keep the latter
// scannable. No behavior change.

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
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
func spawnAgentSession(ctx *Context, member teamtemplates.TeamTemplateMember, req spawnTeamRequest, envVars map[string]string, teamConfigs []*office.TeamMemberConfig, perpetual bool, trySDK bool, ov *memberOverride, spec spawnAgentSpec) (string, string) {
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
