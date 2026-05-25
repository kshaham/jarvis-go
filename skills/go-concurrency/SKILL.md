---
name: go-concurrency
description: Goroutines, channels, sync.RWMutex vs sync.Mutex, context.Context propagation, done-channel graceful shutdown, and the race detector. The vocabulary for working on jarvis-go's concurrency-heavy internal packages (office manager, pty session, sse broker, respawn controller).
---

# Go Concurrency

jarvis-go is concurrency-heavy. Every long-lived subsystem has goroutines, mutexes, channels, and a graceful shutdown path. If you're touching `internal/office/`, `internal/pty/`, `internal/web/sse/`, or `internal/respawn/`, you need the vocabulary in this skill.

## Cardinal rule

> Share memory by communicating (channels); share communication by locking (mutexes). Pick the tool that matches the lifetime, not the one you used last time.

## When to use which primitive

| You have... | Use | Example in repo |
|---|---|---|
| Many readers, occasional writers | `sync.RWMutex` | `internal/office/manager.go` Manager.mu |
| One writer, one or more consumers | channel (buffered if fan-out) | `internal/web/sse/broker.go` event chan |
| Short-lived exclusion (map update) | `sync.Mutex` | `internal/pty/manager.go` createMu |
| "Wait for N things to finish" | `sync.WaitGroup` | `internal/pty/session.go` read/write goroutines |
| "Do this once, lazily" | `sync.Once` | frontmatter cache in `internal/office/skills.go` |
| "Cancel everything below me" | `context.Context` with cancel | every HTTP handler |

## Patterns from this repo

### Done-channel graceful shutdown

The canonical jarvis-go shutdown shape (from `internal/office/manager.go` and `internal/pty/session.go`):

```go
type Manager struct {
    mu          sync.RWMutex
    ticker      *time.Ticker
    monitorDone chan struct{}
}

func (m *Manager) StartIdleMonitoring() {
    m.ticker = time.NewTicker(m.config.IdleCheckInterval)
    m.monitorDone = make(chan struct{})
    go func() {
        for {
            select {
            case <-m.ticker.C:
                m.checkIdle()
            case <-m.monitorDone:
                return
            }
        }
    }()
}

func (m *Manager) StopIdleMonitoring() {
    if m.ticker != nil {
        m.ticker.Stop()
    }
    if m.monitorDone != nil {
        close(m.monitorDone)
        m.monitorDone = nil
    }
}
```

Closing the done channel is idempotent-safe only if you nil it after close. Always nil after close.

### Double-checked creation under a read lock

From `internal/web/handler/context.go` (`GetOrCreateRespawnController`):

```go
ctx.mu.RLock()
v, ok := ctx.m[key]
ctx.mu.RUnlock()
if ok {
    return v
}
ctx.mu.Lock()
defer ctx.mu.Unlock()
// Re-check after upgrading to write lock -- another goroutine may have won the race.
if v, ok = ctx.m[key]; ok {
    return v
}
v = newThing()
ctx.m[key] = v
return v
```

This is how the repo avoids a write lock on every lookup. Match the pattern when you add new lazy-init maps.

### Context propagation

Every HTTP handler receives `r.Context()`. Pass it to anything that blocks:

```go
ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
defer cancel()
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
```

Never create a `context.Background()` inside a request-scoped handler -- you'll leak the cancellation.

### Buffered channels for fan-out

The SSE broker (`internal/web/sse/broker.go`) uses a buffered channel per client so one slow consumer can't block the publisher:

```go
client := &Client{
    ch: make(chan Event, 256), // drop when full, never block
}
```

When the buffer fills, the publisher drops rather than blocks. For the same reason, `internal/pty/manager.go` uses a 256-buffered events channel.

## Race detector

Run tests with `-race` whenever you touch concurrent code:

```
go test -race ./internal/office/...
```

This WILL fail slower than non-race runs. Worth it -- `-race` has caught every real concurrency bug in this repo.

## Anti-patterns

- **`for _, x := range things { go f(x) }`** in Go <1.22 -- the loop variable is shared. Pass `x` as an arg: `go func(x T) { f(x) }(x)`.
- **Mutex-protecting a channel send.** If you're locking to send on a chan, you probably want a plain `sync.Cond` or a state machine instead.
- **`time.Sleep` in a hot loop.** Use `time.Ticker` + `select` on a done channel.
- **`context.TODO()` in prod code.** Use `context.Background()` if genuinely root, `r.Context()` if request-scoped.
- **Holding a mutex across an I/O call.** Release before the blocking op, re-acquire to commit.
- **Unclosed channels feeding goroutine leaks.** If a producer can exit, close its channel so consumers can break out of `range`.

## Verification

After any concurrency change:
1. `go test -race ./...` -- must be clean.
2. `go vet ./...` -- catches `copylocks` (copying a mutex) and others.
3. Spot-check: does every `go func()` have a clear exit path (done chan, context cancel, or naturally-closing input)?

If you cannot answer "how does this goroutine stop?" for every goroutine you spawn, you have a leak.
