---
name: go-state-machine
description: Explicit state machines in Go using int iota states, a validTransitions map, and setState(newState) error under a mutex -- the pattern used in internal/respawn/controller.go. Invalid transitions return errors, not panics. Generalizable to any lifecycle-shaped problem (session, task, meeting, recovery).
---

# Go State Machines (jarvis-go flavor)

Several subsystems in jarvis-go are state machines: `internal/respawn/controller.go` has 13 states, `internal/pty/session.go` has a status lifecycle, `internal/office/standup.go` has a meeting lifecycle. Every well-shaped state machine in this repo follows the same pattern. This skill is that pattern.

## Cardinal rule

> States are `int iota` with a `String()` method. Transitions are declared explicitly in a `validTransitions` map. `setState` takes a write lock, validates, transitions. Invalid transitions return errors. No panics, no silent no-ops.

## The canonical example

From `internal/respawn/types.go`:

```go
type RespawnState int

const (
    StateStopped RespawnState = iota
    StateWatching
    StateConfirmingIdle
    StateAiChecking
    StateSendingUpdate
    StateWaitingUpdate
    StateSendingClear
    StateWaitingClear
    StateSendingInit
    StateWaitingInit
    StateMonitoringInit
    StateSendingKickstart
    StateWaitingKickstart
)

func (s RespawnState) String() string {
    switch s {
    case StateStopped:        return "STOPPED"
    case StateWatching:       return "WATCHING"
    // ... one case per state
    }
    return "UNKNOWN"
}

var validTransitions = map[RespawnState][]RespawnState{
    StateStopped:         {StateWatching},
    StateWatching:        {StateConfirmingIdle, StateStopped},
    StateConfirmingIdle:  {StateAiChecking, StateWatching, StateStopped},
    // ... one entry per state, listing legal next states
}
```

Three design choices worth internalizing:

1. **`int iota` not string.** Comparisons are integer comparisons. Serialization is via the `String()` method for logs + errors, not the raw int. Integer states are also immune to typos ("WATCHING" vs "WACHING") -- the compiler catches them.
2. **`String()` method.** Every log line, every error message prints the state as a human-readable name. `slog.Info("state transition", "from", old, "to", new)` Just Works because `slog` calls `String()`.
3. **Explicit transition map.** Legal transitions are data, not code scattered through conditionals. You can print the map, diff it, unit test it, and spot illegal combinations at a glance.

## The setState helper

From `internal/respawn/controller.go`:

```go
func (rc *Controller) setState(newState RespawnState) error {
    rc.mu.Lock()

    if newState == rc.state {
        rc.mu.Unlock()
        return nil // no-op is always legal
    }

    allowed, ok := validTransitions[rc.state]
    if !ok {
        rc.mu.Unlock()
        return fmt.Errorf("no transitions from state %s", rc.state)
    }
    if !slices.Contains(allowed, newState) {
        cur := rc.state
        rc.mu.Unlock()
        return fmt.Errorf("invalid transition: %s -> %s", cur, newState)
    }

    old := rc.state
    rc.state = newState
    rc.mu.Unlock()

    slog.Info("respawn state", "from", old, "to", newState, "session", rc.sessionID)
    return nil
}
```

Notes:

- **Write lock for the entire decision + mutation.** Reading the current state, validating, and setting the new state must be atomic. A read lock followed by a write lock would race.
- **`newState == rc.state` returns nil.** "Set to same state" is a legal no-op. This simplifies caller code (they don't have to check).
- **Unknown source state returns an error.** If you ever call `setState` with an uninitialized controller, you want to find out immediately.
- **Error on invalid transition, not panic.** Callers decide whether a failed transition is fatal. The recovery loop might tolerate one failure and retry; a test might assert on the error; `panic` takes that choice away.
- **Log after unlock, before return.** Logging under a mutex is a contention smell; logging after release is safe because the values you need (`old`, `newState`, `sessionID`) are captured in local variables.

See also the `go-concurrency` skill for the "never hold a mutex across I/O" rule, which is why the log happens after `Unlock`.

## Testing a state machine

Two kinds of tests matter: **valid-transition tests** ("the happy paths work") and **invalid-transition tests** ("the guards are real"). The repo has both.

```go
// internal/respawn/controller_test.go ~line 58
func TestController_InvalidTransition(t *testing.T) {
    rc := NewController("test-session", DefaultRespawnConfig())
    // From Stopped, can only go to Watching.
    err := rc.setState(StateSendingUpdate)
    if err == nil {
        t.Error("expected error for invalid transition")
    }
}
```

For valid transitions, exercise the state machine through its public methods and assert you end up in the expected state:

```go
func TestController_WatchingToIdle(t *testing.T) {
    rc := NewController("test-session", DefaultRespawnConfig())
    if err := rc.setState(StateWatching); err != nil {
        t.Fatalf("unexpected: %v", err)
    }
    // ... trigger whatever should cause the ConfirmingIdle transition ...
    if got := rc.CurrentState(); got != StateConfirmingIdle {
        t.Errorf("expected ConfirmingIdle, got %s", got)
    }
}
```

A table-driven approach covers the matrix cheaply (see `go-testing` skill):

```go
func TestValidTransitions(t *testing.T) {
    cases := []struct {
        name    string
        from    RespawnState
        to      RespawnState
        wantErr bool
    }{
        {"stopped_to_watching", StateStopped, StateWatching, false},
        {"stopped_to_sending", StateStopped, StateSendingUpdate, true},
        // ... cover the matrix you care about
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            rc := NewController("t", DefaultRespawnConfig())
            rc.state = tc.from // skip directly to the starting state
            err := rc.setState(tc.to)
            if (err != nil) != tc.wantErr {
                t.Errorf("err=%v wantErr=%v", err, tc.wantErr)
            }
        })
    }
}
```

## When to reach for this pattern

Use an explicit state machine when **any** of these apply:

- The subsystem has 4+ distinct phases.
- Illegal phase combinations should be caught at runtime, not left as comments.
- Multiple goroutines can request transitions and you want to linearize them.
- Logs should carry a clear "phase" attribute that survives filtering.
- Tests need to assert on intermediate states.

If your subsystem has a `isRunning bool` + `isStopping bool` + `isCompleted bool`, you already have a state machine -- just a hidden one. Make it explicit.

## Anti-patterns

- **String states.** Typos compile. Use `int iota` + `String()`.
- **Transition logic spread across caller code.** If the caller does `if current == X && newState == Y { ... }`, you've reinvented the transition map badly. Put the rule in data.
- **`panic` on invalid transition.** Callers lose the choice to recover. Return an error.
- **Read-modify-write without a mutex.** Two goroutines transitioning simultaneously is a race the detector will catch.
- **Logging inside the critical section.** Capture values, unlock, log.
- **Mutating the state from outside `setState`.** If there's a public setter and a private direct assignment, you've broken the invariant. Route everything through `setState`.

## Verification

1. `go test -race ./internal/respawn/...` -- the race detector catches simultaneous transitions.
2. `go test ./internal/respawn/ -run TestController_InvalidTransition -v` -- the negative case passes.
3. Print the `validTransitions` map in a test and diff it against your design doc -- the two should match exactly.
4. Grep for direct state assignment (`\.state = State`) and confirm there are zero hits outside `setState` / test fixtures.
