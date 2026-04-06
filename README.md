# Jarvis Go

> Multi-agent orchestration platform — manage teams of autonomous Claude Code agents working on real codebases.

![Go 1.25](https://img.shields.io/badge/go-1.25-blue) ![License MIT](https://img.shields.io/badge/license-MIT-green) ![Tests 311+](https://img.shields.io/badge/tests-311%2B-brightgreen)

---

## Overview

Jarvis Go is a full port of the Node.js Jarvis server, implemented in pure Go with zero CGO dependencies. It runs a web server that manages PTY sessions hosting Claude Code agents, coordinates them as a virtual office team, and provides a clean browser-based dashboard for monitoring and control.

Key capabilities:
- Spawn teams of Claude Code agents in isolated PTY sessions with role-specific kickstart prompts
- Autonomous monitoring loop with 12 functions: stuck detection, reflection, curiosity, task triage, water-cooler chat, and more
- Self-improvement engine: agents reflect on completed tasks, build a learning store, distill insights, and share skills across the team
- 5-column kanban for task tracking with dependencies, priorities, and leader assignment
- Telegram bot integration for remote monitoring and session control
- MCP server exposing 30 tools for external orchestration
- Z.AI Coding Plan support (GLM models via Anthropic-compatible API)

---

## Architecture

```
jarvis-go/
├── cmd/
│   ├── jarvis/          # Main web server binary
│   ├── jarvis-mcp/      # MCP stdio server (30 tools)
│   ├── cm/                 # Agent CLI (injected into sessions)
│   └── ptyserver/          # Standalone PTY server
│
└── internal/
    ├── office/             # Virtual office: manager, agents, tasks, learning, recovery
    ├── pty/                # PTY session management, screen buffering, idle detection
    ├── respawn/            # Automatic session respawn state machine
    ├── ralph/              # RALPH loop tracker and circuit breaker
    ├── subagent/           # Subagent watcher (Claude Code sub-tasks)
    ├── telegram/           # Telegram bot, commands, streaming, queue
    ├── templates/          # Session/team configuration templates
    ├── state/              # Persistent application settings (JSON)
    ├── cases/              # Saved prompt cases
    ├── collections/        # Shared data structures
    ├── integrations/       # External integration state
    ├── runsummary/         # Run summary parsing and storage
    ├── transcript/         # Transcript read/parse utilities
    ├── cleanup/            # Graceful shutdown helpers
    ├── filewatcher/        # File system watcher for hot-reload
    └── web/
        ├── handler/        # HTTP handlers (session, office, config, monitoring)
        ├── sse/            # Server-Sent Events broker and client pool
        ├── middleware/     # Auth, CSRF, request logging
        ├── auth/           # Session authentication
        ├── request/        # Request parsing helpers
        └── static/         # Embedded frontend assets
```

**22 packages**, ~31,700 lines of Go.

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

### Run

```bash
./jarvis web                    # Start on port 3000
./jarvis web --port 3555        # Custom port
./jarvis web --https            # TLS mode
./jarvis version
```

Open `http://localhost:3000` in your browser.

### Docker Deployment

The recommended production setup uses Docker Compose:

```bash
# From the jarvis/ project root (not jarvis-go/)
docker compose up -d
```

The container exposes port **3555** by default. See `docker-compose.yml` for volume mounts — workspaces, Claude credentials, skills, and configuration are all mounted from the host.

### Environment Variables

| Variable | Description | Default |
|---|---|---|
| `JARVIS_API_URL` | Server URL (used by `cm` CLI inside sessions) | — |
| `JARVIS_SESSION_ID` | Current session ID (set automatically per agent) | — |
| `GEMINI_API_KEY` | Gemini API key (for Nano Banana image generation) | — |
| `HEADLESS_BROWSER_HOST` | Chrome headless host for agent browser testing | `headless-shell` |
| `HEADLESS_BROWSER_PORT` | Chrome DevTools port | `9222` |

---

## Office Agent System

### Team Templates

Jarvis ships with prebuilt team configurations accessible via `GET /api/office/templates`. Templates define member roles, personalities, skills, and tier assignments. Custom teams can be created and saved through the UI.

Each team member has:
- **Role** — e.g., Frontend Engineer, Backend Engineer, QA Engineer, Tech Lead, DevOps Engineer
- **Tier** — `strategic` (leader) or `worker`
- **Personality** — multi-paragraph character description injected at session start
- **Skills** — list of domain competencies
- **Color** — visual identifier in the dashboard

### Spawn Flow

1. User clicks "Spawn Team" in the UI (or calls `POST /api/office/spawn-team`)
2. For each team member, the server creates a PTY session running `claude`
3. After session stabilizes, `BuildKickstartPrompt` generates the full onboarding prompt and injects it
4. Each agent receives: team mission, previous memory, learned knowledge, identity, personality, skills, team relationships, task queue instructions, file lock protocol, communication tools, and `cm` CLI reference
5. Idle monitoring starts on a 60-second tick

### Kickstart Prompt Sections

| Section | Description |
|---|---|
| Team Mission | Project-level goal shared by all members |
| Previous Work Memory | Persisted memory from prior sessions via `cm memory` |
| Learned Knowledge | Distilled insights from past `cm reflect` calls |
| Identity + Personality | Deep role immersion paragraph |
| Team Relationships | Names and roles of all teammates |
| Task Queue | Current assigned tasks |
| File Lock Protocol | How to use `cm lock`/`cm unlock` before editing |
| Communication | How to use `cm message`, `cm standup`, `cm brainstorm` |
| cm CLI Reference | Full command reference inline |

### Standup

Automated standup is triggered on a configurable interval (default: 4 hours) or manually via `POST /api/office/standup`. Each agent receives a prompt asking for status and blockers, and responds via `cm standup respond <content>`. The office manager collects responses with a 5-minute timeout, sends nudges every 60 seconds (up to 3 times), and publishes the summary.

### Brainstorm

`POST /api/office/brainstorm` initiates a collaborative planning session. The leader receives task proposals from all workers via `cm brainstorm respond <proposals>`. The brainstorm system uses a role-based collaboration map to route proposals to relevant teammates.

### Wrap-Up

`POST /api/office/wrap-up` sends a graceful shutdown prompt to all agents instructing them to commit work, write memory, and exit cleanly. The wrap-up state is tracked so the UI can display progress. `POST /api/office/wrap-up/cancel` aborts the sequence.

### Shutdown

`POST /api/office/shutdown-team` terminates all agent sessions after an optional wrap-up.

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

When building the kickstart prompt, `BuildCrossAgentContext` loads learning stores from all other agents on the team (excluding self) and includes a summary. This lets workers benefit from techniques discovered by teammates without manual coordination.

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

The `jarvis-mcp` binary implements the Model Context Protocol (MCP) over stdio transport. It exposes 30 tools for external orchestrators (e.g., Claude Desktop, another Claude Code agent) to control Jarvis programmatically.

### Session Tools

| Tool | Description |
|---|---|
| `create_session` | Create a new PTY session |
| `list_sessions` | List all active sessions |
| `get_session` | Get session details by ID |
| `stop_session` | Stop a session |
| `send_to_session` | Send input to a session |
| `get_session_output` | Retrieve recent session output |

### Agent / Office Tools

| Tool | Description |
|---|---|
| `list_agents` | List office team members |
| `get_agent_status` | Get agent state and task queue |
| `get_agent_transcript` | Read agent conversation transcript |

### Task Tools

| Tool | Description |
|---|---|
| `add_task` | Assign a task to an agent |
| `list_tasks` | List tasks for an agent |
| `get_task` | Get task details |
| `remove_task` | Remove a task |

### Skill Tools

| Tool | Description |
|---|---|
| `list_skills` | List all shared skills |
| `get_skill` | Read a skill by name |
| `create_skill` | Create a shared skill |
| `search_skills` | Search skills by keyword |
| `delete_skill` | Remove a skill |

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

### Generation + Dynamic Tools

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

---

## UI Features

The embedded frontend provides a single-page dashboard served from `internal/web/static/`.

### Dashboard

- **Session cards** with live status indicators (working, idle, stopped), token counts, color labels, and role badges
- **Agent cards** in the office view showing task queue, tier badge, recovery status, and last-activity timestamp
- **Progress ring** per session showing completion percentage of the current task queue

### Command Palette (Cmd+K)

Press `Cmd+K` (or `Ctrl+K`) from anywhere to open the command palette. Supports fuzzy search over:
- Session actions (spawn, rename, delete, send input)
- Office actions (standup, brainstorm, wrap-up, spawn team)
- Settings and navigation

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
| `respawn:stateChanged` | Respawn controller state transition |
| `ralph:updated` | RALPH tracker event |
| `runsummary:updated` | Run summary parsed |

### Agent Personality Icons

Each agent has a unique SVG icon based on their role/personality (24 icons across 4 templates). Icons are Feather-style outlines rendered in the avatar slot: crown (manager), compass (architect), palette (designer), code brackets (frontend), server (backend), wrench (devops), shield (QA), brain (AI lead), atom (React), rocket (ops), and more. Role-based fallbacks match unknown agents by keyword.

### View Modes

Toggle between grid view (responsive card grid) and compact list view. Preference persists in localStorage.

### Quick Actions

Hover over any agent card to reveal quick-action buttons: open details modal, send prompt, or jump to terminal session.

### Nav Tab Badges

Tab icons show live count badges: in-progress tasks, unread messages, active standup indicator.

---

## PTY Session Management

### WriteInkPrompt

Claude Code uses Ink (React-based terminal UI) which requires special input handling. `WriteInkPrompt` chunks text at 700 bytes with inter-chunk delays, then sends CR separately -- Ink's text input doesn't respond to CR embedded in the same write as text content. This is critical for kickstart prompt delivery.

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

**311+ tests** across 22 packages. Key test coverage areas:

| Package | Coverage Focus |
|---|---|
| `internal/office` | Manager lifecycle, task queues, standup, brainstorm, reflection, locks, orchestration |
| `internal/pty` | Session creation, PTY I/O, idle detection, buffer management |
| `internal/respawn` | State machine transitions, signal aggregation, timer pool |
| `internal/ralph` | RALPH loop tracking, circuit breaker trips, status parsing |
| `internal/telegram` | Bot commands, message queue, streaming |
| `internal/web/handler` | HTTP handler responses, monitoring endpoints |
| `internal/web/sse` | Broker subscribe/broadcast, client write goroutines, event batching |

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
