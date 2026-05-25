---
name: office-skill-discovery
description: How to use cm skill list / search / read without dumping the full skill library into context. Always search then read exactly one skill. Never list unless you really mean to scan all sixty-plus skills. The right discovery flow keeps context cheap.
---

# Office Skill Discovery

You have 60+ skills available. They cover everything from headless-browser visual testing to React Native deployment to debugging discipline. Most of them are not relevant to any given task -- and reading them all would consume your entire context.

This skill is the discipline for finding the right skill cheaply.

## The wrong way

```bash
cm skill list                    # dumps every skill name + path
cm skill read swift-architecture # totally unrelated to your task
cm skill read swift-data         # ditto
cm skill read swift-uikit        # ditto
```

You just spent 5-15% of your context on irrelevant docs. The skill list itself is ~100 lines; each skill body is 100-300 lines.

## The right way

```bash
cm skill search <one or two keywords>      # 5-15 results, just names
cm skill read <the one that matches>       # only the one you need
```

If `search` returns more than 3 hits, read the descriptions (they're in the search output) and **pick exactly one** to read in full.

## When to use which command

| Command | When | Cost |
|---|---|---|
| `cm skill search <kw>` | You know the topic but not the skill name | low (names + 1-line descs) |
| `cm skill read <name>` | You know the exact skill name | medium (full SKILL.md) |
| `cm skill list` | Almost never -- only for genuine inventory | high (all 60+) |

## Picking the right keyword

| Task | Bad keyword | Good keyword |
|---|---|---|
| Test a UI change | `test` | `browser` or `headless` |
| Fix a memory leak | `memory` | `performance` or `profiling` |
| Refactor safely | `code` | `refactor` |
| Add ML/AI feature in Swift | `ai` | `swift-ai` or `coreml` |
| Improve a system prompt | `prompt` | `prompt-engineer` |
| Generate a 3D model | `3d` or `model` | `3d-print` or `mesh` |

If your first search returns 0 results, pick a more general keyword. If it returns 30+, narrow.

## Office-specific skills (the ones you want most often)

These all start with `office-` and live in your skills dir. They're listed in your kickstart's "Suggested skills" section, but for completeness:

- `office-protocols` -- task lifecycle, git workflow, standups, all team protocols
- `office-memory-discipline` -- when and how to use cm memory
- `office-context-budget` -- how to recognize and react to context fill
- `office-onboarding` -- structured first-pass project exploration
- `office-focused-search` -- tool routing for code search
- `office-task-handoff` -- format for cm task complete --summary
- `office-standup` -- format for cm standup respond
- `office-reflection` -- format for cm reflect
- `office-skill-discovery` -- this skill

You should know these by name. Read each one once after your first spawn, and then re-read them only when you forget a step.

## When to skip skills entirely

Don't read a skill if:

- You already read it this session.
- You can solve the task in a few turns without guidance.
- The skill is for a stack you're not working in (don't read swift-* on a Go task).

A skill is a tool, not a ritual.

## What to do after reading

After `cm skill read <name>`, you have absorbed it once -- but it's now sitting in your context. If you need it on every future task, that's wasteful. The fix is to **internalize the rule, then trust the rule**:

1. Read the skill.
2. Apply it to your current task.
3. **Reflect** on the one or two key takeaways:
   ```
   cm reflect "office-focused-search: always set head_limit on Grep, prefer files_with_matches first" --category technique
   ```
4. Next time, you'll see the takeaway in your "Recent learnings" kickstart section without re-reading the full skill.

## Verification

After your first spawn in a project, you should have read at most:
- 1 onboarding skill (`office-onboarding`)
- 2-4 office-* skills relevant to your role
- 0-2 stack-specific skills (e.g. `debug` for backend, `headless-browser` for frontend)

If you're up to 6+ skills read, you're over-investing in tooling and under-investing in shipping. Start the work.
