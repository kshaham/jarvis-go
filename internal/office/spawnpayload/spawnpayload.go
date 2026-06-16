// Package spawnpayload holds the request / overlay types and the small
// helpers (CodegraphOptOut, EccOptOut, ResolveSessionTuning,
// DefaultEffortFor) that describe a team-spawn intent. Both
// internal/web/handler and internal/office can import this package
// without creating a cycle, which is the load-bearing reason it
// exists.
//
// Migration plan:
//
//  1. Types live here (this file). Handler keeps thin type-aliases
//     (memberOverride = spawnpayload.MemberOverride etc.) so existing
//     handler files don't all churn at once.
//
//  2. A future internal/office/spawn.go defines an Orchestrator type
//     that consumes a spawnpayload.SpawnTeamRequest and does the
//     actual spawn work currently in handler.spawnTeamFromRequest.
//
//  3. handler.spawnTeamFromRequest shrinks to: decode body into
//     SpawnTeamRequest → call orchestrator.SpawnTeam → write
//     response.
//
// The neutral package name avoids the temptation to put HTTP-shape
// concerns (status codes, header parsing) here. Anything HTTP belongs
// in handler; anything spawn-logic belongs in office; the types that
// describe a spawn intent belong here.
package spawnpayload

// MemberOverride is the per-member overlay an operator can attach
// to a spawn request — extends or replaces fields from the chosen
// template's member entry.
type MemberOverride struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Name         string   `json:"name,omitempty"`
	Role         string   `json:"role,omitempty"`
	Tier         string   `json:"tier,omitempty"`
	Color        string   `json:"color,omitempty"`
	Emoji        string   `json:"emoji,omitempty"`
	Personality  string   `json:"personality,omitempty"`
	Voice        string   `json:"voice,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	WorkingDir   string   `json:"workingDir,omitempty"`

	// Per-member turn cap from the spawn modal. 0 means "use the
	// team-wide default" (defaultMaxTurnsPerSession). The MaxCostUSD
	// counterpart was removed — subscription-plan users have no
	// meaningful dollar signal.
	MaxTurns int `json:"maxTurns,omitempty"`

	// Operator-controlled stagger. When > 0, the outer spawn loop
	// sleeps this many ms AFTER creating this member's session before
	// moving to the next. Useful to avoid auth bottlenecks (Claude
	// subscription rate limits) when spawning many agents at once.
	// 0 = no delay (default).
	SpawnDelayMs int `json:"spawnDelayMs,omitempty"`

	// Per-member Claude Code tuning. Empty / zero defers to the
	// team-wide default in SpawnTeamRequest, which itself falls back
	// to DefaultEffortWorker / DefaultEffortStrategic. Dollar-cap
	// budgets were intentionally removed because jarvis runs against
	// subscription-billed providers where they're meaningless.
	Effort        string `json:"effort,omitempty"`
	FallbackModel string `json:"fallbackModel,omitempty"`

	// When set, the new SDK session resumes this Claude session ID
	// via --resume <id>. Snapshot-restore in fork mode populates
	// these from the snapshot's per-member ClaudeSessionID; routine
	// spawns leave it empty for a fresh transcript.
	ResumeClaudeSessionID string `json:"resumeClaudeSessionId,omitempty"`
	ForkResumedSession    bool   `json:"forkResumedSession,omitempty"`
}

// SpawnTeamRequest is the operator's complete intent for a new team:
// which template, what overrides, what tuning, what integrations.
type SpawnTeamRequest struct {
	WorkingDir          string           `json:"workingDir"`
	TemplateID          string           `json:"templateId"`
	Mission             string           `json:"mission"`
	MemberOverrides     []MemberOverride `json:"memberOverrides"`
	PerpetualMode       *bool            `json:"perpetualMode"` // nil = default true
	TelegramEnabled     bool             `json:"telegramEnabled"`
	TelegramIntervalMin int              `json:"telegramIntervalMin"`

	// NewProject + ProjectName together signal that the operator
	// wants to spawn into a freshly-created /app/workspace/<ProjectName>
	// directory seeded with a CLAUDE.md. Mutually exclusive with a
	// pre-populated WorkingDir; when NewProject is true, WorkingDir
	// is computed server-side and the caller's value is ignored.
	NewProject  bool   `json:"newProject"`
	ProjectName string `json:"projectName"`

	// GithubRepo, when non-empty, signals the operator wants to spawn
	// against a GitHub repository. The handler clones it into
	// /app/workspace/<owner>-<repo> (or pulls if present) and uses that
	// as WorkingDir. Mutually exclusive with NewProject and a pre-filled
	// WorkingDir. Accepts "owner/repo" or a github.com URL.
	GithubRepo string `json:"githubRepo,omitempty"`

	// Team-wide Claude Code defaults. Each member can override
	// individually via MemberOverride; if both are blank the
	// DefaultEffortWorker / DefaultEffortStrategic constants apply.
	DefaultEffort                      string `json:"defaultEffort,omitempty"`
	DefaultFallbackModel               string `json:"defaultFallbackModel,omitempty"`
	ExcludeDynamicSystemPromptSections *bool  `json:"excludeDynamicSystemPromptSections,omitempty"`
	StrictMcpConfig                    *bool  `json:"strictMcpConfig,omitempty"`

	// Optional GitHub PR linkage. Non-zero forwards --from-pr <n>
	// to Claude (so the agent sees PR context in its system prompt)
	// and records the linkage in OfficeManager.teamPRNumber so
	// external tooling can map a PR back to its live team via
	// GET /api/office/pr/<n>. Zero = no PR linkage (default).
	PRNumber int `json:"prNumber,omitempty"`

	// Opt-in browser-automation tooling for the whole team. When
	// true, every spawned member gets Claude's --chrome flag
	// (navigate, click, screenshot, evaluate). Off by default
	// because it widens the agent's reach beyond the worktree.
	// Per-team (not per-member) because the browser session needs
	// to be reachable from every agent that might want to verify
	// a UI change.
	BrowserEnabled bool `json:"browserEnabled,omitempty"`

	// Opt-in MCP channel subscriptions (research preview). Each
	// entry becomes a --channels <spec> arg on the spawn. Empty =
	// no subscriptions (default). Operators only — surfaced under
	// "Advanced" in the spawn modal because the spec format is
	// owned by Claude Code's preview API and may change.
	Channels []string `json:"channels,omitempty"`

	// Per-team kill switch for Claude Code Dynamic Workflows (the
	// capability where an agent fans out up to ~1,000 subagents, ~16
	// concurrent). Default false = workflows ON (Claude Code's default
	// on paid plans), so a lead can orchestrate a workflow on-demand.
	// Set true to inject CLAUDE_CODE_DISABLE_WORKFLOWS=1 into every
	// member's session — a guardrail, since agents run permission-
	// bypassed and would otherwise self-launch large runs with no human
	// gate. The per-session cost cap remains the spend backstop.
	DisableWorkflows bool `json:"disableWorkflows,omitempty"`

	// Per-team escape hatch for the codegraph MCP wiring. Default
	// behaviour (nil) = codegraph on if the binary is on PATH,
	// mirroring jarvis-mcp + codegraphcontext (always on). Set true
	// to skip both the MCP server entry AND the index init
	// goroutine — useful for a team that won't benefit (very small
	// workspace, non-code work, etc).
	DisableCodegraph *bool `json:"disableCodegraph,omitempty"`

	// Per-team escape hatch for the vendored ECC bundle (skill
	// recommender hints, the 4 ECC Task-tool agents, the 3 ECC
	// slash commands, and the context7 + sequential-thinking +
	// shadcn MCP servers). Default (nil) = ECC on, mirroring the
	// codegraph pattern. Set true to ship only jarvis-native
	// content for the team.
	DisableEcc *bool `json:"disableEcc,omitempty"`

	// Per-team escape hatch for the Headroom context-compression MCP
	// (mcp__headroom__headroom_compress / _retrieve / _stats). Default
	// (nil) = Headroom on if the binary is on PATH, mirroring the
	// codegraph/pageindex pattern. Set true to skip the MCP entry for a
	// team that won't benefit.
	DisableHeadroom *bool `json:"disableHeadroom,omitempty"`

	// SevenStepMode enables the additive 7-step engineering discipline overlay
	// (Addy Osmani's agent-skills SDLC: DEFINE/PLAN/BUILD/VERIFY/REVIEW/SIMPLIFY/
	// SHIP). Default (nil) = OFF -- it's opt-in, unlike perpetual mode. When on,
	// agents get the /spec.. /ship slash commands + personas and a kickstart
	// section driving each task through the 7 phases, combinable with perpetual
	// or goal-focused mode.
	SevenStepMode *bool `json:"sevenStepMode,omitempty"`
}

// SessionTuning is the resolved bundle of Claude Code knobs that
// flows onto pty.SessionConfig. Built by ResolveSessionTuning by
// layering jarvis defaults < team-wide overrides < per-member
// overrides.
type SessionTuning struct {
	Effort                             string
	FallbackModel                      string
	ExcludeDynamicSystemPromptSections bool
	StrictMcpConfig                    bool

	// Resume + fork passed through from a per-member override.
	// Only populated by the snapshot-restore "fork" path; routine
	// spawns leave both zero.
	ResumeClaudeSessionID string
	ForkResumedSession    bool
}

// Defaults applied when neither the team nor the per-member override
// sets a value.
const (
	DefaultEffortWorker    = "medium"
	DefaultEffortStrategic = "high"
)

// CodegraphOptOut reports whether the team explicitly disabled
// codegraph at spawn time. Centralised so both the MCP map and the
// index-init call site agree on the decision.
func CodegraphOptOut(req SpawnTeamRequest) bool {
	return req.DisableCodegraph != nil && *req.DisableCodegraph
}

// EccOptOut reports whether the team explicitly disabled the ECC
// bundle at spawn time. Mirrors CodegraphOptOut. Centralised so the
// agent generator, command generator, MCP map, and skills
// recommender all converge on the same decision.
func EccOptOut(req SpawnTeamRequest) bool {
	return req.DisableEcc != nil && *req.DisableEcc
}

// HeadroomOptOut reports whether the team explicitly disabled the
// Headroom compression MCP at spawn time. Mirrors CodegraphOptOut.
func HeadroomOptOut(req SpawnTeamRequest) bool {
	return req.DisableHeadroom != nil && *req.DisableHeadroom
}

// DefaultEffortFor returns the floor effort tier for a member based
// on its tier string ("strategic" → high, anything else → medium).
func DefaultEffortFor(tier string) string {
	if tier == "strategic" {
		return DefaultEffortStrategic
	}
	return DefaultEffortWorker
}

// ResolveSessionTuning produces the per-session Claude Code knobs
// by layering (low precedence → high): jarvis defaults < team-wide
// (SpawnTeamRequest) < per-member override. Centralised so every
// spawn call site shares one source of truth. Takes a value-receiver
// req (call sites have it on the stack) and a member tier string
// so the helper doesn't need to import the specific member type.
func ResolveSessionTuning(req SpawnTeamRequest, memberTier string, ov *MemberOverride) SessionTuning {
	t := SessionTuning{
		Effort:                             DefaultEffortFor(memberTier),
		FallbackModel:                      "",
		ExcludeDynamicSystemPromptSections: true,
		StrictMcpConfig:                    true,
	}
	// Team-wide overrides.
	if req.DefaultEffort != "" {
		t.Effort = req.DefaultEffort
	}
	if req.DefaultFallbackModel != "" {
		t.FallbackModel = req.DefaultFallbackModel
	}
	if req.ExcludeDynamicSystemPromptSections != nil {
		t.ExcludeDynamicSystemPromptSections = *req.ExcludeDynamicSystemPromptSections
	}
	if req.StrictMcpConfig != nil {
		t.StrictMcpConfig = *req.StrictMcpConfig
	}
	// Per-member overrides win.
	if ov != nil {
		if ov.Effort != "" {
			t.Effort = ov.Effort
		}
		if ov.FallbackModel != "" {
			t.FallbackModel = ov.FallbackModel
		}
		// Resume/fork is strictly per-member: each agent has its own
		// Claude session ID and may have been alive longer or shorter
		// than its peers.
		t.ResumeClaudeSessionID = ov.ResumeClaudeSessionID
		t.ForkResumedSession = ov.ForkResumedSession
	}
	return t
}
