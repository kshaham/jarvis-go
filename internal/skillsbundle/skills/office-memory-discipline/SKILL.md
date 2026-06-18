---
name: office-memory-discipline
description: How office agents should use cm memory and cm reflect so working state lives on disk, not in the context window. Includes templates for memory entries and the cardinal rule -- write findings the moment you have them, not at the end of the day.
---

# Office Memory Discipline

Your context window is finite. Your memory file is not. Anything you would otherwise have to "remember" should live on disk, indexed by section, retrievable by `cm memory query`.

This skill teaches you the cardinal rule and the exact commands.

## Cardinal Rule

**Write findings to memory the moment you have them.** Not at the end of the task. Not at the end of the day. The moment.

If you just discovered that `internal/foo/bar.go:42` has a bug, write a note to your `blockers` section before you do anything else. If you just decided to use SQLite over Postgres for the new cache, write it to `decisions`. If you just figured out how the auth middleware works, write a one-paragraph summary to `notes`.

The reason: every piece of information you keep "in your head" (in your context window) competes with every other piece. Memory has no such competition.

## The Memory File

Each agent has one memory file per project:

```
~/.jarvis/agent-memory/<projectHash>/<your-config-id>.md
```

It is structured into sections you can read, write, append, and search:

| Section | Purpose | Use |
|---|---|---|
| `current_focus` | What you're working on right now | Overwritten as focus changes |
| `decisions` | Architecture / implementation choices and *why* | Append new decisions |
| `blockers` | What is blocking you | Append + remove when resolved |
| `files_owned` | Files you have made non-trivial changes to | Append-only |
| `task_history` | Auto-written by `cm task complete --summary` | Never write directly |
| `notes` | Catch-all scratchpad | Append-only |
| `onboarding` | Project tech-stack snapshot from your first pass | Written once -- see `office-onboarding` |

## Commands

```bash
# Read everything (use sparingly -- prefer query)
cm memory read

# Read one section (cheap, focused)
cm memory section current_focus

# Overwrite a section (e.g. when current_focus changes)
cm memory section current_focus "Migrating user auth from JWT to session cookies. Step 2 of 5 -- updating /api/auth/login."

# Append to a section (without overwriting)
cm memory append "Found that internal/auth/middleware.go has a stale TODO from 2024 about CSRF."

# Search across all sections
cm memory query CSRF
cm memory query "auth middleware"
```

There is also `cm reflect` for *durable lessons* (different from notes -- see below).

## When to write to memory vs when to reflect

| Use this | When |
|---|---|
| `cm memory section current_focus "..."` | Your task focus changes |
| `cm memory section decisions "..."` | You make an architectural choice |
| `cm memory append "..."` (notes) | You discover a transient fact about this project |
| `cm reflect "<lesson>" --category technique` | You learn a **reusable** pattern that will help on future tasks (in this project or others) |

A reflection is for things like:
- "When debugging Go race conditions, run `go test -race` first before reading code."
- "Postgres `UPDATE ... RETURNING` is faster than SELECT-then-UPDATE for single-row mutations."

A note is for things like:
- "The user ID column in `users` is called `account_id` not `user_id`."
- "There's a circular import risk between `internal/auth` and `internal/session` -- be careful."

Reflections live in `~/.jarvis/learnings/<id>.json` and are auto-distilled when they pile up. Notes live in your project memory file.

## Templates

### Decision template

```
cm memory section decisions "[$(date +%Y-%m-%d)] Chose <option> over <alternatives> because <reason>. Trade-off: <known downside>. Revisit if: <condition>."
```

### Blocker template

```
cm memory append "[BLOCKER] <one-line description>. Affects task <task-id>. Workaround attempted: <what you tried>. Need: <what would unblock you>."
```

When the blocker is resolved, append a follow-up:

```
cm memory append "[RESOLVED] <blocker description>. Fix: <what worked>."
```

### Decision-by-default

If you ever find yourself thinking "I should remember this," that's the trigger. Stop, write it, then continue. The rule is **write before you continue**, not "write when convenient."

## Anti-patterns

- **Hoarding context.** Reading 5 large files, keeping them in your head, then writing memory at the end. By then your context is already full.
- **Diary entries.** "I worked on the login form today." This belongs in `task_history`, written by `--summary`. Don't duplicate.
- **Re-deriving on next spawn.** If you start a new spawn and immediately re-read the same files, you didn't write enough notes the first time.
- **Treating memory as logs.** Memory is for things you'll re-read. If no one will ever re-read it, don't write it.

## A good memory file looks like

```
## section: current_focus
Migrating /api/auth/login from JWT to session cookies. Step 3 of 5 -- updating SessionStore tests.

## section: decisions
- [2026-04-08] Chose Redis over in-memory map for SessionStore because we need
  multi-instance support. Trade-off: Redis is now a hard dependency.
- [2026-04-09] Made Secure cookie flag per-request (from r.TLS), not global.
  Reason: HTTP listener cookies were rejected by browsers when global flag was on.

## section: blockers
- [BLOCKER] /api/auth/refresh not yet wired -- waiting on Bolt to finish
  RefreshToken middleware. Workaround: skipping refresh tests for now.

## section: files_owned
- internal/auth/login.go
- internal/auth/session_store.go
- internal/auth/login_test.go

## section: task_history
- [2026-04-08T14:22Z] task-A12 "Wire SessionStore": Replaced JWT extraction
  with cookie-based session lookup. Tests: passing. Files: login.go,
  session_store.go.
- [2026-04-09T10:15Z] task-A13 "Per-request Secure flag": Fixed login
  regression on HTTP listener. Tests: passing. Files: cookies.go.
```

## Verification

After each significant action, ask yourself: "Could a fresh copy of me, reading only my memory file, pick up where I am right now?" If the answer is no, write more.
