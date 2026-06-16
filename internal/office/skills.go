package office

// Per-role skill recommendations injected into the kickstart prompt.
// The goal is to give each agent a short, role-tailored list of skill
// names with one-line descriptions, so they pull skills lazily via
// `cm skill read <name>` instead of having full skill content baked
// into the kickstart.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// suggestedSkillsByRole maps a lowercase substring of a role name (e.g.
// "manager", "architect") to an ordered list of recommended skill names.
// Order is priority -- the kickstart shows the first 8.
//
// To pick the right bucket for a role, BuildSuggestedSkillsSection checks
// each key as a substring of the lowercase role and uses the first match.
// "Senior Architect" matches "architect"; "Engineering Manager" matches
// "manager".
var suggestedSkillsByRole = map[string][]string{
	"manager": {
		"office-protocols",
		// Self-sustaining task loop using Claude Code's /goal command
		// (R37). Read once, applied to every multi-step task.
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-standup",
		"office-task-handoff",
		// Structured help-seeking with lead-first + fallback. The lead
		// is most often on the *receiving* end; this skill teaches them
		// how to answer well and how to ask up the chain when needed.
		"office-ask-for-help",
		"office-code-review",
		"implementation-plan",
		"team-dev-instance",
		"office-self-improvement",
		// P10 · ECC adds: planning + evaluation + safe-change discipline.
		"agent-eval",
		"verification-loop",
		"continuous-learning",
	},
	"architect": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-onboarding",
		"office-ask-for-help",
		"implementation-plan",
		"team-dev-instance",
		"go-concurrency",
		"go-http-server",
		"go-mcp-tools",
		// P10 · ECC adds: API + ADR discipline for cross-cutting design.
		"api-design",
		"architecture-decision-records",
		"backend-patterns",
		"security-review",
	},
	"frontend": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-ask-for-help",
		"headless-browser",
		"team-dev-instance",
		"expect-testing",
		"web-design-guidelines",
		"office-focused-search",
		"design-principles",
		// R21 · Crafted-feel principles: concentric radii, optical
		// alignment, tabular-nums, scale-on-press, 40x40 hit area.
		// Read before merging any UI work.
		"make-interfaces-feel-better",
		// P10 · ECC adds: structured frontend direction + e2e flow.
		"frontend-design-direction",
		"e2e-testing",
		"browser-qa",
	},
	"backend": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-ask-for-help",
		"team-dev-instance",
		"go-concurrency",
		"go-http-server",
		"go-errors",
		"jarvis-auth",
		"go-atomic-writes",
		// P10 · ECC adds: backend patterns + API design discipline.
		"backend-patterns",
		"api-design",
		"deployment-patterns",
	},
	"devops": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-ask-for-help",
		"team-dev-instance",
		"git-advanced",
		"shell-scripting-safe",
		"docker-compose-debug",
		"go-pprof",
		"release-prep",
		// P10 · ECC adds: structured git + deployment + security review.
		"git-workflow",
		"deployment-patterns",
		"security-review",
	},
	"qa": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-ask-for-help",
		// Browser verification is first-class for QA of any user-visible
		// feature. Without these two, QA agents default to unit-test-only
		// verification and miss regressions that only appear in the DOM.
		"headless-browser",
		"team-dev-instance",
		"expect-testing",
		"generate-tests",
		"run-tests",
		"debug",
		"review-changes",
		// R21 · Checklist-driven UI polish pass. QA references this on
		// any UI-keyword task to spot missing animations, hit areas,
		// tabular numbers, and concentric radii before sign-off.
		"make-interfaces-feel-better",
		// P10 · ECC adds: TDD + e2e + verification-loop discipline.
		"tdd-workflow",
		"e2e-testing",
		"verification-loop",
		"eval-harness",
	},
	"designer": {
		"office-protocols",
		"office-goal-loop",
		"office-memory-discipline",
		"office-context-budget",
		"office-ask-for-help",
		"design-principles",
		"web-design-guidelines",
		"make-interfaces-feel-better",
		"headless-browser",
		"office-focused-search",
		"office-task-handoff",
		// P10 · ECC adds: explicit direction-setting framework.
		"frontend-design-direction",
	},
}

