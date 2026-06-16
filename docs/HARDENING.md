# 🔐 Security & Resilience Hardening

> Detailed hardening log for jarvis-go. Summarized in the [README](../README.md).

Jarvis has undergone multiple comprehensive code-review rounds with **100+ bug fixes** across security, correctness, concurrency, and resource management. Every change is race-tested (`go test -race ./...`) and the codebase is hardened against common failure modes.

## Review rounds

The early hardening campaign ran 10 review rounds (95+ fixes); subsequent
adversarial audit passes over the office feature set added more (path traversal
on worktree IDs, loop-state file collisions, concurrent skill-write races,
emit-under-lock data race, secret-scanner coverage, and others — each fixed and
regression-tested).

| Round | Fixes | Focus |
|-------|-------|-------|
| 1 | 15 | Goroutine leaks, data races, crash fixes, SDK session env |
| 2 | 20 | Correctness, frontend timers, resource management |
| 3 | 17 | API contract mismatches, TOCTOU races, JSON parsing |
| 4 | 8 | XSS, task pointer races, Stop() synchronization |
| 5 | 8 | postJson regressions, nil guards, brainstorm lock promotion |
| 6 | 10 | Triage contract fix, CSRF retry, null guards |
| 7 | 8 | N+1 disk reads, wg.Done ordering, raw fetch cleanup |
| 8 | 1 | PATCH handler CSRF freshness |
| 9 | 4 | Telegram webhook chi routing, XSS in quieter files |
| 10 | 4 | PTY reader leak, transcript position, XSS regression |
| Audit passes | 50+ | Office-feature hardening: worktree path-traversal guard, collision-resistant loop-state paths, atomic + serialized shared-skill writes, handoff task-loss guard, unbounded-file-read caps, MCP required-arg validation, home-dir fail-closed, secret-scan accuracy, nil-broker SSE safety |

## Security Hardening

- **CSRF protection** with automatic retry on token expiry (postJson/putJson/deleteJson helpers)
- **Path traversal prevention**: `safeName()` validator (`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, max 128 chars) applied to all user-supplied names (skills, cases, memory paths, task IDs); worktree IDs and loop-state keys are whitelist-validated / hashed so an operator-supplied member ID can never escape `.worktrees/`
- **Shell injection prevention**: `/api/run` endpoint uses `exec.Command` with split args, never `sh -c`
- **Committed-secret scan**: before replenishing an idle agent in perpetual mode, a bounded high-signal secret scan runs over the working tree; any finding halts replenishment team-wide and redacts credential-shaped strings before they reach logs (`internal/office/security_gate.go`)
- **State file permissions**: `~/.jarvis/state.json` written with 0600 mode (contains API keys)
- **Auth middleware**: Password authentication with session cookies, rate-limited login attempts, cached `IsPasswordConfigured` to avoid disk reads on every request
- **XSS prevention**: All HTML insertion in frontend uses escaped helpers or textContent; Ralph wizard preview escapes `taskDescription` and `completionPhrase`; expand history renders escape all server-supplied fields
- **Agent endpoint isolation**: `/api/office/agent/*` routes are auth-exempt but require valid `sessionId` from the `cm` CLI's JARVIS_SESSION_ID env var; path-parameter routing uses `chi.URLParam` for consistency

## Concurrency Safety

- **Zero goroutine leaks**: All background goroutines have explicit `done` channels or `sync.WaitGroup` coordination. `StartIdleMonitoring`/`startAutoStandup` use done channels in their select loops so `Stop()` reliably terminates them.
- **Lock ordering discipline**: `m.mu` (office manager) is always acquired before any `agent.mu` (agent state), never the reverse. `HandleAgentDeath` deep-copies tasks before reassignment to avoid racing on shared pointers.
- **Race-free agent state**: `SetSDKWorking`/`GetSDKWorking` and `SetPersonalityOverride`/`GetPersonalityOverride` wrap mutations in the agent mutex; concurrent readers get stable snapshots.
- **Non-blocking SSE delivery**: The SSE broker uses per-client write goroutines with bounded buffers; slow clients don't block the broadcast path.
- **Brainstorm cooldown TOCTOU fix**: The cooldown check and `lastBrainstormAt` update happen under a single `m.mu.Lock()` so two concurrent goroutines can't launch duplicate brainstorms.
- **SDK event consumer shutdown**: `eventConsumer`'s `defer s.wg.Done()` is the outermost defer so `Stop()` only unblocks after the events channel is closed and the final log line is written.
- **Serialized shared-skill writes**: `WriteSharedSkill`/`DeleteSharedSkill` take a package mutex and write atomically (tmp + rename) so the idle-monitor promotion path and concurrent HTTP skill-create calls can't clobber a `SKILL.md`.

## Resource Management

- **Bounded buffers**: Terminal buffer (2MB trim to 1.5MB), text output (1MB trim to 768KB), message log (50 entries FIFO), activity log (200 entries)
- **Agent memory caps**: 50 learnings, 25 skills, 10 distilled insights per agent (auto-distillation at 30 learnings)
- **I/O limits**: `jarvis-mcp` wraps HTTP response bodies with `io.LimitReader(resp.Body, 32<<20)` to cap at 32MB; session file reads are size-capped (25MB) to prevent OOM
- **Context timeouts**: CGC indexing has a 5-minute timeout; the objective verify gate has a 10-minute timeout; state file writes use atomic tmp-file rename with error logging and tmp cleanup
- **Hard stops**: perpetual runs pause for human review at the iteration cap (`RalphMaxLoopIterations`, default 10) and the optional wall-clock cap (`PerpetualLoopMaxDuration`)
- **Single PTY reader**: Only one goroutine reads from each PTY file descriptor, eliminating the reader-per-iteration leak pattern
- **Transcript position accounting**: Uses `f.Seek(0, io.SeekCurrent)` instead of `len(line)+1` to handle CRLF line endings correctly
