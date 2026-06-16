package office

// Agent Skills integration. agent-skills (https://github.com/addyosmani/agent-skills,
// MIT, © Addy Osmani) encodes a 7-step engineering SDLC -- DEFINE (/spec) ->
// PLAN (/plan) -> BUILD (/build) -> VERIFY (/test) -> REVIEW (/review) ->
// SIMPLIFY (/code-simplify) -> SHIP (/ship) -- plus backing skills and
// specialist agent personas.
//
// jarvis vendors the pack the same way it vendors ECC:
//   - 24 skill bundles under skills/agent-skills/<name>/ (shipped to
//     ~/.claude/skills via scripts/install-skills.sh)
//   - 4 Task-tool agent personas (embedded here, surfaced via BuildAgentSkillsAgents)
//   - 7 phase slash commands (embedded here, surfaced via BuildAgentSkillsCommands)
//
// The commands + personas are appended to an agent's .claude/ tree only when the
// team's 7-step engineering mode is on (see WorktreeHooksConfig.SevenStepMode).
// MIT attribution lives in THIRD_PARTY_LICENSES.md, skills/agent-skills/{LICENSE,NOTICE},
// and an inline `> Sourced from ...` line in each embedded .md body.

import (
	"embed"
	"fmt"
	"strings"
	"sync"
)

//go:embed agentskills_agents/*.md
var agentSkillsAgentsFS embed.FS

//go:embed agentskills_commands/*.md
var agentSkillsCommandsFS embed.FS

// Parse-once memoisation, mirroring ECC: SetupWorktreesWithHooks calls the
// Build* helpers inside the per-member spawn loop, so without this an N-member
// team re-parses every embedded file N times.
var (
	agentSkillsAgentsOnce   = sync.OnceValue(func() []ClaudeAgent { return parseEmbeddedAgents(agentSkillsAgentsFS, "agentskills_agents") })
	agentSkillsCommandsOnce = sync.OnceValue(func() []ClaudeCommand { return parseEmbeddedCommands(agentSkillsCommandsFS, "agentskills_commands") })
)

// BuildAgentSkillsAgents parses every embedded agent-skills persona into a
// ClaudeAgent record. Memoised; returns a defensive copy.
func BuildAgentSkillsAgents() []ClaudeAgent {
	cached := agentSkillsAgentsOnce()
	out := make([]ClaudeAgent, len(cached))
	copy(out, cached)
	return out
}

// BuildAgentSkillsCommands parses every embedded agent-skills slash command
// into a ClaudeCommand record (basename -> command name). Memoised; returns a
// defensive copy.
func BuildAgentSkillsCommands() []ClaudeCommand {
	cached := agentSkillsCommandsOnce()
	out := make([]ClaudeCommand, len(cached))
	copy(out, cached)
	return out
}

// resetAgentSkillsMemoForTest clears the OnceValue caches so tests can
// re-exercise the parse path. Production code must not call this.
func resetAgentSkillsMemoForTest() {
	agentSkillsAgentsOnce = sync.OnceValue(func() []ClaudeAgent { return parseEmbeddedAgents(agentSkillsAgentsFS, "agentskills_agents") })
	agentSkillsCommandsOnce = sync.OnceValue(func() []ClaudeCommand { return parseEmbeddedCommands(agentSkillsCommandsFS, "agentskills_commands") })
}

// hasAgentSkillsAttribution checks vendored content carries the MIT attribution
// in the first 200 bytes of the body (mirrors hasBodyAttribution for ECC).
func hasAgentSkillsAttribution(body string) bool {
	const window = 200
	leading := body
	if len(leading) > window {
		leading = leading[:window]
	}
	return strings.Contains(leading, "github.com/addyosmani/agent-skills")
}

// AgentSkillsEmbedSummary is a startup diagnostic mirroring EmbedSummaryForLogging.
func AgentSkillsEmbedSummary() string {
	return fmt.Sprintf("agent-skills: %d agents, %d commands embedded",
		len(BuildAgentSkillsAgents()), len(BuildAgentSkillsCommands()))
}