// defaultSuggestedSkills is the fallback bundle when a role doesn't match
// any key in suggestedSkillsByRole. Always includes the core office
// discipline skills plus the Go fundamentals so unknown roles still get
// a capability head start in this codebase.
var defaultSuggestedSkills = []string{
	"office-protocols",
	"office-goal-loop",
	"office-memory-discipline",
	"office-context-budget",
	"office-ask-for-help",
	"office-onboarding",
	"office-focused-search",
	"go-concurrency",
	"git-advanced",
	"debug",
}

// matchOrder controls which role-key wins when multiple keys are
// substrings of the same role. Earlier entries win.
var roleMatchOrder = []string{"manager", "architect", "designer", "frontend", "backend", "devops", "qa"}

// skillFrontmatterCache holds parsed `description:` values from
// ~/.claude/skills/<name>/SKILL.md, populated lazily on first call.
var (
	skillFrontmatterCache = map[string]string{}
	skillFrontmatterOnce  sync.Once
)

// resetSkillFrontmatterCacheForTest clears the lazy cache + its sync.Once
// so tests can re-exercise loadSkillFrontmatterCache against different
// fixtures (missing dir, malformed SKILL.md, etc). Production code must
// not call this.
func resetSkillFrontmatterCacheForTest() {
	skillFrontmatterCache = map[string]string{}
	skillFrontmatterOnce = sync.Once{}
}

// BuildSuggestedSkillsSection returns the markdown block to inject into
// the kickstart prompt for the given role. The block lists skill names
// with their one-line descriptions read from each SKILL.md frontmatter,
// followed by a hint to discover more via `cm skill search`.
//
// P11.2.7 · Gates ECC-sourced skill names on the team's per-spawn
// `disableEcc` flag. When ECC is disabled, ECC entries are filtered
// from the recommendation list AND the ECC-mentioning footer hint is
// suppressed. Mirrors the existing pattern for codegraph (the
// `CodegraphBinaryAvailable()` gate further down).
func BuildSuggestedSkillsSection(role string, eccEnabled bool) string {
	skills := pickSkillsForRole(role, eccEnabled)
	descs := loadSkillFrontmatterCache()

	var b strings.Builder
	b.WriteString("You have 60+ skills available. Read only what you need with `cm skill read <name>`.\n")
	b.WriteString("For your role, start with these (read them lazily, not all at once):\n")
	for _, name := range skills {
		desc := descs[name]
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- %-28s -- %s\n", name, oneLine(desc))
	}
	b.WriteString("\nDiscover more with `cm skill search <keyword>`. See `cm skill read office-skill-discovery` for the search discipline.\n")

	// P6.3 · Nudge agents toward the codegraph MCP for structural
	// codebase questions. Only emit when the binary is on PATH —
	// otherwise the hint would point at tools the agent can't reach.
	if CodegraphBinaryAvailable() {
		b.WriteString("\nFor structural code questions (\"how does X work\", \"what calls Y\", \"blast radius of changing Z\") prefer the `mcp__codegraph__codegraph_context` tool over grep+read. It returns search + callers + callees + signatures in one call against a pre-built AST index of this workspace.\n")
	}

	// Headroom context-compression nudge. Only emit when the binary is
	// present so the hint points at tools the agent can actually reach.
	// You run perpetually -- context is your scarcest resource.
	if HeadroomAvailable() {
		b.WriteString("\nContext compression: you run for a long time and context is scarce. Before pasting a LARGE tool output, log, dependency tree, or search result into your reasoning, pass it through `mcp__headroom__headroom_compress` -- it shrinks the blob 60-95% while preserving meaning. The compression is REVERSIBLE: call `mcp__headroom__headroom_retrieve` to get the original back if you need a detail. Check `mcp__headroom__headroom_stats` to see how much context you've saved. Prefer this over dumping raw multi-thousand-line outputs into context.\n")
	}
	return b.String()
}

