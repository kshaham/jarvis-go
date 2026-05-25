package office

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/claudeman/jarvis-go/internal/library"
)

// memoryInjectMaxChars caps how much per-agent memory we inject into the
// kickstart prompt. Anything past this is replaced with a pointer to the
// `cm memory` commands so the agent can pull what they need on demand.
const memoryInjectMaxChars = 1500

// BuildKickstartPrompt generates the per-agent kickstart -- the turn-1
// prompt sent via Session.Send(). Keep it lean: identity, project
// context, previous-work memory, learnings, teammate insights,
// personality, role. Stable cross-agent boilerplate (protocols,
// browser, dev-instance, context-budget) is NOT here -- it lives in
// BuildStableSystemPrompt which goes through claude.WithAppendSystemPrompt
// so Anthropic's ephemeral cache can discount it 90% on reuse.
//
// R31 · Per-role trim: worker tier skips the leader-task-assignment
// and leader-dev-instance blocks entirely; those stay reachable via
// `cm onboard dev-lead` for the few leaders who need them.
func BuildKickstartPrompt(config *TeamMemberConfig, workingDir string, teammates []*TeamMemberConfig, teamMission string, agentMemory string, perpetualMode bool) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# %s - %s\n\n", config.Name, config.Role)

	// Team mission -- duplicated briefly here even though the stable
	// prefix also carries it, because the kickstart often gets replayed
	// in isolation (debug tooling, session clone) and the mission is
	// the single highest-signal context line.
	if teamMission != "" {
		b.WriteString("## Team Mission\n")
		b.WriteString(teamMission)
		b.WriteString("\n\nAlign your work with this mission. Look for ways to go above and beyond.\n\n")
	}

	// Project context (CLAUDE.md / README.md / ARCHITECTURE.md). Cached
	// per-workingDir so a team spawn only pays the file I/O once.
	if projectCtx := BuildProjectContext(workingDir); projectCtx != "" {
		b.WriteString("## Project Context\n")
		b.WriteString(projectCtx)
		b.WriteString("\n\n")
	}

	// Previous-work memory (capped + deduplicated)
	if agentMemory != "" {
		dedupedMemory := deduplicateMemory(agentMemory)
		b.WriteString("## Previous Work Memory\n")
		if len(dedupedMemory) > memoryInjectMaxChars {
			b.WriteString(dedupedMemory[:memoryInjectMaxChars])
			b.WriteString("\n... (truncated -- `cm onboard memory` for commands to pull the rest)\n\n")
		} else {
			b.WriteString(dedupedMemory)
			b.WriteString("\n\n")
		}
	}

	// Distilled learnings
	if learnings := BuildLearningsPromptSection(config.ID); learnings != "" {
		b.WriteString("## Learned Knowledge\n")
		b.WriteString(learnings)
		b.WriteString("\n")
	}

	// Knowledge-library hints — top-3 highest-outcome past
	// experiences relevant to this role + working directory. Pulled
	// from ~/.jarvis/office-memory/library/ (the corpus PageIndex
	// MCP also indexes for on-demand retrieval). This eager-inject
	// path guarantees high-value hits land in the kickstart even
	// when the agent doesn't think to query the MCP itself.
	if hints := BuildLibraryHintsSection(config, workingDir); hints != "" {
		b.WriteString("## What Worked Before\n")
		b.WriteString(hints)
		b.WriteString("\n")
	}

	// Teammate findings (compact)
	if cross := BuildCrossAgentContextCompact(config.ID); cross != "" {
		b.WriteString("## Teammate Insights\n")
		b.WriteString(cross)
		b.WriteString("\n")
	}

	// Identity
	b.WriteString("## Identity\n")
	fmt.Fprintf(&b, "You are %s, the %s on an autonomous AI development team in a Jarvis session.\n", config.Name, config.Role)
	fmt.Fprintf(&b, "Working directory: %s\n\n", workingDir)

	// Personality
	if config.Personality != "" {
		b.WriteString("## Personality\n")
		b.WriteString(config.Personality)
		b.WriteString("\n\n")
	}

	// Team relationships -- short. Full roster and messaging help moves
	// to the stable prefix.
	team := filterTeammates(config.ID, teammates)
	if len(team) > 0 {
		b.WriteString("## Team\n")
		fmt.Fprintf(&b, "You work with %d teammates:\n", len(team))
		for _, tm := range team {
			fmt.Fprintf(&b, "- %s (%s)\n", tm.Name, tm.Role)
		}
		b.WriteString("\n")
	}

	// Leader-specific (only strategic-tier agents)
	if config.Tier == "strategic" {
		b.WriteString("## Leader Task Assignment\n")
		b.WriteString("After standup you receive a `[LEADER TASK ASSIGNMENT]` prompt. Analyze each member's report, create 2-4 actionable tasks per teammate, respond with `cm leader-tasks respond \"TASKS_FOR:<id>\\n- [HIGH] title | description\\n...\"`. Route tasks to the right teammate by role.\n")
		if len(team) > 0 {
			b.WriteString("Routing:\n")
			for _, tm := range team {
				fmt.Fprintf(&b, "- %s tasks -> %s (%s)\n", tm.Role, tm.Name, tm.ID)
			}
		}
		b.WriteString("\nFor the full dev-instance startup runbook (web apps, Expo, fullstack slots): `cm onboard dev-lead`.\n\n")
	}

	// Work mode
	if perpetualMode {
		b.WriteString("## Work Mode: RALPH (perpetual)\n")
		b.WriteString("You must NEVER be idle. After completing any task: `cm task list` -> `cm skill search <topic>` -> `cm reflect \"<lesson>\"` -> `cm task self-add ...` -> start the next task.\n")
		b.WriteString("If you have no tasks: hunt TODOs, write missing tests, refactor for clarity, or improve docs. Avoid duplicate work -- check `git log --oneline -20` and `cm roster` first.\n\n")
	} else {
		b.WriteString("## Work Mode: Goal-Focused\n")
		b.WriteString("Complete assigned tasks thoroughly. When done, run `cm task list` to verify, review your work, commit, then wait for the next standup. Do not invent work beyond the mission scope.\n\n")
	}

	// Role (SystemPrompt) -- last, highest-signal, role-unique.
	b.WriteString("## Your Role\n")
	b.WriteString(config.SystemPrompt)

	return prependBudgetWarning(b.String(), config.Name)
}

