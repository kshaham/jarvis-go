---
name: jarvis-ralph-tracker
description: Ralph loop tracking in internal/ralph/ -- completion phrase parsing, todo item extraction, RALPH_STATUS structured blocks, dual-condition circuit breaker (both completion phrase AND todo exit signal required), 85% similarity deduplication, event debouncing. Read when customizing loop detection or debugging why an agent won't stop looping.
---

# Jarvis Ralph Tracker

The Ralph tracker (`internal/ralph/`) watches an agent's terminal output in real time and decides when a work loop has completed. It powers the RALPH (Recognize-Assess-Log-Plan-Handle) continuous improvement loop -- the mode where agents run perpetually, picking up new tasks after finishing old ones.

The tracker is a state machine that parses output lines, tracks todos, detects completion phrases, and fires events. The exit decision goes through a **circuit breaker** that needs TWO independent signals before tripping.

## Cardinal rule

> The circuit breaker requires BOTH a completion phrase AND a todo exit signal within a 30-second window. One alone is not enough. If an agent loops, one of these two conditions isn't being met.

## Architecture

```
Terminal output (from PTY session)
    │
    ├─> Line parser: extracts completion phrases, todo items, RALPH_STATUS blocks
    │   │
    │   ├─> Completion phrases → CircuitBreaker.RecordCompletion()
    │   ├─> Todo items → TodoTracker (dedup + count)
    │   │   └─> All checked? → CircuitBreaker.RecordExitSignal()
    │   └─> RALPH_STATUS → Structured status event
    │
    └─> CircuitBreaker.ShouldBreak()?
        ├─ YES → emit LoopCompleted event → respawn controller takes over
        └─ NO  → keep watching
```

## Completion phrases

The tracker scans each cleaned output line for phrases that indicate the agent has finished a unit of work. Defined in `internal/ralph/tracker.go`:

Default phrases include: `"done"`, `"finished"`, `"completed"`, `"all tasks complete"`, and variations. The match is case-insensitive and requires word boundaries (so "I'm undone" doesn't match "done").

**To add a new completion phrase:** find the `completionPhrases` slice in `tracker.go` and append your phrase. Then run `go test ./internal/ralph/ -run TestCompletionDetection` to verify it triggers.

## Todo tracking

The tracker extracts todo items from markdown-style checkbox output:

```
- [x] Implement login handler      → checked
- [ ] Write login tests            → unchecked
- [x] Update auth middleware        → checked
```

Todos are deduplicated by content similarity (85% threshold via `internal/ralph/tracker.go` using string similarity). This prevents an agent that says "Implement login handler" and "implement the login handler" from counting as two separate todos.

**Exit signal fires when:** all extracted todos are checked (`[x]`), or there are zero unchecked todos remaining.

## The circuit breaker

`internal/ralph/circuit_breaker.go`:

```go
type CircuitBreaker struct {
    mu                sync.Mutex
    completionSeen    bool
    exitSignalSeen    bool
    completionAt      time.Time
    exitSignalAt      time.Time
    window            time.Duration  // default 30s
    tripped           bool
    tripCount         int
    maxResets          int           // default 3
}
```

**Dual condition:** `ShouldBreak()` returns true ONLY when:
1. `completionSeen == true`
2. `exitSignalSeen == true`
3. Both happened within the `window` (30 seconds of each other)

If only one condition fires, the breaker stays open and the loop continues. This prevents false exits from:
- An agent saying "done" mid-task (completion phrase without todo exit)
- Todos getting checked off but the agent continuing to work on something else (exit signal without completion phrase)

**Reset logic:** After the circuit trips and the respawn controller acts, `Reset()` clears the conditions for the next cycle. `maxResets` caps how many times the breaker can trip in a single session (default 3) before it stops resetting (preventing infinite respawn loops).

**Testing the circuit breaker:**
```bash
go test ./internal/ralph/ -run TestCircuitBreaker -v
```

Tests cover: not-tripped-without-both-conditions, trips-on-dual-condition, stays-tripped, reset, max-resets.

## RALPH_STATUS blocks

Agents can emit structured status updates in a special format:

```
RALPH_STATUS:
  phase: ASSESS
  current_task: "Write login tests"
  todos_remaining: 2
  confidence: high
END_STATUS
```

The tracker parses these blocks and emits `StatusBlockDetected` events. The office dashboard uses them for the agent's status card. They're optional -- the circuit breaker works without them.

## Event types

| Event | When it fires |
|---|---|
| `LoopCompleted` | Circuit breaker tripped (both conditions met) |
| `TodosUpdated` | Todo list changed (new item found, item checked/unchecked) |
| `StatusBlockDetected` | RALPH_STATUS block parsed |
| `CompletionDetected` | Completion phrase matched (informational) |

Events flow through the tracker's event channel to the office manager, which decides what to do (start next task, trigger standup, etc.).

## Event debouncing

The tracker debounces rapid-fire events to avoid flooding consumers. If the same event type fires multiple times within 2 seconds, only the first is emitted. This prevents a chatty agent that says "done" three times in a row from triggering three `CompletionDetected` events.

## Common debugging scenarios

### Agent loops without stopping

1. Check circuit breaker state: `curl -s http://localhost:3555/api/sessions/<id>/ralph | jq '.circuitBreaker'`
2. If `completionSeen: true` but `exitSignalSeen: false` → the agent isn't checking off all todos. Look at the todo list.
3. If `exitSignalSeen: true` but `completionSeen: false` → the agent's final message doesn't contain a completion phrase. Check what it actually says in the terminal buffer.
4. If both are false → the agent hasn't finished yet. Wait.

### Agent stops too early

1. Check if a partial-match completion phrase triggered prematurely. Look at the terminal buffer for the matching line.
2. The similarity deduplication (85%) may be collapsing distinct todos into one, making the count reach zero early.

### Agent hits max resets

After 3 circuit trips in one session, the breaker stops resetting. The agent must be manually restarted or the session must be recreated.

## Key files

| File | Lines | Purpose |
|---|---|---|
| `internal/ralph/tracker.go` | 532 | Main tracker: line parsing, event emission, completion phrases |
| `internal/ralph/circuit_breaker.go` | 148 | Dual-condition exit gate |
| `internal/ralph/tracker_test.go` | 340 | Tests: completion detection, circuit breaker, todo parsing |
| `internal/ralph/types.go` | ~50 | Event types and todo structures |

## Anti-patterns

- **Adding a completion phrase that's too common** (e.g., "ok" or "yes"). Agents say these conversationally and the breaker trips when they're mid-task.
- **Lowering the similarity threshold below 80%.** Too many false dedup matches; distinct todos collapse.
- **Bypassing the circuit breaker** by directly emitting `LoopCompleted`. The dual-condition check exists to prevent false exits.
- **Relying on RALPH_STATUS blocks for exit logic.** They're informational. The circuit breaker is the authority.

## Verification

1. `go test ./internal/ralph/...` -- covers completion detection, circuit breaker conditions, todo parsing, deduplication.
2. For live debugging: watch the ralph state via `curl -s http://localhost:3555/api/sessions/<id>/ralph | jq` during an agent run.
3. After modifying completion phrases: verify the new phrase triggers with `go test ./internal/ralph/ -run TestCompletionDetection`.
