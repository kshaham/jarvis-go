---
name: office-protocols
description: The full team protocol reference for Jarvis office agents -- task lifecycle, git workflow, standup/brainstorm/leader assignment, restart recovery, systematic debugging, verification before completion. Read this when you spawn and re-read it when you forget a step.
---

# Office Protocols

You are an agent in a Jarvis virtual office. Your kickstart prompt is intentionally short -- the full team protocol lives here. Read it once at spawn and re-read any section when you forget a step.

Every command on this page is run via `Bash` (the `cm` binary is on your PATH; `$JARVIS_API_URL` and `$JARVIS_SESSION_ID` are already set).

## Communication

Team messages arrive in your prompt as `[Team Message from {Name}]: {content}`. You may receive task assignments from the team leader or code-review requests from peers. Respond naturally.

The `cm` command is mandatory for ALL server communication. Never use raw `curl`.

```
cm roster                       # Get teammate sessionIds
cm message <sessionId> "..."    # Message a teammate
```

## Task Lifecycle (follow this for EVERY task)

```
1. cm task ack <id>                                    # acknowledge receipt
2. cm task start <id>                                  # mark in progress
3. cm task progress <id> "<brief update>"              # report progress
4. cm task blocked <id> "<what is blocking you>"       # if stuck
5. cm task complete <id> --summary "<2-3 sentence handoff>"
```

**Step 5 is non-negotiable.** The `--summary` flag is appended to your memory under `section: task_history` automatically. Without a summary, your future self will not know what you did. The summary should answer:

- What was the goal?
- What files / commits changed?
- Anything the next agent should know?

Other task commands:

```
cm task list                                              # your queue
cm task self-add "<title>" "<description>" <priority>    # propose work
```

## Git Workflow

You work in your own isolated git worktree on a dedicated branch (`agent/<your-id>`).

- **Commit directly to your branch.** No need to create new branches.
- Use **conventional commit messages**: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`.
- Keep commits **atomic** -- one logical change per commit.
- **Never push to a remote.** Forge is the only agent who merges and pushes.
- **Never switch branches.** You stay on `agent/<your-id>` for your entire lifetime.
- Run `git branch` once to confirm you're on the right branch before your first commit.

## Standup Protocol

When you receive a message starting with `[STANDUP]`, you MUST immediately run the `cm` command embedded in that message. **Do NOT just reply in chat** -- you must execute the command via Bash so the standup system records your response.

For the format and how to draft a frugal standup that pulls from memory, read `cm skill read office-standup`.

## Brainstorm Protocol

When you receive a message starting with `[BRAINSTORM]`, you need new work. Look at the codebase, identify improvements matching your role, and propose 3-5 concrete tasks. Then respond with the `cm` command in the brainstorm prompt.

After proposing tasks, pick your highest-priority one and start working immediately.

## Leader Task Assignment (strategic-tier agents only)

If your role is strategic (manager, architect), after standup you will receive a `[LEADER TASK ASSIGNMENT]` prompt with each member's standup. You MUST:

1. Read each agent's report.
2. Create 2-4 specific, actionable tasks for EACH team member.
3. Respond with:
   ```
   cm leader-tasks respond "TASKS_FOR:<agentId>
   - [HIGH] <title> | <description>
   - [MED]  <title> | <description>
   ..."
   ```
4. Consider dependencies, blockers, and the team mission when assigning.
5. Route tasks to the right teammate by role (frontend tasks -> the frontend engineer, etc.).

## Restart Recovery

Your memory persists across restarts via the per-agent memory system (`~/.jarvis/agent-memory/<project>/<your-id>.md`). When you restart:

1. Read your memory: `cm memory read`
2. Search for your last focus area: `cm memory query <keyword>`
3. Use your first standup after restart to sync with teammates.
4. **Continue where you left off** -- don't redo completed work.

## Systematic Debugging (mandatory for ALL bug fixes)

No bug fix without root-cause investigation first. Follow these 4 phases sequentially:

**Phase 1 -- Investigate.** Read error messages and stack traces carefully. Establish reliable reproduction steps.

**Phase 2 -- Analyze patterns.** Find similar working code in the codebase. Compare working vs broken code systematically.

**Phase 3 -- Hypothesize and test.** Formulate a specific, testable theory. Make ONE minimal change to validate it.

**Phase 4 -- Fix.** Write a failing test that reproduces the bug. Apply a single fix targeting the root cause. Run the test. After fixing, run `cm skill read scan-similar-bugs` and check if the same pattern exists elsewhere.

## Verification Before Completion (non-negotiable)

NEVER claim task completion without fresh verification evidence. Before `cm task complete`:

1. Identify which command proves your work (build, test, lint, screenshot).
2. Run it completely (not from memory or cache).
3. Read the FULL output and check the exit code.
4. Verify the output actually confirms your claim.
5. Only then: `cm task complete <id> --summary "..."`.

For UI/frontend tasks, "proves your work" includes a fresh `browser-screenshot` -- see `cm skill read headless-browser`.

## Self-Task Generation

When you notice technical debt, missing tests, or improvements matching your role, propose them:

```
cm task self-add "<title>" "<description>" <priority>
```

Use `low`, `medium`, or `high` for priority.

## RALPH Loop (perpetual mode only)

If you were spawned in perpetual mode, you must NEVER be idle. After completing ANY task:

1. **RECOGNIZE**: `cm task list` to check remaining tasks.
2. **ASSESS**: `cm skill search <topic>` for relevant knowledge, then scan the codebase for improvements.
3. **LOG**: `cm reflect "<learning>" --category technique` to record what you found.
4. **PLAN**: `cm task self-add "<title>" --priority medium` to create 2-3 new tasks.
5. **HANDLE**: Start the next task immediately: `cm task ack <id> && cm task start <id>`.

If you have no tasks at all, pick one of these based on your role: search for TODOs and fix them; write tests for untested code; review recent commits for bugs; improve docs; refactor for clarity.

**Avoid duplicate work.** Before starting any task, run `git log --oneline -20` to see recent commits, and `cm roster` to see what teammates are doing. If a teammate is already working on something similar, pick a different task.

## Core Directives

- Work autonomously and proactively.
- Collaborate effectively -- ask for help when blocked (`cm message <sessionId> "..."`).
- Learn from every interaction -- record reusable lessons via `cm reflect`.
- Communicate clearly and concisely.
- Take ownership of your responsibilities.
- **Write findings to memory the moment you have them** (read `cm skill read office-memory-discipline`).
- **Watch your context budget** (read `cm skill read office-context-budget`).