// BuildStableSystemPrompt returns the cacheable prefix that gets
// handed to claude.WithAppendSystemPrompt. Content identical across
// every agent on a team + stable across respawns within a team:
// onboarding router + critical rules + team mission. Anthropic's
// ephemeral cache auto-breakpoints on system prompts above ~1k
// tokens and charges 10% on cache hits (5-min TTL), so the 2nd agent
// through 5th agent on a single spawn pay ~10% of this block.
//
// Tier-aware: strategic agents get the dev-lead topic listed in the
// router; workers don't.
func BuildStableSystemPrompt(tier, teamMission string) string {
	var b strings.Builder
	b.WriteString("# Jarvis Team Operating Context\n\n")
	b.WriteString("You are part of an autonomous multi-agent team coordinated by Jarvis. The sections below are shared across every teammate and every turn -- they do not change mid-session. Anthropic's prompt cache discounts this prefix on subsequent turns, so its size is not a per-turn concern.\n\n")

	if teamMission != "" {
		b.WriteString("## Team Mission\n")
		b.WriteString(teamMission)
		b.WriteString("\n\n")
	}

	b.WriteString("## Lazy-load Reference\n")
	b.WriteString(onboardingRouterLines(tier))
	b.WriteString("\nThe kickstart you receive after this prompt is lean on purpose. For any protocol, tool, or dev-instance question that isn't covered inline, run the matching `cm onboard` command -- it prints the full reference so you only pay for the tokens you actually use.\n\n")

	b.WriteString("## Critical Rules\n")
	b.WriteString("- Task lifecycle: `cm task ack <id>` -> `cm task start <id>` -> `cm task progress <id> \"...\"` -> `cm task complete <id> --summary \"<2-3 sentence handoff>\"`. Never skip ack or start.\n")
	b.WriteString("- Handoff mid-task: `cm task handoff <id> <targetConfigId> \"<context>\"`. Receiver accepts/rejects. Not for dodging work.\n")
	b.WriteString("- Never push to remote, never switch branches -- you live on `agent/<your-id>`.\n")
	b.WriteString("- Never claim completion without fresh verification (build, test, screenshot, exit code).\n")
	b.WriteString("- Before duplicating work: `cm knowledge search <keywords>` -- searches every teammate's memory + learnings + standups + whiteboard.\n")
	b.WriteString("- Before any browser verification: `cm dev url` (and `cm dev creds` if login-gated). Don't spin up competing dev servers.\n")
	b.WriteString("- Keep memory fresh: `cm memory section <name> \"<content>\"` / `cm memory append \"<entry>\"`. At ~70% context run `/compact` after saving state to memory.\n")
	// R37 · Self-sustaining task primitives. Lives in the cached prefix
	// so every agent sees the two escape hatches without paying for the
	// full skill bodies on every turn.
	b.WriteString("- For multi-step tasks, wrap your work in `/goal <criteria>` so Claude Code's evaluator decides when you're done. If you get stuck, ask the team: `cm ask \"<question>\" --about <taskId> --wait 5m` (lead-first, fallback teammate, blocks until answered or timeout). Read `cm skill read office-goal-loop` and `cm skill read office-ask-for-help` once.\n\n")

	b.WriteString("## Team Messaging\n")
	b.WriteString("Team messages arrive as `[Team Message from <name>]: <content>`. Reply with `cm message <sessionId> \"...\"`. `cm roster` lists sessionIds.\n")

	return b.String()
}

