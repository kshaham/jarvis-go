---
name: go-atomic-writes
description: Crash-safe file persistence in Go via write-to-tmp-then-os.Rename. Covers permissions (0o600 for secrets, 0o644 for normal), MkdirAll before write, when fsync matters vs when Rename is enough, the 500ms debounce pattern from internal/state/store.go. Small pattern, load-bearing for data integrity.
---

# Go Atomic Writes

jarvis-go persists state to disk (session store, per-agent memory, TLS cert, skills, learnings) and a mid-write crash must not corrupt anything. Every persistent write in this repo follows the same 4-line recipe. This skill is that recipe.

## Cardinal rule

> Write to `<path>.tmp`, then `os.Rename(tmp, final)`. Rename is atomic on the same filesystem. Never `os.WriteFile` directly over a live file.

## The recipe

```go
func saveFoo(path string, data []byte) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil {
        return fmt.Errorf("write %s: %w", tmp, err)
    }
    if err := os.Rename(tmp, path); err != nil {
        os.Remove(tmp) // best-effort cleanup
        return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
    }
    return nil
}
```

Four operations, each important:

1. **`MkdirAll` first.** Ensures the parent directory exists. `0o755` on directories (readable/traversable), consistent across the repo.
2. **`WriteFile` to `<path>.tmp`.** A full-file write that either completes or doesn't. Use `0o644` for normal data files, `0o600` for secrets (see below).
3. **`os.Rename` atomically replaces the target.** On the same filesystem, Rename is atomic from the perspective of any process reading the file: they see either the old version or the new version, never a half-written mix.
4. **Cleanup the tmp on Rename failure.** Best-effort -- if cleanup fails too, log and move on. The tmp file won't corrupt anything.

## Repo examples

### `internal/state/store.go` -- session state

```go
// flushLocked writes AppState to ~/.jarvis/state.json.
func (s *AppState) flushLocked() {
    data, err := json.MarshalIndent(s, "", "  ")
    if err != nil {
        slog.Error("Failed to marshal state", "error", err)
        return
    }
    dir := filepath.Dir(s.filePath)
    os.MkdirAll(dir, 0700) // 0700 -- user-only access to state dir
    tmp := s.filePath + ".tmp"
    if err := os.WriteFile(tmp, data, 0600); err != nil { // 0600 -- user-only secrets
        slog.Error("Failed to write state", "error", err)
        return
    }
    if err := os.Rename(tmp, s.filePath); err != nil {
        slog.Error("Failed to rename state file", "error", err)
        os.Remove(tmp)
    }
}
```

Note the Policy-2 error handling from `go-errors`: persistence failures are logged and swallowed, not returned. The caller is always "best effort persist, retry on next save." Also note the permissions: `0o700`/`0o600` because the state file may contain auth tokens.

### `internal/office/memory.go` -- per-agent memory

```go
// writeMemorySections serializes sectioned memory back to disk.
func writeMemorySections(configID, workingDir, header string, sections map[string]string) error {
    path := GetAgentMemoryPath(configID, workingDir)
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    // ... build b.String() ...
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}
```

Policy-1 here: errors propagate because agent-memory failures should abort the operation the agent was doing. Same recipe, different error handling.

### `internal/office/learning.go` -- learnings JSON

Uses the same `<path>.tmp` + `os.Rename` dance inside `writeLearningData`, guarded by a module-level `sync.Mutex` since many goroutines can write learnings concurrently.

## Permissions cheat sheet

| Content | File mode | Dir mode | Example |
|---|---|---|---|
| Secrets (tokens, auth config, TLS keys) | `0o600` | `0o700` | `~/.jarvis/state.json`, `~/.jarvis/auth-secret.key`, TLS key |
| Normal data (memory, skills, learnings) | `0o644` | `0o755` | `~/.jarvis/agent-memory/*/`, `~/.jarvis/learnings/*.json` |
| Executables | `0o755` | `0o755` | generated scripts |

