package office

import (
	"fmt"
	"log/slog"
	"strings"
)

// memoryInjectMaxChars caps how much per-agent memory we inject into the
// kickstart prompt. Anything past this is replaced with a pointer to the
// `cm memory` commands so the agent can pull what they need on demand.
const memoryInjectMaxChars = 1500

// BuildKickstartPrompt generates the kickstart prompt for a team member
// session. The prompt is intentionally short -- the full team protocol
// lives in the `office-protocols` skill, which the agent reads on first
// use via `cm skill read`. This keeps the per-spawn context cost low so
// long-running agents have headroom for actual work.
//
// The prompt sections are:
//
//   1. Header (name + role)
//   2. Team mission, if set
//   3. Capped previous-work memory + cm memory pointers
//   4. Distilled learnings (small)
//   5. Compact teammate findings (one line per peer)
//   6. Identity + working dir
//   7. Context budget reminder (5 lines)
//   8. Personality
//   9. Suggested skills for role (names + 1-line descriptions)
//  10. Team relationships
//  11. One-paragraph protocol pointer to `office-protocols` skill
//  12. Headless browser pointer to `headless-browser` skill
//  13. Optional RALPH or goal-focused mode block
//  14. Role-specific SystemPrompt
func BuildKickstartPrompt(config *TeamMemberConfig, workingDir string, teammates []*TeamMemberConfig, teamMission string, agentMemory string, perpetualMode bool) string {
	var b strings.Builder

	// 1. Header
	fmt.Fprintf(&b, "# %s - %s\n\n", config.Name, config.Role)

	// 2. Team mission
	if teamMission != "" {
		b.WriteString("## Team Mission\n")
		b.WriteString(teamMission)
		b.WriteString("\n\nAlign your work with this mission. Look for ways to go above and beyond.\n\n")
	}

	// 2b. Project context (CLAUDE.md / README.md / ARCHITECTURE.md). Cached
	// per-workingDir so a team spawn only pays the file I/O once.
	if projectCtx := BuildProjectContext(workingDir); projectCtx != "" {
		b.WriteString("## Project Context\n")
		b.WriteString(projectCtx)
		b.WriteString("\n\n")
	}

	// 3. Previous-work memory (capped + deduplicated to cut redundant
	// learnings that accumulate across respawns).
	if agentMemory != "" {
		dedupedMemory := deduplicateMemory(agentMemory)
		b.WriteString("## Previous Work Memory\n")
		if len(dedupedMemory) > memoryInjectMaxChars {
			b.WriteString(dedupedMemory[:memoryInjectMaxChars])
			b.WriteString("\n... (truncated -- use `cm memory section <name>` or `cm memory query <kw>` for the rest)\n\n")
		} else {
			b.WriteString(dedupedMemory)
			b.WriteString("\n\n")
		}
		b.WriteString("Update memory as you work:\n")
		b.WriteString("  cm memory section <name> \"<content>\"   # overwrite a section\n")
		b.WriteString("  cm memory append \"<entry>\"             # append to notes\n")
		b.WriteString("  cm memory query <keyword>               # search across sections\n\n")
	}

	// 4. Distilled learnings (existing helper, already small)
	if learnings := BuildLearningsPromptSection(config.ID); learnings != "" {
		b.WriteString("## Learned Knowledge\n")
		b.WriteString(learnings)
		b.WriteString("\nReflect on new lessons via `cm reflect \"<lesson>\" --category <technique|pitfall>`. Read `cm skill read office-reflection` for the format.\n\n")
	}

	// 5. Compact teammate findings (one line per peer)
	if cross := BuildCrossAgentContextCompact(config.ID); cross != "" {
		b.WriteString("## Teammate Insights\n")
		b.WriteString(cross)
		b.WriteString("\n")
	}

	// 6. Identity
	b.WriteString("## Identity\n")
	fmt.Fprintf(&b, "You are %s, the %s on an autonomous AI development team in a Jarvis session.\n", config.Name, config.Role)
	fmt.Fprintf(&b, "Working directory: %s\n\n", workingDir)

	// 7. Context Budget reminder
	b.WriteString("## Context Budget (CRITICAL)\n")
	b.WriteString("Your context is finite. As you work:\n")
	b.WriteString("- Write findings to memory the moment you have them (`cm memory section`, `cm memory append`)\n")
	b.WriteString("- Search code, do not browse it (`cm skill read office-focused-search`)\n")
	b.WriteString("- At ~70% context full: offload working state to memory, then `/compact`\n")
	b.WriteString("- Full discipline: `cm skill read office-context-budget`\n\n")

	// 8. Personality (skip if empty)
	if config.Personality != "" {
		b.WriteString("## Personality\n")
		b.WriteString(config.Personality)
		b.WriteString("\n\n")
	}

	// 9. Suggested skills for role
	b.WriteString("## Suggested Skills (read on demand)\n")
	if len(config.Skills) > 0 {
		b.WriteString("Custom skills: ")
		b.WriteString(strings.Join(config.Skills, ", "))
		b.WriteString("\n")
	}
	b.WriteString(BuildSuggestedSkillsSection(config.Role))
	b.WriteString("\n")

	// 10. Team relationships
	b.WriteString("## Team\n")
	team := filterTeammates(config.ID, teammates)
	if len(team) > 0 {
		fmt.Fprintf(&b, "You work with %d teammates:\n", len(team))
		for _, tm := range team {
			fmt.Fprintf(&b, "- %s (%s)\n", tm.Name, tm.Role)
		}
	} else {
		b.WriteString("You are currently working solo.\n")
	}
	b.WriteString("\nTeam messages arrive as `[Team Message from <name>]: <content>`. Use `cm roster` for sessionIds, `cm message <sessionId> \"...\"` to reply.\n\n")

	// 11. Team protocols pointer (replaces ~95 lines of static content)
	b.WriteString("## Team Protocols\n")
	b.WriteString("The full team protocol -- task lifecycle, git workflow, standups, brainstorms, leader assignment, debugging, and verification -- lives in the `office-protocols` skill. Read it now and re-read any section when you forget a step:\n")
	b.WriteString("  cm skill read office-protocols\n\n")
	b.WriteString("Critical reminders:\n")
	b.WriteString("- Every task: `cm task ack <id>` -> `cm task start <id>` -> `cm task progress <id> \"...\"` -> `cm task complete <id> --summary \"<2-3 sentence handoff>\"`\n")
	b.WriteString("- Never push to remote, never switch branches -- you live on `agent/<your-id>`.\n")
	b.WriteString("- Never claim completion without fresh verification (build, test, screenshot, exit code).\n\n")

	// 12. Headless browser pointer
	b.WriteString("## Headless Browser\n")
	b.WriteString("A real Chromium runs as a sibling container at `ws://headless-shell:9222` (env: `$EXPECT_BROWSER_WS_ENDPOINT`, `$HEADLESS_BROWSER_HOST`). Three pre-installed CLIs drive it over CDP: `browser-screenshot <url> [out.png]` for one-shot PNG captures, `expect-cli -m \"<natural-language test>\" -y` for UI flow assertions, and `puppeteer-core` in Node for custom scripts. Full reference: `cm skill read headless-browser`.\n")
	b.WriteString("Run a fresh browser verification BEFORE marking complete any task that touches user-visible behavior -- this covers frontend/UI work AND any QA sign-off on a feature rendered in the browser. Capture the PNG, then Read it to confirm the expected state; a screenshot you didn't look at is not evidence.\n\n")

	// 12b. Team dev instance pointer — every agent learns to check the
	// team dev URL + creds before running browser verification. The lead
	// announces one dev server per slot; fullstack teams can have several
	// (e.g. "frontend" + "api"). Default slot is "app".
	b.WriteString("## Team Dev Instance\n")
	b.WriteString("The team may run one or more shared dev instances of whatever app you're building. Before any browser verification, run `cm dev url` (default \"app\" slot) or `cm dev url <slot>` (for named slots like `frontend` or `api`). `cm dev list` shows every running slot. If empty, the lead hasn't started one yet -- ask them, or fall back to the Jarvis office URL itself for dashboard verification. Never spin up your own competing dev server on an already-held port.\n")
	b.WriteString("If the dev instance requires login (admin pages, auth-gated flows), run `cm dev creds` -- the lead posts developer credentials there as a free-text blob (e.g. `admin@dev.local / password123`). Paste those into your browser-driver script or expect-cli test instead of being blocked at the login screen.\n")
	b.WriteString("**Strong rule:** any task whose title mentions UI / frontend / page / button / modal / layout / styling / css / design / screenshot / visual / Expo / React-Native MUST cite the browser verification in your `cm task complete --summary`. Example: \"Screenshotted at /tmp/signup.png -- confirmed the new copy and disabled state\". A summary for a UI-keyword task that says only \"implemented X\" will trigger a dashboard warning.\n")
	b.WriteString("Full reference: `cm skill read team-dev-instance`.\n\n")

	// 13. Work mode
	if perpetualMode {
		b.WriteString("## Work Mode: RALPH (perpetual)\n")
		b.WriteString("You must NEVER be idle. After completing any task: `cm task list` -> `cm skill search <topic>` -> `cm reflect \"<lesson>\"` -> `cm task self-add ...` -> start the next task.\n")
		b.WriteString("If you have no tasks: hunt TODOs, write missing tests, refactor for clarity, or improve docs. Avoid duplicate work -- check `git log --oneline -20` and `cm roster` first.\n")
		b.WriteString("Full RALPH protocol: `cm skill read office-protocols`.\n\n")
	} else {
		b.WriteString("## Work Mode: Goal-Focused\n")
		b.WriteString("Complete assigned tasks thoroughly. When done, run `cm task list` to verify, review your work, commit, then wait for the next standup. Do not invent work beyond the mission scope.\n\n")
	}

	// Leader-specific (only strategic-tier agents)
	if config.Tier == "strategic" {
		b.WriteString("## Leader Task Assignment (strategic tier)\n")
		b.WriteString("After standup you receive a `[LEADER TASK ASSIGNMENT]` prompt. Analyze each member's report, create 2-4 actionable tasks per teammate, respond with `cm leader-tasks respond \"TASKS_FOR:<id>\\n- [HIGH] title | description\\n...\"`. Route tasks to the right teammate by role. Full protocol: `cm skill read office-protocols`.\n")
		if team := filterTeammates(config.ID, teammates); len(team) > 0 {
			b.WriteString("Routing:\n")
			for _, tm := range team {
				fmt.Fprintf(&b, "- %s tasks -> %s (%s)\n", tm.Role, tm.Name, tm.ID)
			}
		}
		b.WriteString("\n")

		// Leader responsibility for the team dev instance. Placed in the
		// strategic block so only leads get the "you start the server"
		// instruction; workers get the "check cm dev url" reminder above.
		b.WriteString("## Leader: Team Dev Instance\n")
		b.WriteString("If the team's mission involves building or improving a web app, start a dev instance of it in the team's working directory after your first standup. Bind the dev server to `0.0.0.0` (not `localhost`), pick a free port in 3000-3999, then capture its PID and announce:\n")
		b.WriteString("  `nohup npm run dev -- --host 0.0.0.0 --port 3001 > /tmp/dev.log 2>&1 & cm dev announce http://jarvis:3001 --pid $!`\n")
		b.WriteString("Passing `--pid` lets Jarvis SIGTERM the *full process tree* automatically if you die or the team wraps up -- no orphaned servers racing next team's ports.\n")
		b.WriteString("**Fullstack teams:** run multiple slots, e.g. `cm dev announce http://jarvis:3001 --name frontend --pid $FRONT_PID` AND `cm dev announce http://jarvis:8080 --name api --pid $API_PID`. `cm dev list` shows the full map. Port conflicts across slots return 409 -- pick a different port.\n")
		b.WriteString("**Expo / React-Native teams:** use `expo start --web --host 0.0.0.0 --port 19006` to get a browser-screenshottable build of the app; announce `http://jarvis:19006`. Web mode covers most RN components; truly-native modules (Haptics, NativeModules) won't render.\n")
		b.WriteString("If the app requires login, also run `cm dev creds set \"admin@dev.local / password123\"` (optional `--name <slot>`) so teammates can get past the login screen. On wrap-up, `cm dev clear --all` handles every slot. Full reference: `cm skill read team-dev-instance`.\n\n")
	}

	// 14. Your Role
	b.WriteString("## Your Role\n")
	b.WriteString(config.SystemPrompt)

	return prependBudgetWarning(b.String(), config.Name)
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
