---
name: go-pprof
description: Go performance profiling with pprof -- the net/http/pprof side-effect import, runtime/pprof for CLIs, testing.B for micro-benchmarks, -cpuprofile / -memprofile / -race flags, and the go tool pprof workflow. Use when investigating slow endpoints, goroutine leaks, or memory growth in jarvis-go.
---

# Go Performance Profiling with pprof

When an endpoint is slow, a goroutine count is growing, or memory isn't getting freed, guessing is a waste of time. Profile first. This skill is the pprof recipe for jarvis-go.

## Cardinal rule

> Measure before optimizing. pprof first, optimization second. "I think X is slow" costs more than `go tool pprof`.

## Three profiling modes

| Tool | Use for | Flag / endpoint |
|---|---|---|
| `net/http/pprof` | Running servers (jarvis itself) | side-effect import + `/debug/pprof/*` |
| `runtime/pprof` | One-shot CLIs (`cm`, `jarvis-mcp`) | explicit start/stop |
| `testing.B` | Micro-benchmarks of a single function | `go test -bench` |

## Mode 1: `net/http/pprof` for running servers

Add one side-effect import in `internal/web/server.go`:

```go
import _ "net/http/pprof"
```

This auto-registers handlers on `http.DefaultServeMux`. Since jarvis uses chi/v5 (not DefaultServeMux), you have to either:

**Option A: mount the default mux under a chi prefix** (recommended for debug builds only):
```go
r.Mount("/debug", http.DefaultServeMux)
```

**Option B: register the specific pprof endpoints explicitly** (production-safe):
```go
import "net/http/pprof"

debug := chi.NewRouter()
debug.HandleFunc("/pprof/", pprof.Index)
debug.HandleFunc("/pprof/cmdline", pprof.Cmdline)
debug.HandleFunc("/pprof/profile", pprof.Profile)
debug.HandleFunc("/pprof/symbol", pprof.Symbol)
debug.HandleFunc("/pprof/trace", pprof.Trace)
r.Mount("/debug", debug)
```

**Important**: Gate `/debug/pprof` behind auth or bind to localhost only. Exposing pprof publicly is a DoS vector.

Once wired:

```bash
# CPU profile (30s sample)
go tool pprof -http=:8088 http://localhost:3555/debug/pprof/profile?seconds=30

# Heap allocations (current)
go tool pprof -http=:8088 http://localhost:3555/debug/pprof/heap

# Goroutine dump (for leaks)
curl http://localhost:3555/debug/pprof/goroutine?debug=2 > goroutines.txt

# Execution trace (visual timeline)
curl -o trace.out http://localhost:3555/debug/pprof/trace?seconds=10
go tool trace trace.out
```

`-http=:8088` starts a local visualization server in your browser with flame graphs, source annotation, and top callers. Use it -- it's 10x better than the text output.

## Mode 2: `runtime/pprof` for CLIs

For one-shot binaries like `cm` where you want a single profile of one execution:

```go
import "runtime/pprof"

func main() {
    if profileFlag {
        f, _ := os.Create("cpu.prof")
        pprof.StartCPUProfile(f)
        defer pprof.StopCPUProfile()
    }
    // ... real work
}
```

Then analyze:
```bash
go tool pprof -http=:8088 cpu.prof
```

For memory:
```go
f, _ := os.Create("mem.prof")
runtime.GC() // force GC so we see retained not yet freed
pprof.WriteHeapProfile(f)
f.Close()
```

## Mode 3: `testing.B` benchmarks

For targeted function profiling:

```go
func BenchmarkBuildKickstartPrompt(b *testing.B) {
    team := makeOffice7Team()
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = BuildKickstartPrompt(team[0], "/tmp", team, "", "", true)
    }
}
```

Run with profiling:
```bash
go test ./internal/office/ -bench BenchmarkBuildKickstartPrompt \
    -benchmem -cpuprofile cpu.prof -memprofile mem.prof
go tool pprof -http=:8088 cpu.prof
```

`-benchmem` prints allocations per op. `-cpuprofile` writes a pprof profile. `-memprofile` writes a heap profile.

## Debugging goroutine leaks

jarvis-go runs for 24+ hours. Goroutine growth is the #1 leak signal.

```bash
# Snapshot at start
curl -s http://localhost:3555/debug/pprof/goroutine?debug=2 > /tmp/g1.txt
# ... wait 1 hour
curl -s http://localhost:3555/debug/pprof/goroutine?debug=2 > /tmp/g2.txt
# Count
grep -c "^goroutine " /tmp/g1.txt /tmp/g2.txt
# Diff the kinds
diff <(grep "^goroutine " /tmp/g1.txt | awk '{$1=$2=""; print}' | sort -u) \
     <(grep "^goroutine " /tmp/g2.txt | awk '{$1=$2=""; print}' | sort -u)
```

A doubled count with the same stack is a leak. The stack tells you where the goroutine was spawned -- that's the fix site. See `go-concurrency` skill for the done-channel pattern that prevents leaks.

## Race detector (-race)

`-race` is not a profiler but it's the single highest-ROI flag for concurrent code:

```bash
go test -race ./...
go build -race ./...   # runs the server with race detection
```

Race-instrumented binaries are 2-20x slower and use more memory, so don't run them in prod. Use them in CI and local dev.

## What to look for

| Symptom | Profile | What to find |
|---|---|---|
| Slow endpoint | `/debug/pprof/profile?seconds=30` | Top-N in flame graph during the slow window |
| Memory growth | `/debug/pprof/heap` | `inuse_space` cumulative by allocation site |
| High CPU | same CPU profile | Hot paths, unexpected allocations |
| Goroutine leak | `/debug/pprof/goroutine?debug=2` | Repeated stacks across snapshots |
| Lock contention | `/debug/pprof/mutex` + `runtime.SetMutexProfileFraction(1)` | Top waiters |
| Blocking syscalls | `/debug/pprof/block` + `runtime.SetBlockProfileRate(1)` | Who's blocking |

## Anti-patterns

- **"Optimizing" without a profile.** You'll pick the wrong function.
- **Profiling a benchmark without `b.ResetTimer`** -- setup shows up as hot.
- **Publishing `/debug/pprof` to the internet.** Profile endpoints accept parameters that can burn CPU; treat as admin surface.
- **Ignoring `inuse_space` in heap profiles.** If `inuse` isn't growing, you don't have a leak, just churn.
- **Reading pprof text output.** Use `-http=:8088`. The flame graph is 10x clearer.
- **Running race in prod.** It's a dev-only tool.

## Verification

1. A change motivated by pprof should come with a before/after profile screenshot or a benchmark delta.
2. Goroutine leak fixes: rerun the leak-reproducing workload for 10 minutes and confirm the goroutine count stabilizes.
3. For allocation reductions: `go test -bench ... -benchmem` should show a lower `allocs/op`.
