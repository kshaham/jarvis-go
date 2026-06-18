---
name: office-standup
description: How office agents draft a standup response that draws from memory and recent commits rather than re-deriving status from context. Strict three-bucket format -- yesterday, today, blockers -- one to two lines per bucket.
---

# Office Standup

When you receive a `[STANDUP]` prompt, the cheap and correct way to respond is **not** to scroll through your context history. It's to pull from the memory you've been writing all along.

This skill is the format and the recipe.

## Why memory-first

If you do this:

```
# bad: re-derive from context
"Yesterday I worked on the login refactor. I read login.go and session_store.go,
then I made some changes... let me check what I did..."
```

You burn 20-30 turns of recall. You also forget half of it.

If you do this:

```
# good: pull from memory
cm memory section current_focus
cm memory section task_history    # or: cm memory query "$(date -d yesterday +%Y-%m-%d)"
git log --oneline --author="$(whoami)" -5
```

You have authoritative facts in three commands. The standup writes itself.

## The format

Three buckets, one or two lines each. No filler. No "I think" or "I believe."

```
Yesterday: <what I shipped + commit short-hashes if any>
Today:     <what I'm working on right now (current_focus)>
Blockers:  <none, OR one-line blocker>
```

That's it. The whole response should be 4-6 lines including labels.

## The recipe

Run these in this order on every standup:

```bash
# 1. What is my current focus right now?
cm memory section current_focus

# 2. What did I finish recently?
git log --oneline --author="$AGENT_GIT_AUTHOR" -5
# or, if git author isn't set:
git log --oneline -5

# 3. Do I have unresolved blockers?
cm memory query BLOCKER

# 4. Compose the response
```

Then submit:

```bash
cm standup respond "Yesterday: <line>
Today: <line>
Blockers: <line>"
```

## Examples

### A backend engineer with two completed tasks and one in flight

```
Yesterday: Shipped task A-127 (per-request Secure cookie fix, commit a3f7c91) and A-128 (HSTS gating, commit b81d220).
Today: Task A-129 -- migrate /api/auth/refresh to session cookies. Step 2 of 4.
Blockers: None.
```

### A frontend engineer with one blocker

```
Yesterday: Wired the new "Talk to Jarvis" pill button into office.html (commit 4a2b13c). Visual verified via browser-screenshot.
Today: Hooking up the voice-state pulsing animation in concierge.js.
Blockers: Need Sage's call on whether to use the existing concierge gradient or a new red-state gradient. Pinged in #design.
```

### A QA engineer with nothing in flight

```
Yesterday: Generated tests for SessionStore (commit 9c1f0aa), all passing.
Today: Brainstorming the next test gap -- starting with auth/refresh which has no e2e coverage.
Blockers: None.
```

### After a respawn (fresh context)

If you just restarted, your standup should reflect that explicitly so the team knows you've re-anchored:

```
Yesterday: (Restarted -- summary from task_history) Shipped A-127, A-128. Memory section current_focus says I was mid-A-129 step 2.
Today: Resuming A-129 step 2 from memory.
Blockers: None.
```

This is also a good signal to the leader that you successfully recovered.

## Anti-patterns

- **Vague verbs.** "Worked on auth." -- worked how? Pulled what commits? Be specific.
- **Multi-paragraph essays.** Standup is sync, not a status report. Three lines.
- **"Will continue working on X"** with no specifics. Cite the step number, the file, or the next concrete sub-task.
- **Hiding blockers.** If you're stuck, say so. The whole point of standup is unblocking.
- **Reading your full memory file** (`cm memory read`) just to write a standup -- that defeats the point. Use `section` and `query`.

## Frequency

Standups happen on the manager's schedule -- you do not initiate them. When you receive `[STANDUP]`, run the recipe and submit immediately. Don't queue it.

## Verification

A good standup response is < 400 characters. If yours is longer, you're padding. Cut it.