Match the existing mode for a file when you add a new writer to it. Mismatched modes cause weird "permission denied" bugs later.

## Debounced writes

When many calls can mutate the same file in a short window (e.g. SSE events updating `state.json`), a debounce timer batches them:

```go
// internal/state/store.go
const debouncePersistDelay = 500 * time.Millisecond

func (s *AppState) save() {
    s.debounceDirty = true
    if s.debounceTimer == nil {
        s.debounceTimer = time.AfterFunc(debouncePersistDelay, func() {
            s.mu.Lock()
            s.debounceTimer = nil
            if !s.debounceDirty {
                s.mu.Unlock()
                return
            }
            s.debounceDirty = false
            s.flushLocked()
            s.mu.Unlock()
        })
    }
}
```

The invariants:

- Many `save()` calls in a 500ms window become one `flushLocked` call.
- `debounceDirty` is cleared before `flushLocked` -- if a new save comes in while we're writing, it'll schedule the next flush without missing a mutation.
- `debounceTimer` is nilled inside the callback so the next `save()` schedules a fresh timer.

Use this when writes are frequent and ordering doesn't matter at sub-500ms resolution. Do NOT debounce a log file -- each event needs to hit disk.

## When `fsync` matters

`os.Rename` gives atomicity: readers see old-or-new, never half-written. It does NOT guarantee durability across a hard power loss -- the new file's data may live in the kernel page cache for seconds before hitting disk.

For jarvis-go, this is acceptable: on a hard crash, losing up to 500ms of state is fine (the debounce window already concedes that). Don't bother with `fsync`.

You DO need `fsync` when:

- Data integrity matters more than performance (financial ledgers, WAL).
- You're writing a file that must survive a power loss (you probably aren't in jarvis-go).

If you ever need it, the pattern is:

```go
f, err := os.CreateTemp(dir, "name-*.tmp")
// ... write data to f ...
if err := f.Sync(); err != nil { return err } // flush to disk
if err := f.Close(); err != nil { return err }
if err := os.Rename(f.Name(), finalPath); err != nil { return err }
// Also fsync the parent directory so the rename itself is durable:
dirF, _ := os.Open(dir); dirF.Sync(); dirF.Close()
```

Don't ship this unless you've measured the perf impact and have a reason to care.

## Anti-patterns

- **`os.WriteFile(path, ...)` directly.** A crash mid-write leaves a half-written file.
- **Writing JSON piecewise with `Encoder.Encode`.** Same problem -- partial writes are visible. Marshal to a buffer first, then atomically replace.
- **Forgetting `MkdirAll`.** First run on a clean machine fails with "no such directory."
- **`0o777` because "it works."** It works because every user can read and write it. Including the ones you don't want to.
- **Not removing `<path>.tmp` on Rename failure.** The stale tmp files pile up and confuse the next run.
- **Rename across filesystems.** On Linux, Rename across mount points returns `EXDEV`. If your dataDir and cache live on different mounts, you need to write + close + read + write + remove instead. For jarvis-go this never comes up (everything is under `~/.jarvis`).
- **Relying on `Rename` under Windows concurrency.** Windows `Rename` is NOT atomic if the target exists -- use a package like `github.com/google/renameio` if you ever port this to Windows. (jarvis-go is Linux-only -- no concern here.)

## Verification

1. `go build ./... && go test ./...` -- standard.
2. Kill -9 the jarvis process mid-save (attach via debugger or SIGKILL), restart, verify the state file is either the old or new version -- never malformed JSON.
3. Check permissions: `ls -l ~/.jarvis/state.json` -- should be `-rw-------` (`0600`) for secrets.
4. Search for direct `os.WriteFile` over a persistent path (`Grep` for `os.WriteFile.*\.json`) and verify each one is followed by a `Rename` or uses the `.tmp` suffix.
