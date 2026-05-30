package office

import (
	"strings"
	"testing"
)

// Maximum acceptable kickstart prompt length for a default office-7
// spawn. Before R31's router refactor it was ~13k chars; the kickstart
// now ships only per-agent-unique content (identity, memory, role) and
// delegates boilerplate to the append-system-prompt (cacheable) + the
// `cm onboard <topic>` CLI. Tight ceiling to catch regressions.
const kickstartMaxChars = 7000

// makeOffice7Team builds a representative office-7 lineup for size tests.
func makeOffice7Team() []*TeamMemberConfig {
	return []*TeamMemberConfig{
		{ID: "max", Name: "Max", Role: "Engineering Manager", Tier: "strategic",
			Personality:  "Pragmatic, decisive, calm under pressure.",
			SystemPrompt: "Lead the team. Assign work after standup. Unblock people."},
		{ID: "sage", Name: "Sage", Role: "Senior Architect", Tier: "strategic",
			Personality:  "Thoughtful, system-thinking, evidence-driven.",
			SystemPrompt: "Own architecture decisions. Document tradeoffs."},
		{ID: "nova", Name: "Nova", Role: "UI/UX Designer", Tier: "worker",
			Personality:  "Visual, opinionated, user-empathetic.",
			SystemPrompt: "Own design decisions. Verify with screenshots."},
		{ID: "vex", Name: "Vex", Role: "Frontend Engineer", Tier: "worker",
			Personality:  "Detail-oriented, fast iterator.",
			SystemPrompt: "Own the frontend. Verify in browser."},
		{ID: "bolt", Name: "Bolt", Role: "Backend Engineer", Tier: "worker",
			Personality:  "Methodical, test-driven, root-cause thinker.",
			SystemPrompt: "Own backend services. Test before complete."},
		{ID: "forge", Name: "Forge", Role: "DevOps Engineer", Tier: "worker",
			Personality:  "Automation-first, cautious about prod.",
			SystemPrompt: "Own infra and merges. Resolve conflicts."},
		{ID: "lint", Name: "Lint", Role: "QA Engineer", Tier: "worker",
			Personality:  "Skeptical, edge-case hunter.",
			SystemPrompt: "Own test coverage. Find what others miss."},
	}
}

func TestKickstartPromptUnderSizeLimit(t *testing.T) {
	team := makeOffice7Team()
	mission := "Ship the Q2 reliability initiative -- improve uptime to 99.9%."

	for _, member := range team {
		got := BuildKickstartPrompt(member, "/app/workspace/example", team, mission, "", true, nil)
		if len(got) > kickstartMaxChars {
			t.Errorf("kickstart for %s (%s) is %d chars, exceeds limit of %d",
				member.Name, member.Role, len(got), kickstartMaxChars)
		}
		// Sanity: the prompt must still mention the role. The protocol
		// and context-budget pointers moved to BuildStableSystemPrompt
		// (see TestStableSystemPromptCoversBoilerplate).
		if !strings.Contains(got, member.Role) {
			t.Errorf("kickstart for %s missing role text", member.Name)
		}
	}
}

// R31 · Workers should skip the leader-task-assignment + leader
// dev-instance blocks entirely. Strategic agents keep them.
func TestKickstartWorkerTrimsLeaderBlocks(t *testing.T) {
	team := makeOffice7Team()
	strategic := team[0] // Max, Engineering Manager
	worker := team[2]    // Nova, UI/UX Designer
	stratKickstart := BuildKickstartPrompt(strategic, "/tmp/x", team, "", "", true, nil)
	workerKickstart := BuildKickstartPrompt(worker, "/tmp/x", team, "", "", true, nil)

	if !strings.Contains(stratKickstart, "Leader Task Assignment") {
		t.Error("strategic kickstart should contain Leader Task Assignment block")
	}
	if strings.Contains(workerKickstart, "Leader Task Assignment") {
		t.Error("worker kickstart should NOT contain Leader Task Assignment block")
	}
	if strings.Contains(workerKickstart, "cm onboard dev-lead") {
		t.Error("worker kickstart should NOT reference cm onboard dev-lead")
	}

	// And worker kickstart should be meaningfully smaller.
	if len(workerKickstart) >= len(stratKickstart) {
		t.Errorf("expected worker kickstart (%d) < strategic (%d)",
			len(workerKickstart), len(stratKickstart))
	}
}

// R31 · Stable prefix carries the cross-agent boilerplate. Anthropic's
// prompt cache auto-breakpoints on system prompts above ~1k tokens
// and gives 90%% discount on cache hits, so the 2nd-Nth agent on a
// team spawn pay ~10%% of this prefix.
func TestStableSystemPromptCoversBoilerplate(t *testing.T) {
	for _, tier := range []string{"strategic", "worker"} {
		got := BuildStableSystemPrompt(tier, "Ship the Q2 initiative.", nil)
		for _, mustHave := range []string{
			"Team Mission",
			"Lazy-load Reference",
			"cm onboard",
			"protocols",
			"memory",
			"cm task ack",
			"agent/<your-id>",
			// R37 · Self-sustaining task primitives (Claude /goal +
			// help-seeking) live in the cached prefix.
			"/goal",
			"cm ask",
			"office-goal-loop",
			"office-ask-for-help",
		} {
			if !strings.Contains(got, mustHave) {
				t.Errorf("stable prefix for %s missing %q", tier, mustHave)
			}
		}
	}
	// dev-lead only appears for strategic.
	strat := BuildStableSystemPrompt("strategic", "", nil)
	worker := BuildStableSystemPrompt("worker", "", nil)
	if !strings.Contains(strat, "dev-lead") {
		t.Error("strategic stable prefix should mention dev-lead topic")
	}
	if strings.Contains(worker, "dev-lead") {
		t.Error("worker stable prefix should NOT mention dev-lead topic")
	}
}

func TestKickstartCapsPreviousWorkMemory(t *testing.T) {
	team := makeOffice7Team()
	huge := strings.Repeat("x", memoryInjectMaxChars*3)
	got := BuildKickstartPrompt(team[0], "/app/workspace/example", team, "", huge, false, nil)
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
		// Frontend: browser verification + team dev instance.
		{"Frontend Engineer", "headless-browser"},
		{"Frontend Engineer", "expect-testing"},
		{"Frontend Engineer", "team-dev-instance"},
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
		// QA: test generation + browser verification + dev-instance discovery.
		{"QA Engineer", "generate-tests"},
		{"QA Engineer", "headless-browser"},
		{"QA Engineer", "expect-testing"},
		{"QA Engineer", "team-dev-instance"},
		// Leads (manager/architect) + backend/devops also need the dev-instance skill.
		{"Engineering Manager", "team-dev-instance"},
		{"Senior Architect", "team-dev-instance"},
		{"Backend Engineer", "team-dev-instance"},
		{"DevOps Engineer", "team-dev-instance"},
		{"UI/UX Designer", "design-principles"},
		// Unknown role -> default bundle.
		{"Some Unknown Role", "office-protocols"},
		{"Some Unknown Role", "go-concurrency"},
		{"Some Unknown Role", "git-advanced"},
	}
	for _, tc := range cases {
		skills := pickSkillsForRole(tc.role, true)
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
