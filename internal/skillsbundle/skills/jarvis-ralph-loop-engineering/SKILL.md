---
name: jarvis-ralph-loop-engineering
description: How jarvis's perpetual (RALPH) mode implements loop engineering -- the objective gate, hard stops, maker/checker split, durable state, the 4-condition test, and the accepted-change-rate metric. Read when configuring perpetual mode, debugging why the loop won't exit (or won't stop), or deciding whether a task belongs in a loop at all.
---

# RALPH as a loop-engineered system

Perpetual mode is not "an agent that never stops." It is a loop-engineered system:
something finds the work, hands it to the agent, an **objective gate** checks the result,
state is recorded, and a **hard stop** bounds the spend. The leverage is in the loop, not
in any single prompt. This skill is the operator + agent reference for that machinery.

> The "real" perpetual loop is the idle-replenishment loop in `internal/office/recovery.go`
> (`CheckForIdleAgents` -> `sendSelfReplenishmentPrompt`), NOT the `internal/ralph` circuit
> breaker (which gates the single-session terminal view). Both now share the same objective
> gate. See also `[[jarvis-ralph-tracker]]`.

## The 4-condition test (run this BEFORE turning work into a loop)

A loop earns its cost only when ALL four hold. Miss one and a single good prompt wins:

1. **The task repeats** (>= weekly). One-offs don't amortize setup.
2. **Verification is automated** -- a test/build/lint can fail the work without a human.
   This is `Config.VerifyCommand` (or auto-detect). No gate = the agent grades its own homework.
3. **The token budget can absorb the waste** -- loops re-read, retry, explore.
4. **The agent has senior-engineer tools** -- logs, a repro env, the ability to run what it writes.

Plus: a **human approval gate** before anything irreversible (merge, deploy, dependency bump).

### Good first loops
CI-failure triage, dependency-bump PRs (drafted, human-merged), lint-and-fix passes,
test-writing on code with strong coverage, flaky-test reproduction.

### Bad loops (keep a human in the chair)
Architecture rewrites, auth, payments, production deploys, schema migrations, vague product
work -- anything where "done" is a judgment call. jarvis enforces this: self-added tasks in
`Config.BlockedTaskCategories` are rejected and escalated during perpetual mode.

## The objective gate (the spine)

The loop exits a task only when the **verify command exits 0**, not when the agent says "done".

- Configured via `Config.VerifyCommand` (e.g. `go test ./... -count=1`). Empty => auto-detect
  from the working dir (`go.mod` -> go test, `package.json` -> npm test, `Cargo.toml` -> cargo
  test, ...).
- A failing gate **overrides** the agent's completion phrase + EXIT_SIGNAL. The loop does not
  exit; the circuit breaker resets and the agent keeps working.
- `autoCompleteIdleAgentTasks` no longer stamps "completed" on a silent agent -- it runs the
  gate first (`gatedAutoComplete`). On failure it nudges instead of completing.
- After 3 consecutive gate failures the agent escalates to stuck-agent recovery.

Spot-check the gate live: `GET /api/sessions/{id}/ralph-state` returns `circuitBreaker`,
`lastGateResult` (command, exit code, summary), and `gateFailureCount`. Gates rot -- verify
periodically that the gate actually rejects a failure you care about.

## Hard stops (so the loop can't run until someone notices the bill)

R24 removed dollar/token caps (subscription billing), so the binding constraints are:

- `Config.RalphMaxLoopIterations` (default 10): replenishment cycles before the loop pauses
  for human review. 0 = unlimited.
- `Config.PerpetualLoopMaxDuration` (default 0/unlimited): wall-clock lifetime cap.
- The existing per-session turn cap (default 100) is the other real ceiling.

On a cap, the agent's loop pauses (`office:loopMaxReached`), a durable escalation is recorded,
and Telegram is notified. Warn-at-80% behavior is built in.

## Maker / checker split

The agent that wrote the code is too nice grading its own homework. The objective gate is the
cheap deterministic check; an independent evaluator (separate model/instructions, no exposure
to the maker's reasoning) is the semantic check that runs after the gate passes. The evaluator
is fail-open: an evaluator outage logs a warning and lets the loop proceed rather than wedging.

For multi-step tasks, agents wrap work in `/goal <criteria>` so Claude Code's evaluator decides
done -- see `[[office-goal-loop]]`. Goal text MUST end in a verification command whose exit code
proves the criterion.

## Durable state (the agent forgets; the repo does not)

- Per-session RALPH loop state persists to `~/.jarvis/office-memory/` so a daemon restart
  resumes iteration count + gate-failure history instead of cold-starting.
- The **escalation log** (`~/.jarvis/office-memory/escalations.jsonl`, `GET
  /api/office/escalations`) is the single place to see what the loop could not resolve:
  `recovery_exhausted`, `blocked_max_triage`, `loop_hard_stop`, `blocked_task_category`.
- **Standing spec reread:** project invariants are re-read every monitor sweep and re-injected
  into each replenishment prompt as `[CURRENT CONSTRAINTS]`, so a rule added mid-loop ("never
  push to main") reaches running agents -- not just newly spawned ones.

## The metric that matters: accepted-change rate

Not tokens spent, not tasks attempted. `GET /api/office/metrics` reports
`acceptedChangeRate = completed / (completed + rejected)` and `costPerAcceptedChange`. Below
0.5 in perpetual mode the loop is creating review work, not saving it -- `office:lowAcceptanceRate`
fires. If you see it, tighten the gate or pull the loop off judgment-call work.

## Comprehension debt (the failure that sharpens as the loop improves)

The faster the loop ships code you didn't write, the larger the gap between what the repo
contains and what you understand. Mitigations are not technical:

- **Read the diffs.** The loop opens draft PRs; a human reviews and merges.
- **Spot-check the gate** (the ralph-state endpoint above). A gate that doesn't catch the
  failure you care about is worse than none.
- **Keep the loop on small, machine-checkable work.** The blocklist enforces this.

## Security tax (an unattended loop is an unattended attack surface)

- The loop runs a security scan before replenishing; new secrets/CVEs halt replenishment
  (`office:securityIssueDetected`).
- Auto-promoted skills are logged (`promoted-skills.jsonl`) and revocable -- audit sources
  before trusting them.
- Task titles are redacted before logging; verbose logging is off in long runs.
- Permission scope is re-audited on `Config.PermissionReauditInterval` (default 30d).

## Related skills
- `[[jarvis-ralph-tracker]]` -- the tracker/circuit-breaker internals.
- `[[office-goal-loop]]` -- driving a single task with `/goal`.
