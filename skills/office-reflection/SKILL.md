---
name: office-reflection
description: Templates for cm reflect that yield reusable lessons rather than diary entries. The pattern is "When you see X, prefer Y because Z." The anti-pattern is "Today I worked on X." Reflections are durable, cross-spawn, and feed cross-agent learning -- treat them as gifts to your future self.
---

# Office Reflection

`cm reflect` writes a learning entry to your durable learning store (`~/.jarvis/learnings/<your-id>.json`). Unlike `cm memory` (project-scoped scratchpad), reflections:

- Are cross-project. They follow you to every codebase.
- Are auto-distilled. After 30 reflections, the oldest 15 get summarized into one-line "distilled insights" that feed your kickstart.
- Are surfaced to teammates via the cross-agent context that every spawn reads.

This means a good reflection is read **many** times by **many** agents. A bad reflection is noise.

## The cardinal rule

> A reflection captures a **transferable lesson**, not what you did today.

A reflection is good when you can imagine a future agent (yourself or a teammate) reading it on a different task and changing what they do. A reflection is bad when reading it teaches nothing actionable.

## The pattern

```
When you see <X>, prefer <Y> because <Z>.
```

Or one of these variants:

- `<Tool/technique> works for <situation> because <reason>.`
- `Avoid <pattern> when <condition> -- it causes <failure mode>.`
- `<Library> requires <quirk> that isn't documented -- the workaround is <X>.`
- `In <context>, <metric> matters more than <other metric>.`

## Examples

### Good reflections

```
cm reflect "When debugging Go race conditions, run 'go test -race' before reading any code -- the race detector usually points straight at the line." --category technique
```

```
cm reflect "Postgres UPDATE ... RETURNING is faster than SELECT-then-UPDATE for single-row mutations because it's one round trip." --category technique
```

```
cm reflect "chromedp/headless-shell rejects CDP requests whose Host header isn't an IP. Always resolve service name to IP via dns.lookup() before opening the WebSocket." --category pitfall
```

```
cm reflect "When kickstart prompt grows past ~10KB, agents start losing earlier turns. Move static protocol blocks into a skill and reference it via cm skill read." --category technique
```

```
cm reflect "Avoid 'git add -A' in agent automation -- it sweeps in .env files and credentials. Always add specific paths." --category pitfall
```

### Bad reflections (do not write these)

```
"Today I fixed the login bug."
# -> diary, no transferable lesson. Belongs in task_history via --summary.

"I learned that Go has goroutines."
# -> too generic, not actionable, not specific to a context.

"The codebase is complex."
# -> opinion, not a pattern.

"Should refactor manager.go."
# -> a TODO, not a learning. Belongs in cm task self-add.
```

## Categories

The `--category` flag groups learnings for distillation. Use one of:

- `technique` -- a positive pattern: "do X to achieve Y."
- `pitfall` -- a negative pattern: "avoid X because Y."
- `general` -- everything else (use sparingly).

The distiller groups by category, so categorizing accurately makes the distilled insights cleaner.

## When to reflect

Reflect **right after** the moment of learning, not at the end of the day. If you just spent 30 minutes figuring out a quirky API, write the reflection now while the lesson is sharp -- not tomorrow when you've forgotten the details.

Good triggers:
- You hit an error you didn't expect, and the fix was non-obvious.
- You discovered a faster / cleaner way to do something you'd done before.
- A teammate's code review pointed out a pattern you didn't know.
- You read a doc / source comment that contradicted what you assumed.

Bad triggers:
- "I should write something down to look productive."
- "I haven't reflected in a while."

Quality > frequency. One sharp reflection per day beats five vague ones.

## Length

Reflections should be **one to two sentences**. The distiller truncates anything over ~80 chars per item. If you have more to say, the rest belongs in `cm memory append` (project notes), not in a reflection.

## Cross-agent visibility

Other agents see the most recent 3 of your reflections in their kickstart, every spawn. So a sharp reflection from you can save every teammate from the same mistake.

This is also why bad reflections are costly: noise here pollutes everyone's context.

## Verification

Re-read your last 5 reflections via your kickstart's "Recent learnings" section. If any of them read like diary entries, edit your habit -- not the reflection (the store is append-only / auto-distilling).
