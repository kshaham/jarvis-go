---
name: office-ask-for-help
description: Ask the team lead (or a non-busy teammate) a question and block on the answer with a timeout. Use when you are stuck mid-task and would otherwise spin for more than two turns on the same blocker. Prefer this over silently retrying or jumping straight to `cm task blocked`.
---

# Asking for help, the right way

When you cannot make progress on the current task, the office gives you a structured question primitive: `cm ask`. It picks the team lead first, falls back to a non-busy teammate by role-affinity if the lead is unavailable, and blocks your shell on the answer with a timeout. Use it before falling back to `cm task blocked` -- a real answer from a teammate is almost always faster than the blocked-task triage path.

## When to use it

Use `cm ask` when ALL of these are true:
- You have already tried at least one approach that did not work.
- You are about to retry the same kind of step (more searches, more reads) without new information.
- The question has a *concrete* answer a teammate could give in a sentence or two.

Do not use it for:
- Polite team chat -- use `cm message <id>` instead.
- Decisions that need group input -- ask the lead to start a brainstorm.
- Long-running diagnosis ("can you go look at this?") -- file a `cm task self-add` for them or hand off the task with `cm task handoff`.

## Command

```
cm ask "<question>" [--about <taskId>] [--wait 5m]
```

- `--about` attaches the question to your current task so the recipient sees the task title in their prompt and can pull context with `cm task list`.
- `--wait` is the *total* time the asker will block. The server splits it: the lead gets the first half, the fallback teammate gets the second. Default is 5m. You can set it as low as `30s` for trivial questions.

## How to write a good question

A good question gives the recipient enough context that they can answer in one shot:

- Bad: `cm ask "where is auth?"` -- too vague.
- Good: `cm ask "the JWT validator in internal/auth/jwt.go rejects tokens with kid header but no kid in the JWKS. is that intentional, or should we fall back to the first key?" --about t-9821 --wait 5m`

Reference file paths, what you've tried, and what you need to decide.

## What happens server-side

1. The office picks the team **lead** (the strategic-tier agent on your team) and writes a `[QUESTION FROM <you>]:` prompt into their session.
2. Your shell blocks on the answer for half of `--wait`.
3. If the lead does not answer in that window, the office picks a **non-busy teammate** (preferring role-affinity for your role) and re-asks them, blocking for the remaining time.
4. If still no answer, your shell exits non-zero with `status: timeout`. The CLI also prints the list of teammates it tried.

## After the call

**On success** (the printer shows `status: answered` and the answer text):
- Integrate the answer and continue your `/goal` loop.
- Do not thank them with a follow-up message unless something specific is unclear -- the answer is already logged in the office message log.

**On timeout** (`status: timeout`):
- Run `cm task blocked <taskId> "<your question + 'asked: <names>'>"`. The office's blocked-task triage path will escalate.
- Do not re-issue the same `cm ask` immediately -- the server enforces a 10-minute cooldown on duplicate questions and the CLI will reject you.

**On `status: no-recipient`** (everyone on the team is busy or there is only you):
- Same as timeout -- mark the task blocked.

## Answering questions from teammates

When you receive a `[QUESTION FROM <name> (qid: <qid>, about: "<title>")]:` prompt, answer with:

```
cm ask answer <qid> "<your answer>"
```

A few rules:
- Answer within ~2 minutes if you can. The asker is blocked; longer waits cause them to fall back or time out.
- If you don't know, say so explicitly: `cm ask answer <qid> "I don't know -- ask the QA lead"`. A "no" unblocks the asker faster than silence.
- If the question reveals a real bug or design issue, after answering, file `cm task self-add` to track it.
- You can list pending questions for you with `cm ask list`.

## Related skills

- `office-goal-loop` -- when to wrap your task in `/goal`. The escape hatch in that skill IS this command.
- `office-task-handoff` -- if the answer is "you're on the wrong task", hand off rather than struggle on.
- `office-protocols` -- the `cm task blocked` fallback path.
