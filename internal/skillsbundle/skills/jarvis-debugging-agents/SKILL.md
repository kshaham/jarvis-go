---
name: jarvis-debugging-agents
description: Cross-cutting operational guide for debugging stuck, unresponsive, looping, or context-full office agents. Covers the triage flow across PTY sessions, recovery tiers, ralph tracker circuit breakers, respawn controller, and context offload. The first skill to read when an agent misbehaves.
---

# Debugging Jarvis Agents

When an office agent misbehaves -- not responding, looping on the same task, burning context without progress, or getting repeatedly killed -- the fix requires cross-referencing several subsystems. No single file tells you the whole story. This skill is the operational triage guide.

## Cardinal rule

> Observe before intervening. Read `cm roster` + session status + recovery state before killing anything. Blind restarts mask root causes and lose in-memory state.

## Triage flowchart

Start here. Work top to bottom.

### 1. "My agent isn't responding"

**Check:** Is the session alive?
```bash
# From the jarvis container or via docker compose exec:
cm roster                              # shows sessionId + status for each agent
curl -s http://localhost:3555/api/sessions | jq '.[].status'
screen -ls                             # shows GNU screen sessions (PTY mode)
```

- **Session status = `idle`:** Agent is waiting for input. The office manager may not be sending prompts. Check standup/brainstorm cooldowns: `curl -s http://localhost:3555/api/office/status | jq '.standupState'`.
- **Session status = `busy`:** Agent IS running -- it's just slow or working on something big. Check `curl -s http://localhost:3555/api/sessions/<id> | jq '.tokenUsage'` for context consumption.
- **Session status = `stopped` or `error`:** The PTY or Claude process exited. Check the terminal buffer for the exit reason: `curl -s http://localhost:3555/api/sessions/<id>/text | tail -50`.
- **Session missing from roster:** It was never registered or was cleaned up. Check `docker compose logs jarvis | grep "session died"` for the cleanup event.

### 2. "My agent is stuck in a loop"

The agent is alive but repeating the same action without progress.

**Check the ralph tracker state:**
```bash
curl -s http://localhost:3555/api/sessions/<id>/ralph | jq
```

This shows: current todos, completion phrases detected, circuit breaker status.

**Circuit breaker logic** (`internal/ralph/circuit_breaker.go`):
The circuit breaker needs BOTH conditions met within a 30-second window:
1. A completion phrase detected (e.g., "done", "finished", "completed")
2. An exit signal from the todo tracker (all todos checked off)

If only ONE condition fires, the breaker doesn't trip and the loop continues.

**Common causes:**
- Agent says "done" but has unchecked todos → breaker doesn't trip → agent loops back to check todos → says "done" again → infinite loop.
  **Fix:** The agent needs to actually check off all `- [x]` items before declaring done.
- Agent's completion phrases don't match what it actually says. Check `internal/ralph/tracker.go` for the phrase list.
- Todo deduplication (85% similarity threshold) is collapsing distinct items into one, so the count never reaches the target.

### 3. "My agent's context is full"

The agent is getting slower, forgetting earlier turns, or producing confused output.

**Check token usage:**
```bash
curl -s http://localhost:3555/api/sessions/<id> | jq '.tokenUsage'
```

If `total` is >150k (on a 200k model), the agent is at ~75%+ and should have already compacted.

**Recovery steps (in order):**
1. Check if the agent has `office-context-budget` in its skill set -- agents who read it will self-compact at ~70%.
2. If the agent hasn't compacted, send it a nudge:
   ```bash
   cm message <sessionId> "You're at ~75% context. Offload to memory then /compact. See cm skill read office-context-budget."
   ```
3. If the agent is too far gone to read the message (>90%), the recovery loop will eventually kill and respawn it. Check the respawn state.

### 4. "My agent keeps getting killed and restarted"

The recovery loop (`internal/office/recovery.go`) detects stuck agents and escalates through 3 tiers.

