package office

// R31 · Onboarding topics -- extracted kickstart blocks that agents
// fetch on-demand via `cm onboard <topic>` (backed by
// /api/office/agent/onboarding). The kickstart itself becomes a thin
// router that lists the topics; the full text lives here. Two
// benefits:
//
//   1. Kickstart shrinks ~55-70% per agent, reducing the spawn-burst
//      that was triggering Anthropic rate limits on 5-agent teams.
//   2. The stable portion of the kickstart (mission, router, protocols
//      reminders) moves to Claude's appendSystemPrompt where Anthropic's
//      ephemeral cache auto-breakpoints give a 90% discount on subsequent
//      agent spawns + subsequent turns.
//
// Each topic is a pure function of (agent config, working dir) so the
// handler can render without any Manager state.

import (
	"fmt"
	"strings"
)

// OnboardingTopic is the URL-safe identifier clients pass to
// /api/office/agent/onboarding?topic=...
type OnboardingTopic string

const (
	TopicProtocols     OnboardingTopic = "protocols"
	TopicBrowser       OnboardingTopic = "browser"
	TopicDevInstance   OnboardingTopic = "dev-instance"
	TopicDevLead       OnboardingTopic = "dev-lead"
	TopicContextBudget OnboardingTopic = "context-budget"
	TopicSkills        OnboardingTopic = "skills"
	TopicMemory        OnboardingTopic = "memory"
)

// OnboardingTopics returns the canonical list of topics available to
// the given tier. Worker agents get everything except "dev-lead"
// (which is leader-only; a worker calling it gets 403).
func OnboardingTopics(tier string) []OnboardingTopic {
	common := []OnboardingTopic{
		TopicProtocols,
		TopicBrowser,
		TopicDevInstance,
		TopicContextBudget,
		TopicSkills,
		TopicMemory,
	}
	if tier == "strategic" {
		return append(common, TopicDevLead)
	}
	return common
}

// GetOnboardingTopic renders the body for the given topic. Returns
// ("", false) for unknown topics so the handler can 404.
//
// Access control: dev-lead is leader-only. cfg.Tier must be
// "strategic" for that topic; otherwise callers see "" + false.
func GetOnboardingTopic(topic OnboardingTopic, cfg *TeamMemberConfig, workingDir string, eccEnabled bool) (string, bool) {
	switch topic {
	case TopicProtocols:
		return onboardingProtocols(), true
	case TopicBrowser:
		return onboardingBrowser(), true
	case TopicDevInstance:
		return onboardingDevInstance(), true
	case TopicDevLead:
		if cfg == nil || cfg.Tier != "strategic" {
			return "", false
		}
		return onboardingDevLead(), true
	case TopicContextBudget:
		return onboardingContextBudget(), true
	case TopicSkills:
		return onboardingSkills(cfg, eccEnabled), true
	case TopicMemory:
		return onboardingMemory(), true
	}
	return "", false
}

func onboardingProtocols() string {
	return `# Team Protocols

The full team protocol -- task lifecycle, git workflow, standups, brainstorms, leader assignment, debugging, and verification -- lives in the office-protocols skill. Read it when you forget a step:

  cm skill read office-protocols

Critical reminders:

- Every task: cm task ack <id> -> cm task start <id> -> cm task progress <id> "..." -> cm task complete <id> --summary "<2-3 sentence handoff>"
- Handing work over mid-task: cm task handoff <id> <targetConfigId> "<one-paragraph context>". The receiver accepts with cm task handoff-accept <id> or declines with cm task handoff-reject <id> "<reason>". Hand off when the next step is someone else's specialty AND you want the same task ID (continuous in Timeline). Complete instead when the work is done. Don't hand off to dodge effort -- operators see rejection rates per agent.
- Never push to remote, never switch branches -- you live on agent/<your-id>.
- Never claim completion without fresh verification (build, test, screenshot, exit code).
- Your bash output is auto-compressed by rtk (60-90%% fewer tokens on git / build / test / cat / docker / etc.). Failures fall back to raw output automatically. Prefix a command with rtk --raw if you need the unfiltered stream for one invocation. Full reference: cm skill read rtk.
- Before starting a task a teammate might have touched, run cm knowledge search <keywords> -- it searches every agent's memory + learnings + standups + whiteboard at once, recency-ranked. Saves hours on duplicate work. See cm skill read office-knowledge-search.
- Make your thinking visible: at every decision, blocker, milestone, hand-off, or finding, post a short sticky note with cm whiteboard "<short note>" (alias cm note). It renders as a card on the Whiteboard screen and is merged into the Reports library so the team's reasoning is browsable.
`
}

