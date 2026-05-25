---
name: office-knowledge-search
description: Unified team-wide search across every agent's memory, learnings, standup transcripts, and the team whiteboard. Use this instead of `cm memory query` whenever you want to find what a teammate already figured out, or before starting new work that might duplicate prior effort.
---

# office-knowledge-search

Jarvis used to silo knowledge per agent: your `cm memory query` only searched YOUR memory, and `cm skill search` only checked shared skills. If a teammate solved the same problem an hour ago, you'd only learn about it if the one-line-per-peer kickstart digest happened to surface it — usually it didn't.

`cm knowledge search <query>` is the fix. One command, every durable store, recency-ranked.

## When to use which command

| Task | Command |
|---|---|
| Find what ANY teammate wrote about X | `cm knowledge search <X>` |
| Find what I personally wrote about X | `cm memory query <X>` |
| Find an existing playbook / procedure | `cm skill search <X>` |
| See recent team activity across all agents | `cm knowledge tail` |

**Default to `cm knowledge search`** when starting a task. It costs you one command but can save hours of re-deriving something a teammate already solved.

## What it searches

Every hit the search can produce:

- **memory/<agent>** — line from `~/.jarvis/agent-memory/*/*.md` (sectioned markdown)
- **learning/<agent>** — entries from `~/.jarvis/learnings/*.json` (including auto-distilled insights)
- **standup/<speaker>** — transcript lines from past standup meetings
- **whiteboard/<agent>** — persistent team whiteboard (keyed by agent name, not session — survives respawns)
- **task/<agent>** — active task titles + descriptions from the live roster

Default scope is the **current team's agents + current project's memory**. Pass `--all` to widen to every project on disk.

## How hits are ranked

Higher score = shown first. Score = 1 (matched) + recency bonus + source weight.

- **Recency bonus:** +2 if the source is under 1 hour old, +1 if under 24 hours.
- **Source weight:** +1 for memory / learnings (deliberate writes), +0 for standups / whiteboard.

So a teammate's reflection from 10 minutes ago will outrank a 3-day-old standup line that also matches.

## Examples

### Before starting a task

```bash
# You're about to audit liquid-glass styling on mobile. Has anyone already?
cm knowledge search "liquid glass"

[12s] memory/Dash
  Glass blur regresses on iPhone SE at blur>24 — cap at 20.
[3m] learning/Shield (pitfall)
  Glass overlay triggers iOS hit-test miss when z-index ≤ 0
[1h] standup/Captain
  Liquid-glass audit blocked on Ops restarting Expo dev server
[4h] whiteboard/Captain (tracking)
  19 mobile screens → 14 audited, 5 pending
```

Now you know: Dash already solved the blur regression, Shield found a hit-test gotcha, 14 screens are done, and Ops has an Expo server issue. You don't have to re-discover any of that.

### Catching up on team activity

```bash
# You've been heads-down for an hour. What has everyone else been doing?
cm knowledge tail --limit 10
```

### Searching across all projects (wider net)

```bash
cm knowledge search "rate limit" --all
```

## When NOT to use it

- **Code lookups** — use `cm skill read office-focused-search`, `rg`, `ast-grep`.
- **Current task state** — use `cm task list`, `cm roster`.
- **Live dev URL** — use `cm dev url`.
- **Fetching a specific skill** — use `cm skill read <name>`.

## One-sentence rule

> **Before you start a task that someone else on the team might have touched, run `cm knowledge search` with 1–3 keywords from the task title.** The cost is one command; the upside is avoiding an hour of duplicated work.
