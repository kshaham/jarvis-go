# Jarvis Go

> Multi-agent orchestration platform -- manage teams of autonomous Claude Code agents working on real codebases.

![Go 1.25](https://img.shields.io/badge/go-1.25-blue) ![License MIT](https://img.shields.io/badge/license-MIT-green) ![Tests 600+](https://img.shields.io/badge/tests-600%2B-brightgreen) ![Fixes 95+](https://img.shields.io/badge/fixes-95%2B-purple)

---

## Overview

Jarvis Go is a production-grade multi-agent orchestration system implemented in pure Go with zero CGO dependencies. It runs a web server that manages both **Claude Agent SDK** sessions (persistent multi-turn) and traditional **PTY** sessions hosting Claude Code agents, coordinates them as a virtual office team with self-improvement feedback loops, and provides a browser-based dashboard for monitoring and control.

### Key Capabilities

- **Dual session modes**: Claude Agent SDK (Go) for persistent multi-turn sessions with immediate prompt delivery, plus PTY fallback mode
- **Virtual office team**: 7 prebuilt templates (43 agent configs) across full-stack, backend/AI, frontend, research, and innovation categories
- **Autonomous monitoring loop**: 12 functions on a 15-second tick (stuck detection, reflection, curiosity, task triage, water-cooler chat, bug sweeps, blocked-task escalation, more)
- **Self-improvement engine**: agents reflect on completed tasks, build a per-agent learning store, auto-distill insights at 30 entries, and share skills across the team
- **Perpetual RALPH loop**: agents never idle -- they find TODOs, write tests, review commits, refactor code, generate their own tasks
- **Cross-agent knowledge sharing**: teammate learnings and mission insights injected into each agent's kickstart prompt
- **Agent memory persistence**: per-project memory files via `cm memory read/write/append` survive restarts and team respawns
- **CodeGraphContext (CGC) integration**: agents use an MCP-backed code graph to query call chains, dependencies, and patterns instead of reading entire files (cuts context consumption 5-10x)
- **5-column kanban** for task tracking with dependencies, priorities, leader assignment, and blocked-task triage
- **48+ MCP tools**: full `office_*` tool suite for agent task/memory/skill/message operations plus session/monitoring/planning tools
- **Z.AI Coding Plan support**: GLM models via Anthropic-compatible API with rate-limit-aware model allocation across team members (GLM-5.1 Opus, GLM-5 Sonnet, GLM-4.7 per agent)
- **Telegram bot integration**: remote monitoring, session control, periodic office summaries
- **Chat & standup transcript persistence**: inter-agent messages and standup transcripts saved to disk, restored on page refresh and server restart
- **37 agent SVG icons** + 11 role-based fallbacks for visual team identity
- **Hardened for production**: 95+ bug fixes across 10 review rounds (goroutine leaks, data races, XSS, CSRF, path traversal, nil panics, resource cleanup)

---

## Architecture

```
jarvis-go/
├── cmd/
│   ├── jarvis/             # Main web server binary
│   ├── jarvis-mcp/         # MCP stdio server (48+ tools)
│   ├── cm/                 # Agent CLI (injected into agent sessions)
│   ├── ptyserver/          # Standalone PTY server
│   └── fakeclaude/         # Test impersonator of `claude` CLI (hooks integration tests)
│
└── internal/
    ├── office/             # Virtual office: manager, agents, tasks, learning, recovery,
    │                       #   standup, brainstorm, kickstart, scoring, metrics
    ├── pty/                # Dual-mode sessions: SDK (claude-agent-sdk-go) + PTY fallback,
    │                       #   screen buffering, idle detection, rate-limit allocation
    ├── claudehooks/        # Claude Code hook dispatcher (envelope, settings, transcript, handlers)
    ├── respawn/            # Automatic session respawn state machine
    ├── ralph/              # RALPH loop tracker and circuit breaker
    ├── subagent/           # Subagent watcher (Claude Code sub-tasks)
    ├── telegram/           # Telegram bot, commands, streaming, queue, webhook
    ├── teamtemplates/      # Built-in team composition templates + store
    ├── templates/          # Session/team configuration templates
    ├── state/              # Persistent application settings (JSON, atomic write)
    ├── scheduler/          # Cron-style scheduler (runner, store) for recurring tasks
    ├── notifications/      # Notification router + channels + store
    ├── metrics/            # Prometheus metrics exposition
    ├── cases/              # Saved prompt cases
    ├── collections/        # LRU map, stale expiration map
    ├── integrations/       # External integration state
    ├── runsummary/         # Run summary parsing and storage
    ├── transcript/         # Transcript read/parse with authoritative file position
    ├── cleanup/            # Graceful shutdown helpers
    ├── filewatcher/        # File system watcher for hot-reload
    └── web/
        ├── handler/        # HTTP handlers (session, office, config, monitoring, scheduler, ...)
        ├── sse/            # Server-Sent Events broker, batcher, client pool
        ├── middleware/     # Auth, CSRF (with retry), request logging, rate limiting
        ├── auth/           # Session authentication + agent-token verifier
        ├── request/        # Request parsing helpers
        └── static/         # Embedded frontend assets (office.html, office.js, etc.)
```

**30 packages**, ~50k lines of Go. Dual session architecture: SDK sessions spawn
a persistent `claude.NewSession()` subprocess that stays alive across all turns
(kickstart, standup, brainstorm, replenishment), while PTY sessions use traditional
screen-scraping for fallback compatibility.

---

## Quick Start

### Build

```bash
go build ./...

# Build the main server
make build

# Cross-compile for all platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
make build-all
```

### Install bundled skills (one-time)

Jarvis ships ~54 curated skills under `./skills/` that the built-in team
templates reference by name (`office-protocols`, `go-concurrency`,
`agentic-eval`, etc.). Install them into `~/.claude/skills/` so agents
can resolve them:

```bash
./scripts/install-skills.sh              # symlink, skip existing
./scripts/install-skills.sh --copy       # copy instead of symlink
./scripts/install-skills.sh --force      # overwrite existing
./scripts/install-skills.sh --dry-run    # preview only
```

Skipping this step is non-fatal — agents still run, but skill lookups
return empty descriptions and prompt quality degrades.

### Run

```bash
./jarvis web                    # Start on port 3555 (default)
./jarvis web --port 8080        # Custom port
./jarvis web --https            # TLS mode
./jarvis version
```

Open `http://localhost:3555` in your browser.

### Docker Deployment

The recommended production setup uses Docker Compose:

```bash
docker compose up -d
```

The container exposes port **3555** by default. See `docker-compose.yml` for volume mounts — workspaces, Claude credentials, skills, and configuration are all mounted from the host.

### Visual verification (optional)

Agents verify UI changes by taking screenshots of a shared dev instance
via a sibling `chromedp/headless-shell` container. Two things are
required:

1. **Build the browser-enabled image**: `docker build --target=runtime-browser -t jarvis-browser .`
   The `runtime-browser` stage extends the default runtime with
   Node.js + `puppeteer-core` + `/usr/local/bin/browser-screenshot`.
   Without this stage, `browser-screenshot` is a phantom command.
2. **Start the sibling headless-shell container**: uncomment the
   `headless-shell` service in `docker-compose.yml` and run
   `docker compose --profile browser up -d`. Jarvis reaches it on
   the compose network at `ws://headless-shell:9222`.

Flow: a lead agent starts a dev server and runs
`cm dev announce http://jarvis:<port> --pid $!`. Workers fetch the
URL with `cm dev url` (optionally `--probe` for a live HEAD check),
screenshot with `browser-screenshot <url> /tmp/out.png`, and attach
the image to the task via `cm task complete <id> --summary "..."
--screenshot /tmp/out.png`. Operators see the screenshot inline on
the Tasks tab. The `GET /api/office/browser/health` endpoint
exposes the headless-shell's reachability + version for dashboard
indicators.

The production jarvis container we run internally ships a richer
image (custom `expect-cli` etc.) built outside this repo. The
repo's `runtime-browser` stage is a minimal reproduction — enough
to give cloners a working flow.

### Environment Variables

| Variable | Description | Default |
|---|---|---|
| `JARVIS_API_URL` | Server URL (used by `cm` CLI and `jarvis-mcp` inside sessions) | `http://localhost:3555` |
| `JARVIS_SESSION_ID` | Current session ID (injected per agent) | auto-generated |
| `JARVIS_AGENT_TOKEN` | Per-agent auth token; `cm` sends it to authenticate back to the server | auto-generated |
| `JARVIS_HOOK_SIGNING_KEY` | HMAC key used by `internal/claudehooks` to verify hook webhooks | required when hooks mode is on |
| `JARVIS_WORKSPACE_PATHS` | Colon-separated list of paths added to the workingDirAllowlist at startup | — |
| `JARVIS_LOG_LEVEL` | Log verbosity (`debug`, `info`, `warn`, `error`) | `info` |
| `JARVIS_LOG_FORMAT` | Log encoding (`text`, `json`) | `json` |
| `ANTHROPIC_AUTH_TOKEN` | Z.AI API key (Coding Plan proxy auth); injected per agent | — |
| `ANTHROPIC_BASE_URL` | Anthropic-compatible endpoint URL; injected per agent | `https://api.z.ai/api/anthropic` |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | Opus model mapping; injected per agent | `glm-5.1` |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | Sonnet model mapping (per-agent override by allocator) | `glm-4.7` |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | Haiku model mapping; injected per agent | `glm-4.5-air` |
| `ANTHROPIC_API_KEY` | Direct Anthropic API key (if not using Z.AI); injected per agent | — |
| `API_TIMEOUT_MS` | Request timeout for agent API calls | `3000000` |
| `GEMINI_API_KEY` | Gemini API key (for Nano Banana image generation) | — |
| `HEADLESS_BROWSER_HOST` | Chrome headless host for agent browser testing | `headless-shell` |
| `HEADLESS_BROWSER_PORT` | Chrome DevTools port | `9222` |
| `EXPECT_BROWSER_WS_ENDPOINT` | WebSocket endpoint puppeteer-core connects to (set in `runtime-browser` stage) | `ws://headless-shell:9222` |

The `ANTHROPIC_*` variables are read by Claude Code (the subprocess), not by jarvis itself. Jarvis injects them into each agent session based on the account settings configured in the UI.

### Mobile App

A companion React Native app (Expo) lives at `claudeman/jarvis-mobile/`. It connects to the jarvis HTTP server over LAN or TLS and uses a per-device bearer token instead of the browser's session cookie.

Endpoints (all under `/api/auth/mobile/`):

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/auth/mobile/login` | none (rate-limited per IP, same as `/api/auth/login`) | Body `{"password": "...", "deviceName": "My iPhone"}`. Returns `{"token": "...", "deviceId": "...", "expiresAt": <unix-ms>}` exactly once. |
| POST | `/api/auth/mobile/logout` | bearer token | Revokes the presented token. |
| GET | `/api/auth/mobile/tokens` | session cookie OR bearer | Lists registered devices (no tokens, just metadata). |
| DELETE | `/api/auth/mobile/tokens/{deviceId}` | session cookie OR bearer | Revokes a specific device (e.g. lost phone). Idempotent. |

The mobile bearer is accepted alongside session cookies on every existing `/api/office/*` route, so the mobile app can drive everything the web dashboard can. Bearer-authenticated requests skip CSRF (they're not vulnerable to cross-origin abuse the way cookies are).

Tokens persist to `~/.jarvis/mobile-tokens.json` with `0600` permissions. Only the SHA-256 hash of the raw token is on disk; the raw value is returned to the client exactly once. Default TTL is 90 days; expired tokens are evicted lazily on next validation.

---

## Office Agent System

### Team Templates

Jarvis ships with **7 prebuilt team templates** (43 agent configs) accessible via `GET /api/office/templates`:

| Template | Category | Members | Composition |
|----------|----------|---------|-------------|
| **Office (Default)** | fullstack | 7 | Max (Eng Manager), Sage (Architect), Nova (Designer), Vex (Frontend), Bolt (Backend), Forge (DevOps), Lint (QA) |
| **Backend/AI** | backend | 6 | Aria (AI Lead), Tensor (ML), Prompt (Prompt Eng), Dataflow (Data), Nexus (API), Guardian (AI QA) |
| **Frontend Studio** | frontend | 6 | Pixel (Design Lead), React (Architect), Craft (UI), Flow (Interaction), Access (A11y), Perf (Performance) |
| **Full-Stack Compact** | fullstack | 5 | Captain (Tech Lead), Dash (Frontend), Stack (Backend), Shield (QA/Security), Ops (DevOps) |
| **Research Lab** | research | 7 | Atlas (Director), Cipher (Researcher), Prism (Analyst), Proto (POC), Slide (Presentation), Muse (Brand), Quill (Writer) |
| **Innovation Sprint** | research | 6 | Spark (Lead), Scout (Research), Flux (Prototype), Pitch (Storyteller), Brief (Writer), Verify (Validator) |
| **Core Builders** | fullstack | 6 | Vince (Tech Lead + wiki), Lisa (Research/QA), Allen (Framework Scout/QA), Kyle (Design — liquid-glass aesthetic), Kam (Backend), Henry (Frontend) |

Each team member has:
- **Role** -- e.g., Frontend Engineer, Backend Engineer, QA Engineer, Tech Lead, DevOps Engineer, Researcher
- **Tier** -- `strategic` (leader) or `worker`
- **Personality** -- multi-paragraph character description injected at session start
- **Skills** -- list of domain competencies
- **Color** -- visual identifier in the dashboard
- **Emoji** -- Unicode glyph for at-a-glance identification

Custom teams can be created and saved through the UI.

### Perpetual vs Goal-Focused Mode

At spawn time, the UI exposes a **Perpetual Improvement Mode** toggle (default: ON):

- **Perpetual mode**: Agents run the RALPH (Recognize, Assess, Log, Plan, Handle) loop continuously. They never idle -- when task queues empty, they scan for TODOs, write tests, review commits, refactor code, and generate their own tasks via `cm task self-add`. Recovery system injects replenishment prompts if an agent genuinely runs out of work.
- **Goal-focused mode**: Agents complete their assigned tasks then stop. Useful for mission-critical work where you want the team to halt after the goal is met rather than continue self-directing.

The selected mode is injected into the kickstart prompt and governs whether the recovery system's idle-replenishment loop is active.

### Spawn Flow

1. User clicks "Spawn Team" in the UI (or calls `POST /api/office/spawn-team`)
2. For each team member, the server creates a PTY session running `claude`
3. After session stabilizes, `BuildKickstartPrompt` generates the full onboarding prompt and injects it
4. Each agent receives: team mission, previous memory, learned knowledge, identity, personality, skills, team relationships, task queue instructions, file lock protocol, communication tools, and `cm` CLI reference
5. Idle monitoring starts on a 60-second tick

### Kickstart Prompt Sections

The kickstart prompt is composed dynamically per agent and includes:

| Section | Description |
|---|---|
| Team Mission | Project-level goal (plus appended mission insights from `AppendMissionInsight`) |
| Previous Work Memory | Persisted memory from prior sessions via `cm memory` (per-project) |
| Learned Knowledge | Top 10 recent learnings + top 5 skills + all distilled insights from past reflections |
| **Teammate Insights** | Cross-agent context: up to 3 recent learnings from each teammate |
| **Context Optimization** | Instructions to use `mcp__codegraphcontext__*` tools for code queries instead of reading full files |
| Identity + Personality | Deep role immersion paragraph (supports runtime override via API) |
| Self-Improvement Mission | Working-directory context and autonomous improvement framing |
| Team Relationships | Names, roles, tiers of all teammates |
| Task Queue Status | Current assigned/pending task count |
| File Lock Protocol | How to use `cm lock`/`cm unlock` before editing shared files |
| Communication Protocol | Task lifecycle (ack → start → progress → complete), standup/brainstorm commands |
| **Knowledge & Skills Toolkit** | Commands for shared skills (`cm skill list/search/read/create`), curiosity discovery (`cm discover`), memory management (`cm memory read/write/append`) |
| Git Workflow | Branch/worktree guidance per agent (no-push policy, conventional commits) |
| RALPH Loop (perpetual) | Continuous improvement loop if perpetualMode is enabled |
| Goal-Focused (goal mode) | Alternative stop-after-goal guidance |

### Standup

Automated standup is triggered on a configurable interval (default: 4 hours) or manually via `POST /api/office/standup`. Each agent receives a prompt asking for status and blockers, and responds via `cm standup respond <content>`. The office manager collects responses with a 5-minute timeout, sends nudges every 60 seconds (up to 3 times), and publishes the summary.

### Brainstorm

`POST /api/office/brainstorm` initiates a collaborative planning session. The leader receives task proposals from all workers via `cm brainstorm respond <proposals>`. The brainstorm system uses a role-based collaboration map to route proposals to relevant teammates.

### Wrap-Up

`POST /api/office/wrap-up` sends a graceful shutdown prompt to all agents instructing them to commit work, write memory, and exit cleanly. The wrap-up state is tracked so the UI can display progress. `POST /api/office/wrap-up/cancel` aborts the sequence.

### Shutdown

`POST /api/office/shutdown-team` terminates all agent sessions after an optional wrap-up.

### Snapshots & Fork Mode

Team snapshots capture the team's configuration, mission, member list, per-member tasks + queues, whiteboard, perpetual-mode state, pause state, and (when available) each member's `ClaudeSessionID` so restores can rehydrate the AI session via `claude --resume`.

- `POST /api/office/snapshots` — capture the current team to a named snapshot
- `POST /api/office/snapshots/{name}/restore` — spawn the saved team
- `POST /api/office/snapshots/{name}/restore?fork=true` — **fork mode**: keep the original team alive, spawn a parallel team that resumes each member's Claude transcript via `claude --fork-session`. Members without a captured session ID start fresh. Useful for branching experiments off a stuck agent. SSE: `office:snapshotRestored` with `forked: true` and `forkCount: N`.

Implementation: `internal/office/snapshot.go`. Restore handler: `internal/web/handler/office.go` `OfficeRestoreSnapshot`.

### Per-session generated files

When jarvis spawns an agent, it lays down a small `.claude/` tree inside the agent's worktree before the Claude subprocess starts. This is how hooks, tools, and per-team policy reach the agent without operator config:

| File | Purpose | Generated by |
|---|---|---|
| `.claude/settings.local.json` | Hook event URLs (PreToolUse, Stop, SubagentStart, …), strict MCP config, allowed tool list | `internal/claudehooks/settings.go` |
| `.claude/commands/*.md` | Office slash commands + 3 ECC commands (`/security-scan`, `/test-coverage`, `/refactor-clean`) | `internal/office/claudefiles.go` + `claudefiles_defaults.go` |
| `.claude/agents/*.md` | 3 office-native Task-tool agents + 4 ECC agents (`code-architect`, `security-reviewer`, `pr-test-analyzer`, `code-simplifier`) | same |
| `.claude/output-style.json` | Bottom-of-prompt prefix showing team / role / blocker count | `internal/office/statusline.go` |
| `.claude/statusline.sh` | Statusline script that `curl`s `/api/office/agent/statusline?sessionId=...` for a one-line live status | same |
| `.claude/jarvis-system-prompt.md` | Kickstart prompt prefix, prompt-cache-friendly (passed via `--append-system-prompt-file`) | `internal/office/kickstart.go` |

ECC files are skipped when the spawn request carries `disableEcc: true`.

### Ultrareview on completion

Tasks labelled `needs-review` (via `cm label <taskId> needs-review`) trigger an adversarial code review when the agent marks them complete. The review shells out to `claude ultrareview --json` against the agent's diff and posts the findings as an `office:taskReviewed` SSE event. Findings carry severity (`blocker` / `warning` / `nit` / `info`), file, line, and a description; the task detail view renders them as a coloured badge stack. Implementation: `internal/office/ultrareview.go`, hook: `internal/web/handler/office.go` `OfficeAgentTaskComplete`.

---

## Agent Autonomy

### 12 Monitoring Functions

The idle monitoring goroutine fires every 60 seconds and runs all 12 functions in sequence:

| # | Function | Description |
|---|---|---|
| 1 | `checkSessionHealth` | Detects dead PTY sessions and unregisters them |
| 2 | `CleanupExpiredLocks` | Releases file locks past their TTL (30 min) |
| 3 | `SweepBugTasks` | Clears stale bug tasks |
| 4 | `autoCompleteIdleAgentTasks` | Marks tasks complete when agent has been idle too long |
| 5 | `checkUnacknowledgedTasks` | Nudges agents who haven't acknowledged assigned tasks |
| 6 | `checkStaleInProgressTasks` | Sends progress-check prompts to agents on long-running tasks |
| 7 | `checkForStuckAgents` | Triggers recovery tier escalation for unresponsive agents |
| 8 | `CheckForIdleAgents` | Sends replenishment prompts when agents run out of work |
| 9 | `checkForCuriousAgents` | Fires curiosity prompts to encourage exploration |
| 10 | `checkForReflectionOpportunity` | Prompts reflection after task completions |
| 11 | `checkBlockedTasksForLeaderTriage` | Escalates blocked tasks to the strategic-tier leader |
| 12 | `maybeTriggerWaterCoolerChat` | Random probability social interactions between agents |

Additionally, `PersistMetrics` runs each tick to flush metrics to disk.

### Recovery Tiers

When an agent becomes unresponsive (default stuck threshold: 5 minutes), the recovery system escalates through three tiers:

| Tier | Action | Trigger |
|---|---|---|
| 1 | Send Enter key | First inactivity detection |
| 2 | Ctrl+C + recovery prompt | Second consecutive detection |
| 3 | Ctrl+C + diagnostic prompt | Third consecutive detection |

After `RecoveryMaxAttempts` (default: 3) exhausted within the recovery window (30 minutes), an `AgentStuckEvent` is emitted and the agent is flagged.

### Session Health

`checkSessionHealth` runs every tick. Any session whose PTY process has died triggers `HandleAgentDeath`, which:
1. Reassigns all `pending` and `acknowledged` tasks from the dead agent to remaining team members
2. Unregisters the agent from the office roster
3. Broadcasts a `memberLeft` event to SSE clients

### Wrap-Up Enforcement

The manager tracks wrap-up state. During wrap-up, the monitoring loop monitors agent responses and enforces clean shutdown. If agents do not respond within the configured timeout, escalation prompts are sent.

---

## Self-Improvement

Agents accumulate knowledge through a structured reflection loop stored in `~/.jarvis/learnings/<configId>.json`.

### Reflection Loop

```
Task Completed
     |
     v
cm reflect "<learning>" --category <cat>
     |
     v
AddLearningEntry() --> persisted to learning store
     |
     v (every 30 entries)
DistillLearnings() --> groups by category, extracts key insights
     |
     v
distilledInsights[] --> included in next kickstart prompt
```

### Learning Store

Each agent's `AgentLearningData` contains:

| Field | Description | Cap |
|---|---|---|
| `learnings` | Raw learning entries with category and task context | 50 max |
| `skills` | Reusable techniques with usage counts | 25 max |
| `distilledInsights` | Condensed summaries produced by distillation | 10 max |
| `metrics` | Total counts, timestamps for last reflection/curiosity/distillation | — |

Distillation triggers automatically at 30 entries, processing the oldest 15 entries and consolidating them into insights by category.

### Skill Creation

Agents can register reusable techniques via `cm skill create`:

```bash
cm skill create "Optimistic UI updates" "Apply state change immediately, then sync with server on success/failure"
```

Skills are deduplicated by name and tracked with a `usageCount` and `lastUsedAt` timestamp.

### Cross-Agent Sharing

When building the kickstart prompt, `BuildCrossAgentContext` loads learning stores from all other agents on the team (excluding self) and includes up to 3 recent learnings per teammate in the kickstart as a "Teammate Insights" section. This lets workers benefit from techniques discovered by teammates without manual coordination.

### Agent Memory (Per-Project Persistence)

Each agent has a **project-scoped memory file** at `~/.jarvis/agent-memory/{projectHash}/{configId}.md`. Memory survives team respawns, server restarts, and project switches. Agents read/write their memory via:

```bash
cm memory read                  # Read current memory
cm memory write "<content>"     # Overwrite memory (for full restructuring)
cm memory append "<entry>"      # Append a new entry (most common)
```

Memory is loaded into the kickstart as "Previous Work Memory" -- agents resume where they left off. The reflection trigger and wrap-up prompt both remind agents to save their current context via `cm memory write` before ending a session.

### Team Mission Insights

As agents complete work and discover new context about the mission, they can contribute back via `AppendMissionInsight`. The mission becomes a living document:

```
Original mission: "Build a real-time chat application"
+ [Learned] WebSocket heartbeats required for NAT traversal
+ [Learned] Message deduplication needed after reconnect
```

The updated mission is persisted to `~/.jarvis/office-memory/team-mission.txt` and included in every future kickstart prompt. This creates a feedback loop where team knowledge accumulates across sprints.

### Curiosity & Discovery

The monitoring loop fires `checkForCuriousAgents` which sends exploration prompts on a cooldown. Agents respond via `cm discover`:

```bash
cm discover "Found unused legacy auth code" \
  --findings "src/auth/legacy.go has 200 LoC of unused OAuth1 code" \
               "Migration is gated on removing OAuth1 flow" \
  --self-assigned "Remove legacy OAuth1 code" \
  --committed
```

Discoveries feed into the learning store with the `curiosity` category and trigger optional self-task creation.

---

## Task Management

### 5-Column Kanban

Tasks flow through five statuses mapped to kanban columns:

```
Backlog (pending) --> To Do (acknowledged) --> In Progress (in_progress) --> Review (blocked) --> Done (completed/failed)
```

### Task Fields

| Field | Description |
|---|---|
| `id` | UUID |
| `title` | Short description |
| `description` | Full details |
| `status` | `pending`, `acknowledged`, `in_progress`, `completed`, `failed`, `blocked` |
| `priority` | `high`, `medium`, `low` |
| `source` | `leader`, `self`, `bug` |
| `blockedBy` | IDs of blocking tasks |
| `blockerDescription` | Human-readable blocker explanation |

### Task Lifecycle via cm CLI

```bash
cm task list                           # View assigned tasks
cm task ack <taskId>                   # Acknowledge receipt
cm task start <taskId>                 # Mark in progress
cm task progress <taskId> "note"       # Report progress
cm task blocked <taskId> "reason"      # Report blocker
cm task complete <taskId>              # Mark done
cm task self-add "title" "description" # Add self-generated task
cm task triage <taskId> <toSessionId> "reason"  # Transfer to teammate
```

### Dependencies

Tasks support `blockedBy` arrays. `checkBlockedTasksForLeaderTriage` monitors blocked tasks and escalates to the strategic-tier leader after `BlockedTriageMaxAttempts` (default: 3) with prompts asking for resolution strategies.

### Leader Assignment

Strategic-tier agents receive task assignment requests via `cm leader-tasks respond <content>`. The leader receives the full team roster and current task queues to make intelligent routing decisions. The `LeaderTaskCh` channel in `AgentState` delivers assignment responses back to the manager.

---

## cm CLI Reference

The `cm` binary is compiled and injected into every agent session. It communicates with the Jarvis server over HTTP using `JARVIS_API_URL` and `JARVIS_SESSION_ID`.

| Command | Description |
|---|---|
| `cm standup respond <content>` | Submit standup status update |
| `cm brainstorm respond <proposals>` | Submit task proposals during brainstorm |
| `cm leader-tasks respond <content>` | Submit leader task assignments |
| `cm reflect <learning> --category <cat>` | Record post-task learning |
| `cm discover <summary> --findings <f>` | Report curiosity exploration findings |
| `cm task list` | List assigned tasks |
| `cm task ack <taskId>` | Acknowledge a task |
| `cm task start <taskId>` | Begin a task |
| `cm task complete <taskId>` | Complete a task |
| `cm task progress <taskId> <note>` | Report progress on a task |
| `cm task blocked <taskId> <reason>` | Report a blocker |
| `cm task self-add <title> <desc>` | Self-generate a new task |
| `cm task triage <taskId> <to> <reason>` | Transfer task to another agent |
| `cm lock <filePath>` | Acquire a distributed file lock |
| `cm unlock <filePath>` | Release a file lock |
| `cm locks` | List all active file locks |
| `cm roster` | Get current team roster |
| `cm message <toSessionId> <content>` | Send direct message to teammate |
| `cm skill list` | List all shared skills |
| `cm skill read <name>` | Read a skill |
| `cm skill create <name> <desc>` | Create a shared skill |
| `cm skill search <query>` | Search skills by keyword |
| `cm memory read` | Read agent memory |
| `cm memory write <content>` | Write agent memory |
| `cm memory append <entry>` | Append to agent memory |
| `cm memory section <name> "<content>"` | Overwrite a named section in agent memory |
| `cm memory query <keyword>` | Search agent memory across all sections |
| `cm knowledge <query>` (alias `cm know`) | Search the cross-agent knowledge base |
| `cm label <taskId> <label>` | Attach a feature label to a task (cost rollups) |
| `cm handoff <taskId> <toAgent> <reason>` | Hand a task off to another teammate |
| `cm handoff-accept <handoffId>` | Accept an incoming handoff |
| `cm handoff-reject <handoffId> <reason>` | Decline an incoming handoff |
| `cm handoff-cancel <handoffId>` | Cancel a handoff you initiated |
| `cm ask <toAgent> "<question>" [--timeout <sec>]` | Blocking Q&A — wait for the teammate to answer |
| `cm answer <questionId> "<answer>"` | Reply to a pending `cm ask` |
| `cm answered <questionId>` | Mark a question as resolved without an answer |
| `cm no-recipient <questionId>` | Mark a question as undeliverable (no matching agent) |
| `cm dev announce <url> --pid <pid>` | Lead announces the shared dev instance URL |
| `cm dev url [--probe]` | Workers fetch the current dev URL (with optional health probe) |
| `cm dev clear [--all]` | Clear the dev instance + credentials |
| `cm dev creds set "<text>" [--name <slot>]` | Share credentials (e.g. `"admin@dev.local / password123"`) |
| `cm dev creds` | Print stored credentials |
| `cm onboard` / `cm onboarding <topic>` | Print onboarding doc; `<topic>` defaults to the topic router |

---

## Telegram Integration

### Setup

1. Create a bot via [@BotFather](https://t.me/botfather) and get a bot token
2. Start a conversation with the bot and get your chat ID
3. In the Jarvis UI go to Settings > Telegram and enter token + chat ID
4. Click "Test" to verify connectivity

Or via API:

```bash
curl -X POST http://localhost:3000/api/settings/telegram \
  -H "Content-Type: application/json" \
  -d '{"botToken": "BOT_TOKEN", "chatID": "CHAT_ID"}'
```

### Bot Commands

| Command | Description |
|---|---|
| `/start` or `/help` | Show available commands |
| `/status` | Server status and active session count |
| `/sessions` | List all active sessions |
| `/list_agents` | List office team members |
| `/send <sessionId> <message>` | Send input to a session |
| `/new <working-dir>` | Create a new session |
| `/link <sessionId>` | Link chat to a session for streaming |
| `/unlink` | Unlink current chat session |
| `/linked` | Show linked session info |
| `/output` | Get recent output from linked session |

### Office Notifications

When the office team is active, Telegram receives periodic updates (configurable interval, default: every team event). Notifications include: member joins/leaves, standup summaries, task assignments, recovery events, and agent metrics.

---

## MCP Server

The `jarvis-mcp` binary implements the Model Context Protocol (MCP) over stdio transport. It exposes **44+ tools** split into two categories: **agent-facing `office_*` tools** that use `JARVIS_SESSION_ID` automatically (for in-session agents), and **orchestration tools** for external controllers (Claude Desktop, other Claude Code instances).

Jarvis-MCP is also registered as an MCP server inside every spawned agent session, giving agents a natively-scoped toolset for task/memory/skill operations.

### Session Tools (orchestration)

| Tool | Description |
|---|---|
| `create_session` | Create a new PTY or SDK session |
| `list_sessions` | List all active sessions |
| `get_session` | Get session details by ID |
| `stop_session` | Stop a session |
| `send_to_session` | Send input to a session |
| `get_session_output` | Retrieve recent session output |

### Agent Introspection

| Tool | Description |
|---|---|
| `list_agents` | List office team members |
| `get_agent_status` | Get agent state and task queue |
| `get_agent_transcript` | Read agent conversation transcript |

### Office Agent Tools (in-session, auto-scoped to `JARVIS_SESSION_ID`)

These tools let agents manage their own task queue, memory, skills, and team coordination directly via MCP without shelling out to the `cm` CLI.

**Task lifecycle:**

| Tool | Description |
|---|---|
| `office_task_list` | List your own task queue |
| `office_task_self_add` | Add a self-generated task (title, description, priority) |
| `office_task_ack` | Acknowledge receipt of an assigned task |
| `office_task_start` | Mark a task in-progress |
| `office_task_progress` | Report progress with note |
| `office_task_complete` | Mark a task complete |
| `office_task_blocked` | Report a blocker (captures reason + `isBlocked` flag) |

**Collaboration:**

| Tool | Description |
|---|---|
| `office_standup_respond` | Submit standup status update |
| `office_reflect` | Submit a post-task learning with category |
| `office_team_roster` | Get current team roster |
| `office_send_message` | Send a direct message to a teammate (via `fromSessionId`) |

**Persistent memory:**

| Tool | Description |
|---|---|
| `office_memory_read` | Read your project-scoped persistent memory |
| `office_memory_write` | Overwrite your memory |
| `office_memory_append` | Append an entry to your memory |

**Shared skill library (office-scoped):**

| Tool | Description |
|---|---|
| `office_skill_list` | List all shared team skills |
| `office_skill_search` | Search skills by keyword |
| `office_skill_read` | Read a skill's content |
| `office_skill_create` | Create or update a shared skill |

### Shared Skill Tools (orchestration)

| Tool | Description |
|---|---|
| `list_skills` | List all shared skills |
| `get_skill` | Read a skill by name |
| `create_skill` | Create a shared skill |
| `search_skills` | Search skills by keyword |
| `delete_skill` | Remove a skill |

### Scheduled Task Queue Tools

| Tool | Description |
|---|---|
| `add_task` | Add to the scheduled task queue |
| `list_tasks` | List scheduled tasks |
| `get_task` | Get scheduled task details |
| `remove_task` | Remove a scheduled task |

### Monitoring Tools

| Tool | Description |
|---|---|
| `get_git_status` | Git status for a session |
| `get_build_status` | Build status (pass/fail/running) |
| `get_test_status` | Test run status |
| `get_integration_state` | Integration health state |
| `get_app_state` | Full app state snapshot |
| `get_stats` | Server statistics |
| `get_config` | Current server configuration |

### Planning + Dynamic Tools

| Tool | Description |
|---|---|
| `generate_plan` | Generate an implementation plan |
| `register_tool` | Register a dynamic tool at runtime |
| `unregister_tool` | Unregister a dynamic tool |
| `list_dynamic_tools` | List dynamically registered tools |
| `run_prompt` | Run an arbitrary prompt in a session |

### Configuration

Add to `~/.claude/settings.json` or `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "jarvis": {
      "command": "/path/to/jarvis-mcp",
      "env": {
        "JARVIS_API_URL": "http://localhost:3555"
      }
    }
  }
}
```

### Additional MCP servers (auto-attached per team)

Every spawned agent session gets the following MCP servers wired
automatically. Operators don't need to add them to `~/.claude/settings.json`
— jarvis injects them into each agent's `.claude/settings.local.json`
at spawn time.

| Server | Always on? | Gate | What it provides |
|---|---|---|---|
| `jarvis` | yes | always | Office orchestration tools (roster, tasks, memory, knowledge, skills, file locks). |
| `codegraphcontext` | yes | always | Symbol search and codebase navigation. |
| `codegraph` | optional | `codegraph` binary on PATH AND `disableCodegraph: false` (default) | Full symbol index + call graph + impact analysis. |
| `context7` | optional | `npx` on PATH (Node 18+) AND `disableEcc: false` (default) | Live library documentation lookup. |
| `sequential-thinking` | optional | same as context7 | Structured chain-of-thought reasoning for complex tasks. |
| `shadcn` | optional | same as context7 | UI component discovery (metadata only; doesn't run code generation). |

Hosts without `npx` silently skip the ECC MCPs rather than failing the
spawn — see `office.NpxAvailable()` for the cached check. To disable
the entire ECC bundle (commands + agents + these three MCPs) per team,
pass `disableEcc: true` in the spawn request. See `docs/ECC.md`.

---

## Claude Agent SDK (Go) Sessions

Jarvis uses the [Claude Agent SDK for Go](https://github.com/shaharia-lab/claude-agent-sdk-go) to run agents in **persistent multi-turn sessions**. This replaces the traditional single-shot `claude.Query()` model with a long-lived `claude.NewSession()` subprocess that stays alive across all turns.

### Why SDK Sessions?

The old PTY screen-scraping approach had brittleness issues: chunked Ink input, magic-string idle detection, menu auto-selection hacks, pasted-text detection, and ANSI stripping. The SDK provides:

- **Structured events** (TypeSystem, TypeAssistant, TypeStreamEvent, TypeResult, TypeRateLimitEvent) instead of ANSI output parsing
- **Persistent subprocess**: kickstart is Turn 1; standup/brainstorm/replenishment prompts are subsequent turns delivered immediately via `Session.Send()` without waiting for the current turn to complete
- **Non-blocking prompt delivery**: when you want an agent to pay attention RIGHT NOW (standup response, urgent reassignment), `SendPrompt()` writes to stdin instantly
- **Explicit token/cost tracking** per turn via `TypeResult` events
- **MCP integration**: agents receive the `jarvis` and `codegraphcontext` MCP servers automatically via `claude.WithMcpServers(...)`

### Session Lifecycle

```
OfficeSpawnTeam → spawnAgentSession (per member)
     │
     ├── Pre-generate sessionID for MCP/env injection
     ├── Build kickstart prompt (memory, learnings, cross-agent, CGC instructions, RALPH loop)
     ├── Inject JARVIS_SESSION_ID + JARVIS_API_URL into subprocess env
     ├── Create SDK session via claude.NewSession(ctx, options...)
     ├── eventConsumer goroutine ranges over session.Events()
     ├── Send kickstart as Turn 1
     └── Session stays alive; subsequent Send() calls deliver standup/brainstorm/reflection prompts
```

### PTY Fallback

If SDK session creation fails, `spawnAgentSession` falls back to creating a traditional PTY session (`pty.NewSession`) so the system degrades gracefully. Both session types implement the same `SessionStore` interface, so all office manager code works against either.

---

## CodeGraphContext (CGC) Integration

Agents are spawned with an additional MCP server: [CodeGraphContext](https://github.com/CodeGraphContext/CodeGraphContext). CGC builds a knowledge graph of the codebase (functions, classes, relationships, dependencies) in an embedded KuzuDB and exposes query tools via MCP.

### Why?

Long-running autonomous agents on real codebases accumulate massive context as they read entire files to understand what calls what. After a few hours of work, a single agent may have spent $40+ on context alone. CGC lets agents query structured relationships instead:

- **Before**: "Read `auth.go` to understand how login flow works" → 2000 lines in context
- **After**: `mcp__codegraphcontext__find_callers(authenticateUser)` → list of 5 callers + their line numbers

Token usage drops 5-10x on exploration tasks.

### Auto-Index on Spawn

When `OfficeSpawnTeam` is called, Jarvis kicks off `cgc index <workingDir>` in a background goroutine (5-minute timeout). The index is built while agents are already working on their kickstart, so it's ready by the time they need to query it.

### Integration Points

1. **Kickstart**: Every agent's kickstart includes a "Context Optimization (CRITICAL)" section instructing them to prefer `mcp__codegraphcontext__*` tools over reading full files
2. **MCP Server**: `cgc mcp start` is registered as an MCP server in every agent's Claude Code config
3. **RALPH ASSESS step**: The perpetual loop tells agents to run `cgc find pattern <topic>` before scanning the codebase

### Install

Included in the Docker image via `pip install codegraphcontext`. For manual setup:

```bash
pip install codegraphcontext
cgc init   # Initialize KuzuDB (first time only)
```

---

## ECC Bundle

Jarvis vendors a curated subset of the [ECC](https://github.com/affaan-m/ECC)
project (MIT, attribution in `THIRD_PARTY_LICENSES.md`) so spawned
agents have a richer skill + agent + tool surface than the office's
native set provides:

- **22 skills** at `skills/ecc/<name>/SKILL.md` — `agent-eval`,
  `agentic-engineering`, `api-design`, `architecture-decision-records`,
  `backend-patterns`, `browser-qa`, `code-tour`, `codebase-onboarding`,
  `coding-standards`, `context-budget`, `continuous-learning`,
  `deep-research`, `deployment-patterns`, `e2e-testing`, `eval-harness`,
  `frontend-design-direction`, `git-workflow`,
  `make-interfaces-feel-better`, `mcp-server-patterns`,
  `security-review`, `tdd-workflow`, `verification-loop`.
- **4 Task-tool subagents** at `internal/office/ecc_agents/*.md` —
  `code-architect`, `security-reviewer`, `pr-test-analyzer`,
  `code-simplifier`. Laid into each worktree's `.claude/agents/` at
  spawn time.
- **3 slash commands** at `internal/office/ecc_commands/*.md` —
  `/security-scan`, `/test-coverage`, `/refactor-clean`. Laid into
  each worktree's `.claude/commands/`.
- **3 MCP servers** wired per spawn — `context7`,
  `sequential-thinking`, `shadcn`. All gated on `npx` (Node 18+) on
  PATH; missing-Node hosts silently skip these entries.

Per-team opt-out: pass `disableEcc: true` in the spawn request and the
bundle (agents + commands + MCPs) is excluded. Full inventory and
refresh workflow: `docs/ECC.md`.

---

## Tool Policy

Every spawned team enforces an operator-controlled allow/deny matrix
before every tool call, via Claude Code's `PreToolUse` hook. The
default policy blocks the obvious — `rm -rf /` (excluding `/tmp` and
`/var`), `sudo`, `curl … | sh`, and writes to `/etc/`, `/usr/`,
`/System/`, or `~/.ssh/`. Denied calls return a structured
`permissionDecisionReason` to the agent so it can re-plan rather than
silently dropping work.

Policies live in `~/.jarvis/tool-policy.json` and are operator-editable
from the dashboard's Settings page or via the API:

```bash
# Read current policy
curl http://127.0.0.1:3555/api/office/tool-policy

# Push a custom policy (CSRF token + auth cookie required)
curl -X PUT http://127.0.0.1:3555/api/office/tool-policy \
  -H 'Content-Type: application/json' \
  -d '{"denyPatterns":[{"tool":"Bash","pattern":"rm\\s+-rf\\s+/"}],"allowOutsideWorktree":false}'
```

Implementation: `internal/claudehooks/tool_policy.go` (store +
PreToolUse hook), `internal/web/handler/tool_policy.go` (HTTP).

---

## Z.AI Coding Plan

Jarvis supports [Z.AI](https://z.ai) as an Anthropic-compatible provider, enabling GLM models via the Coding Plan endpoint.

### Configuration

In the Jarvis UI, go to Settings > Accounts and select **Z.AI (GLM)** as the provider.

| Setting | Description | Default |
|---|---|---|
| `zaiApiKey` | Z.AI API key | — |
| `zaiModel` | Primary model (Sonnet/Opus equivalent) | `glm-5.1` |
| `zaiHaikuModel` | Lightweight model (Haiku equivalent) | `glm-4.5-air` |
| `zaiBaseUrl` | API base URL | `https://api.z.ai/api/anthropic` |

### Model Mapping

Claude Code maps model tiers to these environment variables at session spawn:

```
ANTHROPIC_DEFAULT_SONNET_MODEL = zaiModel     (glm-5.1)
ANTHROPIC_DEFAULT_OPUS_MODEL   = zaiModel     (glm-5.1)
ANTHROPIC_DEFAULT_HAIKU_MODEL  = zaiHaikuModel (glm-4.5-air)
ANTHROPIC_BASE_URL             = zaiBaseUrl
ANTHROPIC_AUTH_TOKEN           = zaiApiKey
API_TIMEOUT_MS                 = 3000000
```

Per-member model overrides are supported: individual team members can be configured to use a different model than the team default.

### Rate-Limit-Aware Model Allocation

Z.AI's Coding Plan imposes concurrent-request limits per model tier (e.g., 2 concurrent requests for GLM-5.1, 2 for GLM-5, 3 for GLM-4.7, 5 for GLM-4.7-flash). With a 5-agent team all running on GLM-5.1, you'd rapidly hit the rate limit and agents would stall.

Jarvis' `AllocateZAIModels()` distributes agents across model tiers at spawn time so the team stays under rate limits while keeping the most important agents on the best models:

- **Strategic-tier agents** (leaders) get the premium model first (GLM-5.1)
- **Remaining slots** on each tier are filled by workers in priority order
- **Overflow workers** are downgraded to the next tier (GLM-5 → GLM-4.7 → GLM-4.7-flash)

Example allocation for a 5-agent team:

```
Captain (strategic) → glm-5.1   [premium, slot 1 of 2]
Dash    (worker)    → glm-5.1   [premium, slot 2 of 2]
Shield  (worker)    → glm-5     [sonnet, slot 1 of 2]
Stack   (worker)    → glm-5     [sonnet, slot 2 of 2]
Ops     (worker)    → glm-4.7   [sonnet-lite, slot 1 of 3]
```

Each agent gets its own `ANTHROPIC_DEFAULT_SONNET_MODEL` environment variable injected via `claude.WithEnv(...)`, so Claude Code transparently uses the allocated tier.

---

## UI Features

The embedded frontend provides a single-page dashboard served from `internal/web/static/`.

### Dashboard

- **Session cards** with live status indicators (working, idle, stopped), token counts, color labels, and role badges
- **Agent cards** in the office view showing task queue, tier badge, recovery status, and last-activity timestamp
- **Progress ring** per session showing completion percentage of the current task queue
- **Activity strip** — three rolling-window widgets (cost-7d, throughput-24h, agent-health) above the team-overview row, with inline SVG sparklines
- **Per-agent cost-per-turn sparkline** in the detail panel, sourced from `/api/office/agent/{sessionId}/usage-series`
- **React beta dashboard** at `/office-v2` — opt-in, built from `internal/web/spa/` (Vite + TypeScript + Tailwind + Tanstack Query). Run `npm install && npm run build` inside the SPA directory to ship it; without the build, `/office-v2` serves a styled placeholder page

### Command Palette (⌘K) — global

Press `⌘K` (or `Ctrl+K`) from anywhere on `/office` to open the global command palette. Renders as an overlay with fuzzy search + keyboard navigation (`↑↓` move, `↵` run, `esc` close). Default actions:

- Navigate: open Office dashboard, open Terminal sessions
- Team: spawn team, pause team, resume team
- Surfaces: open snapshots, open handoffs, open whiteboard, view tool policy
- Search: search messages + tasks

Pages may register additional actions via `window.CommandPalette.register({id, label, group, hint?, run})`. Implementation at `internal/web/static/public/js/command-palette.js`. The terminal page (`/`) keeps its own session-switcher palette unchanged.

### Status Filters

Filter sessions and agents by status: All, Working, Idle, Stopped, Recovering. Filters persist across page reloads.

### Diff Viewer

Inline diff viewer for session output changes. Triggered automatically when an agent completes a write-to-file action. Shows before/after with syntax highlighting.

### Toast Notifications

Non-blocking toast system for: task assignments, agent joins/leaves, recovery events, standup summaries, and Telegram connectivity changes.

### Real-Time Updates

All state changes are pushed via Server-Sent Events on `GET /api/sse`. The broker supports up to 100 concurrent global connections and 10 per session, with per-client write goroutines for non-blocking delivery.

**SSE Event Types (37+):**

| Event | Description |
|-------|-------------|
| `session:created` | New PTY session started |
| `session:updated` | Session status/metadata changed |
| `session:deleted` | Session stopped and removed |
| `session:idle` | Agent went idle |
| `session:working` | Agent started working |
| `session:output` | Terminal output chunk |
| `office:memberJoined` | Agent registered with office |
| `office:memberUpdated` | Agent status/data changed |
| `office:memberLeft` | Agent unregistered |
| `office:message` | Inter-agent message |
| `office:standupStarted` | Standup meeting began |
| `office:standupMessage` | Standup transcript entry |
| `office:standupEnded` | Standup meeting ended |
| `office:taskQueueGenerated` | New task queue created |
| `office:taskQueueUpdated` | Task status changed |
| `office:bugTaskCreated` | Bug task filed |
| `office:bugTaskUpdated` | Bug task status changed |
| `office:bugTaskResolved` | Bug task resolved |
| `office:meetingStarted` | Brainstorm/ad-hoc meeting started |
| `office:meetingEnded` | Meeting ended |
| `office:fileLockChanged` | File lock acquired/released |
| `office:agentStuck` | Agent unresponsive |
| `office:agentRestarted` | Agent auto-restarted |
| `office:missionUpdated` | Team mission changed |
| `office:personalityUpdated` | Agent personality overridden |
| `office:autoStandupScheduled` | Auto-standup timer set |
| `office:settingsUpdated` | Office settings changed |
| `office:wrapUpStarted` | Wrap-up sequence initiated |
| `office:shutdown` | Office shutdown |
| `office:taskReviewed` | Ultrareview findings posted for a completed task |
| `office:snapshotRestored` | Snapshot restored (includes `forked: true` when fork mode used) |
| `office:remoteSessionCreated` | Operator spawned a claude.ai web session via `/api/office/spawn-remote` |
| `office:claudeSessionStopped` | Operator stopped a Claude session via `/api/office/claude-sessions/{sid}/stop` |
| `respawn:stateChanged` | Respawn controller state transition |
| `ralph:updated` | RALPH tracker event |
| `runsummary:updated` | Run summary parsed |

### Agent Personality Icons

Each agent has a unique SVG icon based on their role/personality (24 icons across 4 templates). Icons are Feather-style outlines rendered in the avatar slot: crown (manager), compass (architect), palette (designer), code brackets (frontend), server (backend), wrench (devops), shield (QA), brain (AI lead), atom (React), rocket (ops), and more. Role-based fallbacks match unknown agents by keyword.

### View Modes

Toggle between **grid view** (responsive card grid, default) and **compact list view** via the view-mode toggle in the agents panel header. Preference persists in localStorage.

### Quick Actions

Hover over any agent card to reveal quick-action buttons:
- **Open Details** -- expand agent modal with memory, personality override, task queue
- **Send Prompt** -- inline prompt composer that routes through CSRF-protected `postJson`
- **Jump to Terminal** -- navigate to the session's terminal tab

### Nav Tab Badges

Tab icons show live count badges:
- **Agents tab**: count of active agents
- **Tasks tab**: count of in-progress tasks across team
- **Chat tab**: unread inter-agent message count
- **Standup tab**: green dot when a standup is active

### Agent Personality Icons

Each agent has a unique **SVG icon** based on their role/personality:
- **37 custom icons** mapped to specific agent IDs (crown for managers, compass for architects, palette for designers, code brackets for frontend, server for backend, wrench for devops, shield for QA, brain for AI leads, atom for React, rocket for ops, microscope for researchers, prism for analysts, etc.)
- **11 role-based fallback icons** matched by keyword in the role name (design, frontend, backend, devops, qa, lead, test, security, research, analyst, writer)

Icons render in Feather-style outlines in the agent card avatar slot. The mapping is defined in `office.js` `AGENT_ICONS` constant and degrades to an emoji fallback if neither ID nor role matches.

### Chat Message Persistence

Inter-agent messages (sent via `cm message` or the `office_send_message` MCP tool) are:
1. Appended to an in-memory `OfficeMessage` slice (capped at 50 via FIFO eviction)
2. Broadcast over SSE as `office:message` events for real-time display
3. **Persisted to disk** at `~/.jarvis/office-memory/messages.json` after each send (async)
4. **Loaded on server restart** so the chat history survives crashes
5. **Re-rendered on periodic refresh** -- the frontend's 30-second state-sync call invokes `renderMessages()` so stale DOM is refreshed

### Standup Transcript Persistence

Standup conversations are similarly persisted:
- **Server-side**: Completed standups are saved to `~/.jarvis/office-memory/standups/{standupId}.json`
- **During standup**: The server returns `standupTranscript` in `GetOfficeState` whenever the standup has responses, regardless of active state
- **Frontend**: Periodic refresh preserves the local transcript if the server sends an empty one, and `renderStandup` is called from the unified `render()` function
- **After standup**: The last completed standup is shown as "Previous Standup" in the standup panel until a new one starts

### Perpetual Mode Toggle

The spawn modal includes a **Perpetual Improvement Mode** checkbox (default: checked). When enabled, agents run the RALPH continuous-improvement loop and never idle. When disabled, agents complete assigned tasks and stop, useful for goal-focused work.

---

## Session Management (PTY + SDK)

Jarvis supports **two session types** via a shared `SessionStore` interface:

### SDK Sessions (default for office agents)

SDK sessions use `claude.NewSession()` from [claude-agent-sdk-go](https://github.com/shaharia-lab/claude-agent-sdk-go). The subprocess stays alive across all turns, and prompts are delivered via `Session.Send()`. See the **Claude Agent SDK (Go) Sessions** section above for details.

Key components:
- **`SDKSession`** in `internal/pty/sdk_session.go` -- wraps `claude.Session`, manages event consumer goroutine, provides `SendPrompt`/`WriteInkPrompt`/`WriteViaScreen` methods for compatibility with PTY code paths
- **`eventConsumer`** goroutine -- ranges over `session.Events()`, emits `EventIdle`/`EventWorking`/`EventTokens`/`EventExit`/`EventTerminal` to the session's event channel
- **Single reader pattern** -- one long-lived goroutine consumes all events, avoiding the goroutine-per-read race that the original PTY implementation had
- **Graceful shutdown** -- `Stop()` cancels the context, waits for the event consumer via `sync.WaitGroup` with a 5-second timeout, then closes the event channel

### PTY Sessions (fallback / interactive shells)

PTY sessions use `creack/pty` to spawn a child process attached to a pseudo-terminal. Jarvis uses these for interactive shell sessions and as a fallback if SDK session creation fails.

**WriteInkPrompt**: Claude Code uses Ink (React-based terminal UI) which requires special input handling. `WriteInkPrompt` chunks text at 700 bytes with inter-chunk delays, then sends CR separately -- Ink's text input doesn't respond to CR embedded in the same write as text content. This is critical for kickstart prompt delivery.

**Screen-scraping idle detection**: PTY mode strips ANSI sequences and pattern-matches on Claude's terminal output to detect idle/working state. This is inherently brittle (hence the SDK migration).

### Respawn Controllers

Each session has a `respawn.Controller` state machine that tracks: idle detection, health checks, automatic restart on crash, and configurable retry policies with exponential backoff (3 tiers: restart, restart with reset, full recreate).

### Ralph Trackers

The RALPH (Recognize, Assess, Log, Plan, Handle) tracker monitors each session's output for loop patterns, excessive tool calls, and stalled progress. It acts as a circuit breaker -- if an agent enters a RALPH loop (repeating the same failed action), the tracker triggers intervention via the respawn controller.

---

## API Reference

All endpoints are prefixed with `/api`. Mutating endpoints (POST/PUT/DELETE) require a CSRF token from `GET /api/csrf-token` except agent callbacks (called by `cm` CLI) and `POST /api/hook-event`.

### Session Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/sessions` | List all sessions |
| `POST` | `/api/sessions` | Create session |
| `GET` | `/api/sessions/{id}` | Get session |
| `DELETE` | `/api/sessions/{id}` | Delete session |
| `POST` | `/api/sessions/{id}/input` | Send input to session |
| `GET` | `/api/sessions/{id}/terminal` | Get terminal buffer |
| `GET` | `/api/sessions/{id}/output` | Get text output |
| `GET` | `/api/sessions/{id}/git-status` | Git status |
| `GET` | `/api/sessions/{id}/build-status` | Build status |
| `GET` | `/api/sessions/{id}/test-status` | Test run status |
| `POST` | `/api/sessions/{id}/respawn/start` | Start auto-respawn |
| `POST` | `/api/sessions/{id}/context/trim` | Trim context window |
| `POST` | `/api/sessions/{id}/plan/checkpoint` | Checkpoint plan version |
| `GET` | `/api/sessions/{id}/plan/history` | Plan version history |
| `POST` | `/api/sessions/{id}/plan/rollback/{version}` | Rollback to plan version |

### Office Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/office/status` | Office state snapshot |
| `GET` | `/api/office/templates` | Available team templates |
| `POST` | `/api/office/spawn-team` | Spawn the configured team |
| `POST` | `/api/office/shutdown-team` | Shutdown all agents |
| `GET` | `/api/office/task-queues` | All agent task queues |
| `GET` | `/api/office/task-queues/{sessionId}` | Single agent task queue |
| `GET` | `/api/office/file-locks` | Active file locks |
| `GET` | `/api/office/messages` | Office message log |
| `POST` | `/api/office/message` | Send office message |
| `GET` | `/api/office/metrics` | Agent metrics snapshot |
| `GET` | `/api/office/audit-log` | Audit log entries |
| `GET` | `/api/office/learnings` | All agent learning stores |
| `GET` | `/api/office/standup` | Current standup state |
| `POST` | `/api/office/standup` | Trigger standup |
| `GET` | `/api/office/standup/history` | Standup history |
| `POST` | `/api/office/wrap-up` | Begin wrap-up sequence |
| `POST` | `/api/office/wrap-up/cancel` | Cancel wrap-up |
| `GET` | `/api/office/code-graph` | Code graph snapshot |
| `POST` | `/api/office/code-graph/build` | Rebuild code graph |
| `GET` | `/api/office/memory/{sessionId}` | Agent persistent memory |
| `POST` | `/api/office/brainstorm` | Trigger brainstorm session |

#### Snapshots, search, and integrations

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/office/snapshots` | List saved team snapshots |
| `POST` | `/api/office/snapshots` | Create a snapshot of the current team |
| `POST` | `/api/office/snapshots/{name}/restore` | Restore a snapshot. Add `?fork=true` for fork mode (keeps the original team running, spawns a parallel team resuming each member's Claude session via `--fork-session`) |
| `DELETE` | `/api/office/snapshots/{name}` | Delete a snapshot |
| `GET` | `/api/office/agent/{sessionId}/usage-series` | Recent per-turn cost series + averages (powers the detail-panel sparkline) |
| `POST` | `/api/office/search` | Text search across messages + tasks (`{query, types?, limit?}`); returns scored hits with kind, snippet, agent name, timestamp |
| `GET` | `/api/office/tool-policy` | Current tool-policy deny matrix (operator-readable) |
| `PUT` | `/api/office/tool-policy` | Replace the tool-policy matrix; takes effect on the next PreToolUse hook fire |
| `GET` | `/api/office/pr/{number}` | Team + sessions linked to a GitHub PR number |
| `POST` | `/api/office/spawn-remote` | Spawn a claude.ai web session via `claude --remote`. Returns `{url, sessionId}` |
| `GET` | `/api/office/claude-sessions/{sid}/logs` | Tail Claude logs for a session (text/plain) |
| `POST` | `/api/office/claude-sessions/{sid}/stop` | Stop a Claude session (`claude stop <sid>`) |
| `GET` | `/api/office/claude-sessions/{sid}/attach-command` | Return argv for `claude attach` so operators can re-enter a session from a terminal |

### Agent Callback Endpoints (auth-exempt, called by `cm` CLI and `jarvis-mcp`)

These endpoints are placed outside the auth middleware group so in-session agents can reach them using only their injected `JARVIS_SESSION_ID`. They are not exposed in the authenticated UI routes.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/office/agent/tasks` | Get agent's task queue |
| `GET` | `/api/office/agent/team-roster` | Get team roster |
| `GET` | `/api/office/agent/file-locks` | List file locks |
| `GET` | `/api/office/agent/skills` | List shared skills |
| `GET` | `/api/office/agent/skills/{name}` | Read a skill |
| `GET` | `/api/office/agent/skills/search` | Search skills |
| `GET` | `/api/office/agent/memory` | Read agent memory |
| `POST` | `/api/office/agent/memory` | Write agent memory |
| `POST` | `/api/office/agent/memory/append` | Append to memory |
| `POST` | `/api/office/agent/standup/response` | Submit standup response |
| `POST` | `/api/office/agent/message` | Send inter-agent message |
| `POST` | `/api/office/agent/task-start` | Mark task in-progress |
| `POST` | `/api/office/agent/task-complete` | Mark task complete |
| `POST` | `/api/office/agent/task-ack` | Acknowledge task |
| `POST` | `/api/office/agent/task-progress` | Report progress (with `isBlocked` flag for blockers) |
| `POST` | `/api/office/agent/task/self-add` | Self-generate a task |
| `POST` | `/api/office/agent/task/triage` | Transfer task to teammate |
| `POST` | `/api/office/agent/brainstorm/response` | Submit brainstorm proposals |
| `POST` | `/api/office/agent/leader-tasks/response` | Submit leader task assignments |
| `POST` | `/api/office/agent/reflection/response` | Submit post-task learning |
| `POST` | `/api/office/agent/curiosity/discovery` | Report curiosity discovery |
| `POST` | `/api/office/agent/file-lock` | Acquire file lock |
| `POST` | `/api/office/agent/skill-create` | Create/update skill |
| `DELETE` | `/api/office/agent/skills/{name}` | Delete a skill |

### Config Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/config` | Server configuration |
| `PUT` | `/api/config` | Update configuration |
| `GET` | `/api/settings` | Application settings |
| `PUT` | `/api/settings` | Update settings |
| `GET` | `/api/settings/accounts` | Provider accounts |
| `POST` | `/api/settings/accounts` | Add provider account |
| `GET` | `/api/stats` | Server statistics |
| `GET` | `/api/token-stats` | Token usage statistics |

### Monitoring Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/status` | Full status payload |
| `GET` | `/api/csrf-token` | Fetch CSRF token |
| `GET` | `/api/auth/status` | Auth status |
| `GET` | `/api/screens` | Screen list |
| `GET` | `/api/subagents` | Subagent list |
| `GET` | `/api/system/stats` | System memory and CPU stats |
| `GET` | `/api/sse` | SSE event stream |

---

## Testing

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Run benchmarks
make bench
```

**600+ tests** across 30 packages. Key test coverage areas:

| Package | Coverage Focus |
|---|---|
| `internal/office` | Manager lifecycle, task queues, standup, brainstorm, reflection, locks, orchestration, learning, metrics |
| `internal/pty` | Session creation, PTY I/O, SDK session lifecycle, idle detection, buffer management |
| `internal/respawn` | State machine transitions, signal aggregation, timer pool |
| `internal/ralph` | RALPH loop tracking, circuit breaker trips, status parsing |
| `internal/telegram` | Bot commands, message queue, streaming, webhook |
| `internal/web/handler` | HTTP handler responses, monitoring endpoints |
| `internal/web/sse` | Broker subscribe/broadcast, client write goroutines, event batching |

All tests pass with `go test -race ./...` (full race-detector coverage).

---

## Security & Resilience

Jarvis has undergone **10 comprehensive code review rounds** with **95+ bug fixes** across security, correctness, concurrency, and resource management. Every change is race-tested and the codebase is hardened against common failure modes.

### Fix Categories (10 rounds, 95+ total)

| Round | Fixes | Focus |
|-------|-------|-------|
| 1 | 15 | Goroutine leaks, data races, crash fixes, SDK session env |
| 2 | 20 | Correctness, frontend timers, resource management |
| 3 | 17 | API contract mismatches, TOCTOU races, JSON parsing |
| 4 | 8 | XSS, task pointer races, Stop() synchronization |
| 5 | 8 | postJson regressions, nil guards, brainstorm lock promotion |
| 6 | 10 | Triage contract fix, CSRF retry, null guards |
| 7 | 8 | N+1 disk reads, wg.Done ordering, raw fetch cleanup |
| 8 | 1 | PATCH handler CSRF freshness |
| 9 | 4 | Telegram webhook chi routing, XSS in quieter files |
| 10 | 4 | PTY reader leak, transcript position, XSS regression |

### Security Hardening

- **CSRF protection** with automatic retry on token expiry (postJson/putJson/deleteJson helpers)
- **Path traversal prevention**: `safeName()` validator (`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, max 128 chars) applied to all user-supplied names (skills, cases, memory paths, task IDs)
- **Shell injection prevention**: `/api/run` endpoint uses `exec.Command` with split args, never `sh -c`
- **State file permissions**: `~/.jarvis/state.json` written with 0600 mode (contains API keys)
- **Auth middleware**: Password authentication with session cookies, rate-limited login attempts, cached `IsPasswordConfigured` to avoid disk reads on every request
- **XSS prevention**: All HTML insertion in frontend uses escaped helpers or textContent; Ralph wizard preview escapes `taskDescription` and `completionPhrase`; expand history renders escape all server-supplied fields
- **Agent endpoint isolation**: `/api/office/agent/*` routes are auth-exempt but require valid `sessionId` from the `cm` CLI's JARVIS_SESSION_ID env var; path-parameter routing uses `chi.URLParam` for consistency

### Concurrency Safety

- **Zero goroutine leaks**: All background goroutines have explicit `done` channels or `sync.WaitGroup` coordination. `StartIdleMonitoring`/`startAutoStandup` use done channels in their select loops so `Stop()` reliably terminates them.
- **Lock ordering discipline**: `m.mu` (office manager) is always acquired before any `agent.mu` (agent state), never the reverse. `HandleAgentDeath` deep-copies tasks before reassignment to avoid racing on shared pointers.
- **Race-free agent state**: `SetSDKWorking`/`GetSDKWorking` and `SetPersonalityOverride`/`GetPersonalityOverride` wrap mutations in the agent mutex; concurrent readers get stable snapshots.
- **Non-blocking SSE delivery**: The SSE broker uses per-client write goroutines with bounded buffers; slow clients don't block the broadcast path.
- **Brainstorm cooldown TOCTOU fix**: The cooldown check and `lastBrainstormAt` update happen under a single `m.mu.Lock()` so two concurrent goroutines can't launch duplicate brainstorms.
- **SDK event consumer shutdown**: `eventConsumer`'s `defer s.wg.Done()` is the outermost defer so `Stop()` only unblocks after the events channel is closed and the final log line is written.

### Resource Management

- **Bounded buffers**: Terminal buffer (2MB trim to 1.5MB), text output (1MB trim to 768KB), message log (50 entries FIFO), activity log (200 entries)
- **Agent memory caps**: 50 learnings, 25 skills, 10 distilled insights per agent (auto-distillation at 30 learnings)
- **I/O limits**: `jarvis-mcp` wraps HTTP response bodies with `io.LimitReader(resp.Body, 32<<20)` to cap at 32MB
- **Context timeouts**: CGC indexing has a 5-minute timeout, state file writes use atomic tmp-file rename with error logging
- **Single PTY reader**: Only one goroutine reads from each PTY file descriptor, eliminating the reader-per-iteration leak pattern
- **Transcript position accounting**: Uses `f.Seek(0, io.SeekCurrent)` instead of `len(line)+1` to handle CRLF line endings correctly

---

## Development

### Prerequisites

- Go 1.25+
- `claude` CLI (for running agents)
- Docker + Docker Compose (for production deployment)

### Build Targets

| Target | Description |
|---|---|
| `make build` | Build `jarvis` binary (current platform) |
| `make build-all` | Cross-compile to all four platforms into `dist/` |
| `make test` | Run tests with 60s timeout |
| `make test-race` | Run tests with race detector |
| `make bench` | Run benchmarks with memory profiling |
| `make clean` | Remove binary and dist/ |

### Project Layout Conventions

- Packages are organized by domain, not by technical layer
- All public APIs have GoDoc comments
- Tests live alongside source files (`*_test.go`)
- No CGO — pure Go only
- Interfaces are defined in the consuming package, not the providing package
- The `office` package is the largest and most critical — changes there require full test coverage

### Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Write tests for new behavior
4. Run `make test-race` and `go vet ./...` before submitting
5. Open a pull request with a description of what changed and why
