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
		"office-memory-discipline",
		"office-context-budget",
		"office-standup",
		"office-task-handoff",
		"office-code-review",
		"implementation-plan",
		"office-reflection",
	},
	"architect": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"office-onboarding",
		"implementation-plan",
		"safe-refactor",
		"go-concurrency",
		"go-http-server",
	},
	"frontend": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"headless-browser",
		"expect-testing",
		"web-design-guidelines",
		"office-focused-search",
		"design-principles",
	},
	"backend": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"go-concurrency",
		"go-http-server",
		"go-errors",
		"go-testing",
		"go-slog",
	},
	"devops": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"git-advanced",
		"shell-scripting-safe",
		"go-pprof",
		"release-prep",
		"performance-check",
	},
	"qa": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"generate-tests",
		"run-tests",
		"go-testing",
		"debug",
		"review-changes",
	},
	"designer": {
		"office-protocols",
		"office-memory-discipline",
		"office-context-budget",
		"design-principles",
		"web-design-guidelines",
		"headless-browser",
		"office-focused-search",
		"office-task-handoff",
	},
}

// defaultSuggestedSkills is the fallback bundle when a role doesn't match
// any key in suggestedSkillsByRole. Always includes the core office
// discipline skills plus the Go fundamentals so unknown roles still get
// a capability head start in this codebase.
var defaultSuggestedSkills = []string{
	"office-protocols",
	"office-memory-discipline",
	"office-context-budget",
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

// BuildSuggestedSkillsSection returns the markdown block to inject into
// the kickstart prompt for the given role. The block lists skill names
// with their one-line descriptions read from each SKILL.md frontmatter,
// followed by a hint to discover more via `cm skill search`.
func BuildSuggestedSkillsSection(role string) string {
	skills := pickSkillsForRole(role)
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
	return b.String()
}

// pickSkillsForRole returns the priority skill list for a role string.
// Matches by lowercase substring against roleMatchOrder; falls back to
// defaultSuggestedSkills.
func pickSkillsForRole(role string) []string {
	r := strings.ToLower(role)
	for _, key := range roleMatchOrder {
		if strings.Contains(r, key) {
			if list, ok := suggestedSkillsByRole[key]; ok {
				return list
			}
		}
	}
	return defaultSuggestedSkills
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
		s = s[:max-3] + "..."
	}
	return s
}