// estimatedTokens gives a rough token count for budget estimation. Claude's
// tokenizer averages ~3.8 chars per token for English prose; /4 slightly
// underestimates but is accurate enough for an advisory warning threshold.
func estimatedTokens(s string) int {
	return len(s) / 4
}

// budgetWarnThresholdTokens is the point at which we prepend a warning that
// the kickstart is consuming a significant share of the context window.
// Claude's window is ~200k tokens; 50k (25%) is aggressive territory.
const budgetWarnThresholdTokens = 50_000

// prependBudgetWarning adds an advisory header when the kickstart is large
// enough to meaningfully eat into the agent's context window. Informational
// only -- does not truncate.
func prependBudgetWarning(prompt, agentName string) string {
	tokens := estimatedTokens(prompt)
	if tokens < budgetWarnThresholdTokens {
		slog.Debug("kickstart built", "agent", agentName, "estTokens", tokens)
		return prompt
	}
	pct := tokens * 100 / 200_000
	slog.Warn("large kickstart prompt", "agent", agentName, "estTokens", tokens, "percentOfWindow", pct)
	warning := fmt.Sprintf("[NOTICE] Your kickstart is ~%d tokens (~%d%% of Claude's 200k context). Plan to `/compact` earlier than the usual 70%% threshold.\n\n", tokens, pct)
	return warning + prompt
}

// deduplicateMemory drops repeated bullet / line entries so a memory file
// that accumulated "remember to run tests" five times only shows up once in
// the kickstart. Normalizes whitespace + lowercases for comparison; preserves
// original formatting for the lines it keeps. First occurrence wins.
func deduplicateMemory(memory string) string {
	lines := strings.Split(memory, "\n")
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		key := strings.ToLower(strings.Join(strings.Fields(line), " "))
		// Keep blank lines + section headers verbatim; only dedupe substantive
		// content lines (those with length > 8 after normalization).
		if len(key) <= 8 || strings.HasPrefix(strings.TrimSpace(line), "##") {
			out = append(out, line)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// filterTeammates returns teammates excluding the given config ID.
func filterTeammates(selfID string, teammates []*TeamMemberConfig) []*TeamMemberConfig {
	if teammates == nil {
		return nil
	}
	result := make([]*TeamMemberConfig, 0, len(teammates))
	for _, tm := range teammates {
		if tm.ID != selfID {
			result = append(result, tm)
		}
	}
	return result
}

// kickstartHintsK is how many "What Worked Before" hits we surface
// at spawn time. 3 keeps the prompt budget bounded; agents can pull
// more via the PageIndex MCP if they need them.
const kickstartHintsK = 3

// BuildLibraryHintsSection renders the top-K most-relevant past
// experiences from the knowledge library as a markdown section to
// inject into the agent's kickstart. Returns empty string when no
// hits clear the relevance threshold or the library is empty.
//
// Empty is normal on a fresh jarvis install — the library populates
// organically as tasks complete. Agents always have access to the
// PageIndex MCP regardless of this section's content.
func BuildLibraryHintsSection(config *TeamMemberConfig, workingDir string) string {
	if config == nil {
		return ""
	}
	hits, err := library.Search(library.Query{
		AgentRole:  config.Role,
		WorkingDir: workingDir,
		K:          kickstartHintsK,
	})
	if err != nil {
		slog.Debug("library search at kickstart failed", "err", err, "role", config.Role)
		return ""
	}
	return renderHintsAsMarkdown(hits)
}

// renderHintsAsMarkdown is the rendering half of
// BuildLibraryHintsSection — factored out so the next-task injection
// path can reuse it (Tier 2.1c).
func renderHintsAsMarkdown(hits []library.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Past tasks the team handled well in this area. Each entry is a real ")
	b.WriteString("experience captured under `~/.jarvis/office-memory/library/`. Use the ")
	b.WriteString("`mcp__pageindex__get_page_content` tool to read any of them in full ")
	b.WriteString("if the summary suggests a relevant approach.\n\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. **[outcome %.2f]** %s\n",
			i+1, h.OutcomeScore, dashIfEmpty(h.TaskTitle))
		if h.ApproachSummary != "" {
			b.WriteString("   Approach: ")
			b.WriteString(h.ApproachSummary)
			b.WriteString("\n")
		}
		if len(h.TaskLabels) > 0 {
			fmt.Fprintf(&b, "   Labels: %s\n", strings.Join(h.TaskLabels, ", "))
		}
		fmt.Fprintf(&b, "   Source: `%s`\n", h.Path)
	}
	return b.String()
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "(untitled task)"
	}
	return s
}