// eccSkillNames is the set of skill names that come from the
// vendored ECC bundle (P10.5 additions to suggestedSkillsByRole).
// Kept here rather than tagged in the per-role lists so a single
// audit reveals which names are ECC-sourced. When ECC is disabled
// for a team, pickSkillsForRole filters these out so operators don't
// see `cm skill read agent-eval` hints they can't satisfy.
var eccSkillNames = map[string]struct{}{
	"agent-eval":                   {},
	"verification-loop":            {},
	"continuous-learning":          {},
	"api-design":                   {},
	"architecture-decision-records": {},
	"backend-patterns":             {},
	"security-review":              {},
	"frontend-design-direction":    {},
	"e2e-testing":                  {},
	"browser-qa":                   {},
	"deployment-patterns":          {},
	"git-workflow":                 {},
	"tdd-workflow":                 {},
	"eval-harness":                 {},
}

// pickSkillsForRole returns the priority skill list for a role string.
// Matches by lowercase substring against roleMatchOrder; falls back to
// defaultSuggestedSkills. Deduplicates the chosen list so a future
// merge that accidentally lists the same skill twice (e.g. jarvis +
// vendored ECC overlap) doesn't surface duplicates to operators.
// P11.2.7 · When eccEnabled is false, drops every name present in
// eccSkillNames so the kickstart hint stays accurate for ECC-disabled
// teams.
func pickSkillsForRole(role string, eccEnabled bool) []string {
	r := strings.ToLower(role)
	var picked []string
	for _, key := range roleMatchOrder {
		if strings.Contains(r, key) {
			if list, ok := suggestedSkillsByRole[key]; ok {
				picked = list
				break
			}
		}
	}
	if picked == nil {
		picked = defaultSuggestedSkills
	}
	deduped := dedupeSkills(picked)
	if eccEnabled {
		return deduped
	}
	out := make([]string, 0, len(deduped))
	for _, name := range deduped {
		if _, isECC := eccSkillNames[name]; isECC {
			continue
		}
		out = append(out, name)
	}
	return out
}

// dedupeSkills returns a copy of `in` with duplicate skill names
// removed, preserving first-occurrence order. Stable so the kickstart
// prompt's skill listing is deterministic across runs.
func dedupeSkills(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// loadSkillFrontmatterCache returns a cached map of skill name -> the
// `description:` field from each ~/.claude/skills/<name>/SKILL.md. Walks
// the skills directory once per process. If the directory does not
// exist or a SKILL.md is malformed, the corresponding entry is set to
// an empty string -- BuildSuggestedSkillsSection then prints
// "(no description)".
func loadSkillFrontmatterCache() map[string]string {
	skillFrontmatterOnce.Do(func() {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return
		}
		skillsRoot := filepath.Join(homeDir, ".claude", "skills")
		entries, err := os.ReadDir(skillsRoot)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(skillsRoot, e.Name(), "SKILL.md")
			desc, _ := readSkillDescription(path)
			skillFrontmatterCache[e.Name()] = desc
		}
	})
	return skillFrontmatterCache
}

// readSkillDescription opens a SKILL.md and extracts the `description:`
// field from its YAML frontmatter (lines between the first two `---`
// markers). Returns "" if the file is missing, has no frontmatter, or
// has no description field.
func readSkillDescription(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 32*1024), 256*1024)

	// Expect first non-empty line to be `---`.
	inFrontmatter := false
	lineNo := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNo++
		if !inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
			if lineNo > 3 {
				// No frontmatter at the top.
				return "", nil
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			return "", nil
		}
		if strings.HasPrefix(line, "description:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			val = strings.Trim(val, `"'`)
			return val, nil
		}
	}
	return "", scanner.Err()
}

// oneLine collapses any internal newlines and trims to a sensible
// kickstart length so the suggested-skills table stays compact.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	const max = 110
	if len(s) > max {
		limit := max - 3
		for limit > 0 && !utf8.RuneStart(s[limit]) {
			limit--
		}
		s = s[:limit] + "..."
	}
	return s
}