func onboardingBrowser() string {
	return `# Headless Browser

A real Chromium runs as a sibling container at ws://headless-shell:9222 (env: $EXPECT_BROWSER_WS_ENDPOINT, $HEADLESS_BROWSER_HOST). Three pre-installed CLIs drive it over CDP:

- browser-screenshot <url> [out.png] -- one-shot PNG captures
- expect-cli -m "<natural-language test>" -y -- UI flow assertions
- puppeteer-core in Node -- custom scripts

Full reference: cm skill read headless-browser.

Recommended flow:

1. ` + "`cm dev url --probe`" + ` -- get the team dev URL and confirm it's reachable right now (the --probe flag HEADs the URL and prints status on stderr).
2. ` + "`browser-screenshot $(cm dev url) /tmp/after.png`" + ` -- capture the PNG.
3. Use the Read tool on /tmp/after.png -- a screenshot you didn't look at is not evidence.
4. ` + "`cm task complete <id> --summary \"...\" --screenshot /tmp/after.png`" + ` -- attach the image to the task. Operators see the thumbnail in the Tasks tab.

If browser-screenshot fails with "Protocol error" or can't reach the host, check the browser-health pill in the Settings tab (or the operator can surface it). The sibling container may be down.

**Strong rule:** any task whose title mentions UI / frontend / page / button / modal / layout / styling / css / design / screenshot / visual / Expo / React-Native MUST carry visual verification. Either cite the path in your --summary ("Screenshotted at /tmp/signup.png -- confirmed the new copy") OR attach the PNG via --screenshot. An attached image trumps a text citation and suppresses the dashboard warning.
`
}

func onboardingDevInstance() string {
	return `# Team Dev Instance (for workers)

The team may run one or more shared dev instances of whatever app you're building. Before any browser verification:

- cm dev url           -- default "app" slot
- cm dev url <slot>    -- named slot (frontend, api, etc)
- cm dev list          -- every running slot

If empty, the lead hasn't started one yet -- ask them, or fall back to the Jarvis office URL itself for dashboard verification. Never spin up your own competing dev server on an already-held port.

If the dev instance requires login (admin pages, auth-gated flows), run cm dev creds -- the lead posts developer credentials there as a free-text blob (e.g. admin@dev.local / password123). Paste those into your browser-driver script or expect-cli test instead of being blocked at the login screen.

Full reference: cm skill read team-dev-instance.
`
}

func onboardingDevLead() string {
	return `# Leader: Team Dev Instance

If the team's mission involves building or improving a web app, start a dev instance of it in the team's working directory after your first standup. Bind the dev server to 0.0.0.0 (not localhost), pick a free port in 3000-3999, then capture its PID and announce:

  nohup npm run dev -- --host 0.0.0.0 --port 3001 > /tmp/dev.log 2>&1 & cm dev announce http://jarvis:3001 --pid $!

Passing --pid lets Jarvis SIGTERM the *full process tree* automatically if you die or the team wraps up -- no orphaned servers racing next team's ports.

**Fullstack teams:** run multiple slots, e.g.

  cm dev announce http://jarvis:3001 --name frontend --pid $FRONT_PID
  cm dev announce http://jarvis:8080 --name api --pid $API_PID

cm dev list shows the full map. Port conflicts across slots return 409 -- pick a different port.

**Expo / React-Native teams:** use expo start --web --host 0.0.0.0 --port 19006 to get a browser-screenshottable build of the app; announce http://jarvis:19006. Web mode covers most RN components; truly-native modules (Haptics, NativeModules) won't render.

If the app requires login, also run cm dev creds set "admin@dev.local / password123" (optional --name <slot>) so teammates can get past the login screen. On wrap-up, cm dev clear --all handles every slot.

**Definition of Done for web-app missions:** the project/task is NOT done until (1) a dev instance is announced (cm dev announce http://jarvis:<port>), (2) its health is green (cm dev list shows no failures), AND (3) you have verified the operator live preview renders through the proxy path /api/office/dev-proxy/<slot>/. Browser-screenshot THAT path (not the raw http://jarvis:<port>) -- it is the only path the operator's browser can reach, and a wrap-up will be blocked (409) without a healthy preview.

So the dev server's assets resolve under that subpath, start it with the proxy base set:

  Vite:  npm run dev -- --host 0.0.0.0 --port <P> --base /api/office/dev-proxy/app/
  Next:  npm run dev -- -H 0.0.0.0 -p <P>   (set basePath '/api/office/dev-proxy/app' in next.config.js)
  CRA:   set "homepage": "/api/office/dev-proxy/app" in package.json, then HOST=0.0.0.0 PORT=<P> npm start

Full reference: cm skill read team-dev-instance.
`
}

