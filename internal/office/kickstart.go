package office

import (
	"fmt"
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

	// 3. Previous-work memory (capped)
	if agentMemory != "" {
		b.WriteString("## Previous Work Memory\n")
		if len(agentMemory) > memoryInjectMaxChars {
			b.WriteString(agentMemory[:memoryInjectMaxChars])
			b.WriteString("\n... (truncated -- use `cm memory section <name>` or `cm memory query <kw>` for the rest)\n\n")
		} else {
			b.WriteString(agentMemory)
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
	b.WriteString("A real Chromium runs as a sibling container at `ws://headless-shell:9222` (env: `$EXPECT_BROWSER_WS_ENDPOINT`, `$HEADLESS_BROWSER_HOST`). Use `browser-screenshot <url> [out.png]` for one-shot captures and read the PNG to verify. Full reference: `cm skill read headless-browser`.\n")
	b.WriteString("Run a fresh browser verification BEFORE marking any UI/frontend task complete.\n\n")

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
	}

	// 14. Your Role
	b.WriteString("## Your Role\n")
	b.WriteString(config.SystemPrompt)

	return b.String()
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
