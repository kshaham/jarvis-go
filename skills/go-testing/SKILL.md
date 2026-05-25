---
name: go-testing
description: Go testing patterns for jarvis-go specifically -- vanilla testing.T (no testify), subtests with t.Run, table-driven tests, recording fixture mocks (like recordingSessionStore), interface-based mocking, t.TempDir for filesystem tests, parallel discipline. Match the in-repo convention so new tests read like existing ones.
---

# Go Testing (jarvis-go flavor)

jarvis-go uses vanilla `testing` -- no testify, no ginkgo, no mock libraries. The in-repo style is deliberate: tests read as plain Go, and mocks are hand-rolled "recording" structs that implement the interface you're testing against. This skill is the pattern.

## Cardinal rule

> Write tests that look like the tests in the file next door. Copy the shape, not just the behavior.

## The basic shape

From `internal/office/agent_test.go`:

```go
func TestNewAgentState(t *testing.T) {
    cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend Engineer"}
    a := NewAgentState("sess-1", "vex", cfg)
    if a.SessionID != "sess-1" {
        t.Errorf("expected sess-1, got %s", a.SessionID)
    }
    if a.Config.ID != "vex" {
        t.Errorf("expected vex, got %s", a.Config.ID)
    }
}
```

- `*testing.T`
- Plain `if ... { t.Errorf(...) }` -- not `assert.Equal`
- `t.Errorf` reports and continues; `t.Fatalf` reports and stops
- Format strings: `"expected %v, got %v"` is the in-repo style

## Subtests with `t.Run`

Use subtests when one function has several independent cases. They isolate failures and let you run one case with `-run`:

```go
func TestMemoryQuery(t *testing.T) {
    t.Run("empty file returns no hits", func(t *testing.T) {
        hits, err := MemoryQuery("missing-id", "/tmp", "anything")
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if len(hits) != 0 {
            t.Errorf("expected 0 hits, got %d", len(hits))
        }
    })
    t.Run("finds hit in notes section", func(t *testing.T) {
        // ...
    })
}
```

Run one: `go test ./internal/office/ -run TestMemoryQuery/empty_file`

## Table-driven tests

When the same assertion applies to many inputs:

```go
func TestPickSkillsForRole(t *testing.T) {
    cases := []struct {
        role     string
        mustHave string
    }{
        {"Engineering Manager", "office-standup"},
        {"Senior Architect", "implementation-plan"},
        {"Backend Engineer", "debug"},
    }
    for _, tc := range cases {
        t.Run(tc.role, func(t *testing.T) {
            skills := pickSkillsForRole(tc.role)
            found := false
            for _, s := range skills {
                if s == tc.mustHave {
                    found = true
                    break
                }
            }
            if !found {
                t.Errorf("role %q: expected %q in %v", tc.role, tc.mustHave, skills)
            }
        })
    }
}
```

The repo already has an example of exactly this in `internal/office/kickstart_test.go`.

## Recording fixture mocks

jarvis-go doesn't use a mock library. Instead it uses **hand-rolled "recording" structs** that implement the interface under test and capture calls for later assertion. From `internal/office/orchestration_test.go`:

```go
type recordingSessionStore struct {
    mu     sync.Mutex
    writes []writeCall
}

type writeCall struct {
    sessionID string
    data      string
}

func (r *recordingSessionStore) WriteViaScreen(sessionID, data string) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.writes = append(r.writes, writeCall{sessionID, data})
    return nil
}

// ...other interface methods that return zero values...
```

Then in the test:

```go
store := &recordingSessionStore{}
m := NewManager(store, func(string, interface{}) {}, nil)
// ... exercise the manager
if len(store.writes) != 1 {
    t.Errorf("expected 1 write, got %d", len(store.writes))
}
```

When your test needs a mock:
1. Find the interface it needs (or define one).
2. Create a struct with a `sync.Mutex` + recorded calls slice.
3. Implement only the methods the test exercises; stub the rest with zero returns.
4. Assert on the recorded calls after the exercise.

## Filesystem tests use `t.TempDir`

Never hardcode `/tmp/test-XXX`. Use `t.TempDir()` which auto-cleans at test end:

```go
func TestMemoryWriteSection(t *testing.T) {
    dir := t.TempDir()
    // ...use dir as working directory
}
```

For tests that want to override the home dir (e.g. to control `~/.jarvis/agent-memory/`):

```go
t.Setenv("HOME", t.TempDir())
```

`t.Setenv` is the Go 1.17+ way; it restores automatically after the test.

## Parallel discipline

Mark a test parallel with `t.Parallel()` only when:
- It doesn't share global state with other tests.
- It uses `t.TempDir` instead of a shared path.
- It doesn't depend on `t.Setenv` if another test also sets the same var.

If in doubt, leave it sequential. The whole office package runs sequentially because of shared `~/.jarvis/learnings/` state -- don't fight that.

## Testing HTTP handlers + benchmarks

HTTP handlers use `httptest.NewRecorder` + `httptest.NewRequest`, then invoke the handler method directly against a fake `handler.Context`. See `internal/web/handler/*_test.go` for the in-repo examples -- copy that shape rather than reinventing.

Benchmarks use `testing.B`:

```go
func BenchmarkKickstart(b *testing.B) {
    team := makeOffice7Team()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = BuildKickstartPrompt(team[0], "/tmp", team, "", "", true)
    }
}
```

Run with `go test ./internal/office/ -bench BenchmarkKickstart -benchmem`. See `go-pprof` skill for profile-driven benchmarking.

## Anti-patterns

- **Importing testify.** Not in this repo. Vanilla `testing.T` only.
- **Global state between tests.** Use `t.TempDir`, `t.Setenv`, and fresh struct instances.
- **`time.Sleep` in a test** to wait for a goroutine. Either use a done channel + `select` with timeout, or use a real synchronization primitive.
- **Asserting against exact map iteration order.** Maps are unordered. Sort slices before comparing.
- **Tests that depend on `time.Now`.** Inject a clock or use `time.Now().Add(-time.Hour)` for the setup.
- **Skipping tests with `t.Skip` silently.** If you skip, log why.

## Verification

1. `go test ./...` -- green.
2. `go test -race ./...` -- green. The race detector finds real bugs.
3. `go vet ./...` -- catches `printf` and `testing` misuse.
4. Grep the new test for `t.Run` and `t.TempDir` -- if neither appears, you might be fighting the in-repo style.