func onboardingContextBudget() string {
	return `# Context Budget

Your context window is finite. Discipline that keeps long-running agents productive:

- Write findings to memory the moment you have them: cm memory section <name> "<content>" / cm memory append "<entry>"
- Search code, do not browse it: cm skill read office-focused-search
- At ~70%% context full: offload working state to memory, then run /compact in your session
- rtk compresses bash output automatically (60-90%% fewer tokens on git / build / test / cat / docker). rtk --raw for the unfiltered stream.

Full discipline: cm skill read office-context-budget.
`
}

func onboardingSkills(cfg *TeamMemberConfig, eccEnabled bool) string {
	var b strings.Builder
	b.WriteString("# Suggested Skills\n\n")
	if cfg != nil && len(cfg.Skills) > 0 {
		b.WriteString("Custom skills for your role: ")
		b.WriteString(strings.Join(cfg.Skills, ", "))
		b.WriteString("\n\n")
	}
	role := ""
	if cfg != nil {
		role = cfg.Role
	}
	// eccEnabled is threaded from the spawn-time DisableEcc flag (the
	// onboarding handler passes !OfficeManager.IsEccDisabled()). When the team
	// opted out of ECC, pickSkillsForRole drops the ECC-sourced names so agents
	// aren't pointed at skills they can't read.
	b.WriteString(BuildSuggestedSkillsSection(role, eccEnabled))
	b.WriteString("\n")
	b.WriteString("Read any of them with `cm skill read <name>`. Search by keyword with `cm skill search <kw>`.\n")
	return b.String()
}

func onboardingMemory() string {
	return `# Memory Commands

Your memory persists across respawns and is indexed for cross-agent search. Operate on it as you work:

  cm memory section <name> "<content>"   # overwrite a named section
  cm memory append "<entry>"             # append a timestamped entry to notes
  cm memory query <keyword>              # search across all sections
  cm memory read                         # print the full file

Sections are free-form names (e.g. "current_focus", "decisions", "blockers"). Keep them short and current; the kickstart only ships the first 1500 chars of your memory, so a bloated section means less room for teammate insights on the next respawn.

Full discipline: cm skill read office-memory-discipline.
`
}

// onboardingRouterLines returns the short list embedded in the
// kickstart so the agent knows which topics exist without paying for
// the content. Tier-aware: worker agents don't see the dev-lead topic.
func onboardingRouterLines(tier string) string {
	lines := []string{
		"For deeper reference on any topic below, run `cm onboard <topic>`:",
		"  protocols       -- task lifecycle, handoffs, git rules, verification discipline",
		"  browser         -- headless Chromium CLIs for UI verification",
		"  dev-instance    -- `cm dev url/list/creds` usage for browser verification",
		"  context-budget  -- memory / compaction / rtk discipline",
		"  skills          -- role-tailored skill index + how to read them",
		"  memory          -- cm memory commands and when to write",
	}
	if tier == "strategic" {
		lines = append(lines, "  dev-lead        -- starting + announcing the team dev server")
	}
	return fmt.Sprintf("%s\n", strings.Join(lines, "\n"))
}
