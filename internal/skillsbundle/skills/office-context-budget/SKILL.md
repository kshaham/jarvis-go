---
name: office-context-budget
description: Three context-fill thresholds (~50%, ~70%, ~85%) and the action to take at each. How office agents recognize that their context is filling and what to do about it -- offload to memory, then /compact, then step away. Read this once and re-apply continuously.
---

# Office Context Budget

Your context window is finite. If you fill it, you start losing earlier turns -- sometimes silently. The fix is not "be more efficient" -- the fix is to **offload working state to memory before you need to**.

This skill defines the three thresholds and the action at each. Apply them continuously, not just when you notice you're slow.

## How to estimate your own context usage

You do not have a precise token counter in-session. You do have these proxies:

- **Number of files you've Read recently.** ~5 large files (>500 lines each) = ~25-40% of a 200k context.
- **Number of large tool outputs you've kept.** A `git log --oneline -200`, an `ls -R`, or a non-`head_limit`ed Grep can each cost 5-15%.
- **How long you've been on this task.** Beyond 30-40 turns of dense work, assume you're past 50%.
- **Whether the assistant feels slower or starts forgetting earlier turns.** That is the >85% signal -- by then you should already have acted.

When in doubt, assume you are higher than you think. Acting at 60% is cheap; acting at 90% is too late.

## The three thresholds

### Threshold 1: ~50% -- "consolidate"

You are halfway through. **Stop and write.** Do all of:

```
cm memory section current_focus "<one-paragraph status>"
cm memory append "<any new findings since last write>"
```

Then keep working. No `/compact` yet -- you have plenty of room.

### Threshold 2: ~70% -- "offload then compact"

You are getting full. Do all of the following, in order:

1. **Write everything you'd lose** to memory:
   ```
   cm memory section current_focus "<exact state right now>"
   cm memory section decisions "<any new decisions made>"
   cm memory append "<remaining loose facts>"
   ```
2. **Report progress on your current task** so the manager knows you're alive:
   ```
   cm task progress <task-id> "Hit ~70% context, compacting now."
   ```
3. **Run `/compact`.** This is a Claude Code command -- type it as a slash command, not via Bash. It compresses your earlier turns into a summary while preserving the recent ones.
4. After compaction, your first action should be `cm memory section current_focus` (to re-anchor).

### Threshold 3: ~85% -- "step away"

You are nearly out. Compaction may not save you because the compacted summary itself takes space. Do:

1. **Final memory dump.** Same as threshold 2 step 1, but be exhaustive.
2. **Mark current task blocked or complete.** If the task is finishable in a few turns, race to finish with `cm task complete <id> --summary "..."`. Otherwise:
   ```
   cm task blocked <id> "Approaching context limit. Resuming after restart -- see memory section current_focus."
   ```
3. **Do not start new work** in this session. Wait for the manager to respawn you, or for you to be naturally restarted by the recovery loop.

## How to avoid hitting Threshold 3 in the first place

The patterns that fill context fastest:

| Pattern | Cost | Better |
|---|---|---|
| `Read` a file > 500 lines | 5-10% | Use `Grep` with `files_with_matches`, then `Read` only the relevant range |
| `cat` / `ls -R` of a big directory | 5-15% | `Glob` with a specific pattern |
| `git log` without `--oneline` or `-N` | 5-10% | `git log --oneline -20` |
| Pasting a large tool output into your reasoning | 5-20% | Save it to a file in `/tmp/`, reference by path |
| Re-reading a file you already read | duplicates the cost | Read once, write key facts to memory |
| `cm memory read` (whole file) | grows over time | Use `cm memory section <name>` or `cm memory query <kw>` |
| `cm skill list` (every spawn) | wasteful | `cm skill search <kw>` then `cm skill read <one>` |

For the full search-tool routing rules, read `cm skill read office-focused-search`.

## The discipline in one line

> Write to memory more often than you think you should; read from memory more often than you re-read files.

If you follow that, threshold 1 takes care of itself, threshold 2 is rare, and threshold 3 never happens.

## Verification

At any point, you should be able to answer "where am I?" by running `cm memory section current_focus` -- not by scrolling back through your context.
