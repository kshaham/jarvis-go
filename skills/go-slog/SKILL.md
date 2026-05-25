---
name: go-slog
description: Structured logging with log/slog -- attribute discipline, log levels, Group/With for scoped context, secret redaction, and the attribute naming conventions actually used in jarvis-go (session, agent, configId, taskId). Match the in-repo style so grep queries across logs stay productive.
---

# Go Structured Logging with `log/slog`

`log/slog` is the only logger jarvis-go uses. It's stdlib, structured, and fast. This skill is the style guide so every agent's new log lines grep the same way as existing ones.

## Cardinal rule

> Log values as attributes, not as format strings. A log line without structured attributes is unsearchable and untypable.

## The shape

```go
slog.Info("Agent registered", "name", cfg.Name, "session", sessionID)
slog.Warn("Stuck agent recovery", "tier", tier, "attempt", attempt, "max", 3, "agent", name, "inactive", elapsed)
slog.Error("Failed to unmarshal state", "error", err, "path", path)
```

The pattern: a **constant human message** first, then alternating key-value attrs. Constant messages let you grep `Agent registered` across a thousand sessions; variable messages defeat that.

## The attribute vocabulary used in this repo

Match these exact attr names so existing observability / `journalctl | grep` continues to work:

| Key | What it means | Where it shows up |
|---|---|---|
| `name` | Agent display name (Vex, Bolt, Max) | `office/recovery.go`, `office/manager.go` |
| `session` | sessionID | `office/*`, `pty/*` |
| `configId` | TeamMemberConfig ID (vex, bolt, max) | `office/learning.go` |
| `taskId` | Office task ID | `office/manager.go`, `office/recovery.go` |
| `error` | Error value (wraps with `%w` preserved) | everywhere using Policy-2 errors |
| `path` | File path | state store, TLS cert loader |
| `tier` | Recovery attempt tier (1/2/3) | `office/recovery.go` |
| `count` | Count of items | many |
| `duration` / `elapsed` | `time.Duration` -- slog formats it nicely | `office/recovery.go` |
| `interval` | `time.Duration` config values | `office/manager.go` |

When adding a new log line, **reuse an existing key** if one fits. Do not invent `sessionID` when the rest of the repo says `session`.

## Log levels

- `slog.Debug` -- high-volume internal state, off in prod. Use sparingly.
- `slog.Info` -- normal operation: spawn, complete, transition. **Default for important events.**
- `slog.Warn` -- something unexpected but recoverable. Still operational.
- `slog.Error` -- a failure you want a human to see. Pair with the wrapped error.

Avoid `Fatal` / `Panic` -- jarvis-go doesn't use them.

## Grouping scoped attributes

When a function has many logs that share the same context, set up a scoped logger with `With`:

```go
log := slog.With("session", sessionID, "agent", cfg.Name)
log.Info("Starting task", "taskId", task.ID)
// ... later
log.Warn("Task blocked", "taskId", task.ID, "reason", reason)
```

This is cheaper than passing the full attr set to every call and keeps the source readable.

For deeply nested scope, `slog.Group` produces a nested JSON object in the default handler:

```go
slog.Info("standup finished",
    slog.Group("meeting", "id", meetingID, "duration", elapsed),
    "responses", len(resps),
)
```

## What NOT to log

- **Secrets.** Never log API keys, cookies, auth tokens, session cookies, user passwords, or anything from `.credentials.json`. If a struct has a `Secret` field, log a redacted marker or exclude it.
- **Full request bodies** on routine success paths. Log IDs, not payloads.
- **Full error chains on every layer.** One `slog.Error` at the policy-decision layer is enough (see `go-errors` skill).
- **Loops without sampling.** If you're about to call `slog.Info` inside a tight loop, you probably want a counter + one log at the end.

## Worked example: adding a new subsystem log

Say you're adding a new feature in `internal/office/manager.go` that processes pending tasks. The in-repo style looks like:

```go
func (m *Manager) processPending(ctx context.Context) {
    m.mu.RLock()
    pending := m.pendingTasks()
    m.mu.RUnlock()

    slog.Info("Processing pending tasks", "count", len(pending))
    for _, t := range pending {
        log := slog.With("taskId", t.ID, "session", t.SessionID)
        if err := m.processOne(ctx, t); err != nil {
            log.Error("Failed to process task", "error", err)
            continue
        }
        log.Info("Task processed")
    }
    slog.Info("Pending processing complete", "processed", len(pending))
}
```

Notes:
- One human message per line, constant across invocations.
- `count`/`processed` integers match the existing pattern.
- Scoped logger (`log := slog.With(...)`) avoids repeating `taskId` / `session`.
- `error` attr carries the wrapped error from policy-1 return sites.

## Anti-patterns

- **`slog.Info(fmt.Sprintf("agent %s did %s", name, action))`.** Dead to grep. Use attrs: `slog.Info("agent action", "name", name, "action", action)`.
- **Logging the same error twice.** See `go-errors` skill.
- **Unique messages per call.** `"Spawning agent-vex-1234"` is worse than `"Spawning agent"` with a `name` attr.
- **`slog.Default().With(...)` in a hot path.** `With` allocates. Build scoped loggers once at function entry.
- **Passing `err` as `"msg", err.Error()`.** Pass it as `"error", err` -- the handler handles stringification.

## Verification

1. After adding logs, grep the repo for your new message: `grep -rn '"Your message"' .` -- it should appear only where you added it.
2. `go vet ./...` -- catches some slog misuse (odd number of args).
3. Tail a real run: `journalctl --user -u jarvis-web -f` and confirm your log shows up with structured fields, not a raw string.