**Recovery tiers** (from `internal/office/config.go`):
- **Tier 1** (attempt 1): Nudge -- send a gentle "are you stuck?" prompt.
- **Tier 2** (attempt 2): Stronger nudge with specific instructions.
- **Tier 3** (attempt 3+): Escalate to the leader (manager agent) for triage, or force-respawn.

**Configuration:**
- `StuckThreshold`: 300 seconds (5 min) of inactivity before declaring stuck.
- `RecoveryMaxAttempts`: 3 per recovery window.
- `RecoveryWindowDuration`: 1 hour (resets the attempt counter).

**Check the recovery state:**
```bash
curl -s http://localhost:3555/api/office/status | jq '.agents[] | {name, recoveryAttempts, lastPromptSentAt}'
```

**Common causes of repeated restarts:**
- Agent is actually working, but not emitting output frequently enough. The stuck detector only sees terminal output -- if the agent is thinking silently for >5 minutes, it looks dead.
  **Fix:** The agent should emit progress messages or `cm task progress` updates regularly so the idle detector sees activity.
- Agent is truly stuck on an error (build fails, missing dependency, permission denied). Each restart hits the same error.
  **Fix:** Check the terminal buffer (`/api/sessions/<id>/text`) for the repeating error. Fix the root cause rather than restarting.
- Agent's worktree is in a bad state (merge conflict, detached HEAD).
  **Fix:** `git -C ~/.worktrees/<agent-id> status` to inspect.

### 5. "I need to force-restart an agent cleanly"

If triage points to a fundamental problem (corrupted worktree, wedged PTY, leaked file handles):

```bash
# 1. Save the agent's current state to memory first
cm message <sessionId> "Save your state to memory now: cm memory section current_focus '<your status>'"
# Wait 30 seconds for the agent to respond

# 2. Kill via the web UI (preferred -- it runs proper cleanup)
curl -s -X POST http://localhost:3555/api/sessions/<id>/stop

# 3. If the session doesn't stop, check if it's a screen issue
screen -ls | grep <screenName>
screen -X -S <screenName> quit      # LAST RESORT -- only if session won't stop via API

# 4. Respawn via the office UI or wait for the manager to detect the dead session
```

**Never `pkill claude` or `pkill screen` globally** -- that kills ALL agents, not just the stuck one. Use session-specific commands.

## Quick reference: key files

| File | What it controls |
|---|---|
| `internal/pty/session.go` | Session lifecycle, PTY I/O, idle/working detection |
| `internal/pty/manager.go` | Session multiplexing, create/stop/list |
| `internal/ralph/tracker.go` | Completion detection, todo tracking, event emission |
| `internal/ralph/circuit_breaker.go` | Dual-condition exit gate for ralph loops |
| `internal/respawn/controller.go` | 13-state respawn state machine |
| `internal/office/recovery.go` | Stuck detection, tiered recovery escalation |
| `internal/office/config.go` | Thresholds (StuckThreshold, RecoveryMaxAttempts) |

## Anti-patterns

- **Blind restart without diagnosis.** You'll hit the same issue again and waste another cycle.
- **`pkill screen` / `pkill claude` globally.** Kills every agent. Use session-specific stop.
- **Lowering `StuckThreshold` to "catch problems faster."** This causes false positives on agents doing legitimate long-running work (e.g., running a test suite). 300 seconds is the tuned value.
- **Sending a long message to a context-full agent.** Your message is more context. Send a one-line directive or let the recovery loop handle it.
- **Ignoring the circuit breaker.** If an agent loops, the circuit breaker logic is the answer -- not a manual kill. Understand why the breaker didn't trip.

## Verification

After any intervention:
1. `cm roster` -- confirm the agent is back and in `idle` or `busy` state.
2. `cm memory query <agent-id>` -- confirm the agent's memory survived (sectioned memory persists across restarts).
3. `curl -s http://localhost:3555/api/sessions/<id> | jq '.status'` -- confirm the new session is healthy.
4. Watch `docker compose logs -f jarvis | grep <agent-name>` for 5 minutes -- confirm no repeated recovery cycles.
