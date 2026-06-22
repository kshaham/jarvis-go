---
name: office-goal-loop
description: Wrap multi-step office task work in Claude Code's /goal command so the evaluator decides when the task is truly done. Use when starting any in_progress task that has more than one verifiable step (build passes, tests pass, screenshot matches, etc).
---

# Driving office tasks with /goal

Claude Code 2.1.139+ ships a `/goal` slash command. It pins a completion condition for the session and keeps your turn going across multiple model turns until a separate evaluator model decides the condition is satisfied. While `/goal` is active you do not need an operator (or the office's idle ticker) to nudge you between turns -- you keep working until the evaluator says "done" or you explicitly cancel.

This skill teaches you when and how to use `/goal` while working on office tasks.

> Naming note: the office has a team-level setting called "Goal-Focused mode" (the opposite of perpetual / RALPH mode). That is unrelated to the `/goal` slash command described here.

## When to invoke /goal

**Use it when** the task has more than one verifiable step. Typical signals:
- The task description references tests, builds, lint, screenshots, or a feature working end-to-end.
- You expect to need 3+ tool calls (Edit, Bash, Read) to finish.
- Completion has an objectively checkable criterion you can hand to the evaluator.

**Skip it when:**
- The task is a one-shot edit (typo fix, single-line tweak, README touch-up).
- Completion is purely subjective ("write a fun comment").
- You are inside a meeting (standup / brainstorm) -- those have their own response loops.

## How to invoke /goal for an office task

Run `/goal` *immediately after* `cm task ack <id>` and `cm task start <id>`, before doing the work. Your goal text MUST contain three things:

1. **The functional criterion** -- the exact condition the evaluator can check. Be specific: "all tests in `internal/foo/...` pass" beats "tests pass".
2. **The terminal action** -- always end with `cm task complete <taskId> --summary "<2-3 sentence handoff>"`. The evaluator treats successful exit of this command as proof of completion.
3. **The escape hatch** -- if you get blocked twice on the same step, run `cm ask "<question>" --about <taskId> --wait 5m` (see the `office-ask-for-help` skill). This keeps you self-sustaining instead of looping on the same failure.

### Template

```
/goal Complete office task <taskId> ("<short title>"). Done when:
  1. <functional criterion -- e.g. "go test ./internal/foo/... passes with no failures">
  2. <secondary criterion if any -- e.g. "the new HTTP handler returns 200 on /api/foo">
  3. cm task complete <taskId> --summary "..." has exited 0.
If I am blocked twice on the same step, I will run cm ask "<question>" --about <taskId> --wait 5m before retrying. I will never claim completion without running the verification commands first.
```

### Worked example

Task `t-9821`: "Fix race condition in `internal/office/locks.go` cleanup".

```
cm task ack t-9821
cm task start t-9821
/goal Complete task t-9821 (locks.go cleanup race). Done when:
  1. go test -race ./internal/office/... passes
  2. go vet ./... is clean
  3. cm task complete t-9821 --summary "..." has exited 0.
If blocked twice on the same step, I will run cm ask "<q>" --about t-9821 --wait 5m.
```

Then start working. Do not narrate what /goal will do -- just begin.

## While /goal is running

- Treat `cm task progress <id> "<note>"` as your between-step heartbeat. The office's idle monitor uses these to decide you are not wedged.
- If a tool repeatedly fails, switch approach instead of retrying. The evaluator will not stop a loop you are stuck in.
- Do not start a *second* `/goal` for a different task in the same session -- finish the current one or cancel it first. Multiple concurrent goals confuse the evaluator.
- If the task changes scope while you work (e.g. the lead messages you), re-issue `/goal` with the updated criteria. Old goal is replaced.

## When the evaluator declares done

The evaluator stops your loop after `cm task complete` succeeds. At that point:
- Run `cm task list` to confirm there is more queued work.
- If the next task also has multi-step criteria, repeat this skill.
- If you have nothing queued and you're in perpetual mode, do NOT idle and do NOT conclude the project is finished. Run the improvement cycle from the `office-feature-improvement` skill -- inventory the project's existing features, pick the next most-valuable enhancement or new feature, and `cm task self-add` it. The office will also replenish you, but you should find substantive work proactively rather than wait.

## Anti-patterns

- **Goal text without verification commands.** "Done when the bug is fixed" is unverifiable -- the evaluator cannot check it. Always include a Bash command whose exit code proves the criterion.
- **Goal text that doesn't include `cm task complete`.** Without that terminal action, the evaluator may keep you running after the work is done.
- **Wrapping every task in /goal.** Trivial tasks pay overhead with no benefit. Skip the criteria above.
- **Fighting the evaluator.** If it keeps saying "not done" and you believe you are done, run the verification commands explicitly and paste the output -- do not just re-assert.

## Related skills

- `office-ask-for-help` -- structured Q&A primitive when you cannot make progress.
- `office-task-handoff` -- when you should hand the task to someone better suited rather than ask for help.
- `office-protocols` -- the `cm task ack/start/progress/complete` lifecycle that bookends every /goal loop.
