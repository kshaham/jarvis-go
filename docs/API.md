# 📡 API Reference

> Full HTTP API for jarvis-go. Linked from the [README](../README.md).

All endpoints are prefixed with `/api`. Mutating endpoints (POST/PUT/DELETE) require a CSRF token from `GET /api/csrf-token` except agent callbacks (called by `cm` CLI) and `POST /api/hook-event`.

## Session Endpoints

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

## Office Endpoints

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
| `GET` | `/api/office/project/invariants` | Read the standing project invariants |
| `POST` | `/api/office/project/invariants` | Add/replace a project invariant |
| `DELETE` | `/api/office/project/invariants` | Remove a project invariant |

### Snapshots, search, and integrations

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

## Agent Callback Endpoints (auth-exempt, called by `cm` CLI and `jarvis-mcp`)

These endpoints are placed outside the auth middleware group so in-session agents can reach them using only their injected `JARVIS_SESSION_ID`. They are not exposed in the authenticated UI routes. They are registered under both the `/office/agent` and `/claude/hooks` mounts via a shared route helper so the `cm` CLI and `jarvis-mcp` paths always resolve.

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
| `POST` | `/api/office/agent/task/self-add` | Self-generate a task (rejected with HTTP 403 for blocked categories — auth/payment/deploy/migration/dependency-upgrade/architecture-rewrite) |
| `POST` | `/api/office/agent/task/triage` | Transfer task to teammate |
| `POST` | `/api/office/agent/task/phase` | Record the engineering phase of a task (7-step mode; advisory, never changes status) |
| `POST` | `/api/office/agent/task/criteria` | Set/add/remove acceptance criteria on a task |
| `POST` | `/api/office/agent/task/handoff` | Initiate a structured task handoff to a teammate |
| `POST` | `/api/office/agent/task/handoff-accept` | Accept an incoming handoff |
| `POST` | `/api/office/agent/task/handoff-reject` | Decline an incoming handoff |
| `POST` | `/api/office/agent/brainstorm/response` | Submit brainstorm proposals |
| `POST` | `/api/office/agent/leader-tasks/response` | Submit leader task assignments |
| `POST` | `/api/office/agent/reflection/response` | Submit post-task learning |
| `POST` | `/api/office/agent/curiosity/discovery` | Report curiosity discovery |
| `POST` | `/api/office/agent/file-lock` | Acquire file lock |
| `POST` | `/api/office/agent/skill-create` | Create/update skill |
| `DELETE` | `/api/office/agent/skills/{name}` | Delete a skill |

## Config Endpoints

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

## Monitoring Endpoints

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
