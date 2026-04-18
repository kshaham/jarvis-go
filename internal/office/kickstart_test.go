package office

import (
	"strings"
	"testing"
)

// Maximum acceptable kickstart prompt length for a default office-7
// spawn. Before the slim-down it was ~16500 chars; the goal of the
// refactor is to keep it under this ceiling so long-running agents
// have headroom in their context window.
const kickstartMaxChars = 12000

// makeOffice7Team builds a representative office-7 lineup for size tests.
func makeOffice7Team() []*TeamMemberConfig {
	return []*TeamMemberConfig{
		{ID: "max", Name: "Max", Role: "Engineering Manager", Tier: "strategic",
			Personality: "Pragmatic, decisive, calm under pressure.",
			SystemPrompt: "Lead the team. Assign work after standup. Unblock people."},
		{ID: "sage", Name: "Sage", Role: "Senior Architect", Tier: "strategic",
			Personality: "Thoughtful, system-thinking, evidence-driven.",
			SystemPrompt: "Own architecture decisions. Document tradeoffs."},
		{ID: "nova", Name: "Nova", Role: "UI/UX Designer", Tier: "worker",
			Personality: "Visual, opinionated, user-empathetic.",
			SystemPrompt: "Own design decisions. Verify with screenshots."},
		{ID: "vex", Name: "Vex", Role: "Frontend Engineer", Tier: "worker",
			Personality: "Detail-oriented, fast iterator.",
			SystemPrompt: "Own the frontend. Verify in browser."},
		{ID: "bolt", Name: "Bolt", Role: "Backend Engineer", Tier: "worker",
			Personality: "Methodical, test-driven, root-cause thinker.",
			SystemPrompt: "Own backend services. Test before complete."},
		{ID: "forge", Name: "Forge", Role: "DevOps Engineer", Tier: "worker",
			Personality: "Automation-first, cautious about prod.",
			SystemPrompt: "Own infra and merges. Resolve conflicts."},
		{ID: "lint", Name: "Lint", Role: "QA Engineer", Tier: "worker",
			Personality: "Skeptical, edge-case hunter.",
			SystemPrompt: "Own test coverage. Find what others miss."},
	}
}

func TestKickstartPromptUnderSizeLimit(t *testing.T) {
	team := makeOffice7Team()
	mission := "Ship the Q2 reliability initiative -- improve uptime to 99.9%."

	for _, member := range team {
		got := BuildKickstartPrompt(member, "/app/workspace/example", team, mission, "", true)
		if len(got) > kickstartMaxChars {
			t.Errorf("kickstart for %s (%s) is %d chars, exceeds limit of %d",
				member.Name, member.Role, len(got), kickstartMaxChars)
		}
		// Sanity: the prompt must still mention the role and the protocol
		// pointer so agents know where to find the rest.
		if !strings.Contains(got, member.Role) {
			t.Errorf("kickstart for %s missing role text", member.Name)
		}
		if !strings.Contains(got, "office-protocols") {
			t.Errorf("kickstart for %s missing office-protocols pointer", member.Name)
		}
		if !strings.Contains(got, "Context Budget") {
			t.Errorf("kickstart for %s missing Context Budget reminder", member.Name)
		}
	}
}

func TestKickstartCapsPreviousWorkMemory(t *testing.T) {
	team := makeOffice7Team()
	huge := strings.Repeat("x", memoryInjectMaxChars*3)
	got := BuildKickstartPrompt(team[0], "/app/workspace/example", team, "", huge, false)
	if !strings.Contains(got, "(truncated") {
		t.Errorf("expected truncation marker when injecting %d-char memory; got prompt of %d chars",
			len(huge), len(got))
	}
	if len(got) > kickstartMaxChars {
		t.Errorf("kickstart with huge memory still exceeded size limit: %d > %d",
			len(got), kickstartMaxChars)
	}
}

func TestPickSkillsForRoleMatchesExpectedBuckets(t *testing.T) {
	cases := []struct {
		role     string
		mustHave string
	}{
		// Core office-* skills + round-3 self-improvement meta-skill.
		{"Engineering Manager", "office-standup"},
		{"Engineering Manager", "office-code-review"},
		{"Engineering Manager", "office-self-improvement"},
		// Architect needs implementation-plan + Go fundamentals + MCP tools.
		{"Senior Architect", "implementation-plan"},
		{"Senior Architect", "go-http-server"},
		{"Senior Architect", "go-mcp-tools"},
		// Frontend unchanged.
		{"Frontend Engineer", "headless-browser"},
		{"Frontend Engineer", "expect-testing"},
		// Backend: Go fundamentals + jarvis-auth + atomic writes.
		{"Backend Engineer", "go-concurrency"},
		{"Backend Engineer", "go-http-server"},
		{"Backend Engineer", "go-errors"},
		{"Backend Engineer", "jarvis-auth"},
		{"Backend Engineer", "go-atomic-writes"},
		// DevOps: git + shell + compose debug.
		{"DevOps Engineer", "git-advanced"},
		{"DevOps Engineer", "shell-scripting-safe"},
		{"DevOps Engineer", "docker-compose-debug"},
		{"DevOps Engineer", "release-prep"},
		// QA: test generation + browser verification are both first-class.
		{"QA Engineer", "generate-tests"},
		{"QA Engineer", "headless-browser"},
		{"QA Engineer", "expect-testing"},
		{"UI/UX Designer", "design-principles"},
		// Unknown role -> default bundle.
		{"Some Unknown Role", "office-protocols"},
		{"Some Unknown Role", "go-concurrency"},
		{"Some Unknown Role", "git-advanced"},
	}
	for _, tc := range cases {
		skills := pickSkillsForRole(tc.role)
		found := false
		for _, s := range skills {
			if s == tc.mustHave {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("role %q: expected suggested skills to include %q; got %v",
				tc.role, tc.mustHave, skills)
		}
	}
}
