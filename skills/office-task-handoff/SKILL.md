---
name: office-task-handoff
description: Structured handoff format that office agents must write to memory before cm task complete. Captures what was done, files touched, decisions made, follow-ups, and commit hashes so the next agent (or your future self after restart) can pick up without re-deriving.
---

# Office Task Handoff

A task is not done when the code works. A task is done when **someone else can pick up where you stopped without reading your context window**. That someone else might be:

- Your future self after a respawn.
- A teammate who needs to extend your work.
- The leader assigning a follow-up.

The mechanism is the `--summary` flag on `cm task complete`. This skill is the format.

## The mechanism

```bash
cm task complete <task-id> --summary "<your handoff>"
```

The `--summary` text is automatically appended to your memory under `section: task_history`. You do not need to write to memory separately for this.

## The format

A good summary is **2-4 sentences** containing:

1. **What was the goal?** (one short clause)
2. **What changed?** (files or modules, not every line)
3. **Verification.** (which command proved it -- test name, build target, screenshot path)
4. **Anything the next person should know.** (only if non-obvious)

Optionally, prefix with the relevant commit short-hash.

## Examples

### Backend bug fix

```
cm task complete A-127 --summary "Fixed login regression on HTTP listener (commit a3f7c91). Root cause: Secure cookie flag was set globally from isHTTPS, so browsers rejected cookies on the HTTP port. Now per-request from r.TLS in internal/web/auth/manager.go. Verified: go test ./internal/web/auth/... PASS, manual login on http://server:3555 works."
```

### Feature addition

```
cm task complete B-43 --summary "Added cm memory section/query subcommands plus matching HTTP endpoints. Files: cmd/cm/main.go, internal/web/handler/office.go, internal/office/memory.go, server.go (route reg). Verified: go build ./..., go test ./internal/office/..., curl POST/GET against running container. Next: docs in office-memory-discipline skill (already updated)."
```

### Refactor

```
cm task complete C-12 --summary "Slimmed kickstart.go from 280 to 118 lines by extracting protocol blocks into office-protocols skill and replacing flat skills list with BuildSuggestedSkillsSection. Verified: go test ./internal/office -run TestKickstart, prompt size now ~9100 chars (was ~16500). No behavioral change for spawned agents -- they read the skill on demand."
```

### Investigation that produced no code change

```
cm task complete D-8 --summary "Investigated reported standup duplicate-message bug. Could not reproduce after 20 standup cycles in office-7 template. Checked manager.go message dedup (line 412) -- looks correct. Closing as not-reproducible. If it recurs, capture the standup state at ~/.jarvis/office-memory/standups/<ts>.json before reporting."
```

### Stuck / handing off mid-work

If you can't complete the task in this session, do NOT use `cm task complete`. Use:

```
cm task blocked <task-id> "<one-line blocker>"
```

Then write a fuller handoff to memory:

```
cm memory section current_focus "BLOCKED on task A-127. Need: confirmation from Sage on whether the cookie should be Secure on Cloudflare-fronted HTTP. Last state: implemented per-request flag, all tests pass, but unsure of edge case for X-Forwarded-Proto. Files modified: internal/web/auth/manager.go, security.go. No commits yet. Resume by re-reading manager.go:isRequestSecure."
```

The next spawn will read `current_focus` first thing.

## What NOT to put in a summary

- **Diary text.** "I started by reading the file, then I noticed..." -- this is what your context window is for, not memory.
- **Restated requirements.** The task already has a description. Don't repeat it.
- **Reasoning chains.** Keep that in `decisions` if it's important; don't bloat the summary.
- **Apologies / hedges.** "I think this should work but..." -- if it's not verified, mark it blocked, not complete.
- **Long stack traces or output.** Reference the path: "see /tmp/test-output-A127.txt".

## Why this matters

The task_history section is the ledger your future self reads on respawn. If every task summary is a clean 2-4 sentences, after 20 tasks you have a project diary that fits in 2-3KB and tells you exactly what's been done. If every summary is sloppy, it grows to 20KB and you start ignoring it -- and then you re-derive everything.

Keep it tight.

## Verification

Before marking complete, ask: "If I disappeared and a new agent took over with only my memory file, could they continue?" If no, expand the summary or block the task instead.
