---
name: office-code-review
description: How one office agent reviews another agent's work on a sibling worktree before it gets merged to main. Covers reading a teammate's branch without switching, running their tests, leaving focused feedback via cm message, the "2 must-fix + 2 nice-to-have" cap, and escalation to the manager when reviewer and author disagree.
---

# Office Code Review

`review-changes` (a generic pre-commit skill) covers reviewing your own staged diffs. This skill covers the different problem: **one office agent reviewing another agent's completed branch** before it gets merged.

In jarvis-go, Forge is the only agent who merges to `main`. But code review should happen *before* Forge merges -- ideally by a peer who owns the touched area (Bolt for backend changes, Vex for frontend, Lint for test coverage). This skill is the peer-review workflow.

## Cardinal rule

> Leave at most 2 must-fix and 2 nice-to-have comments. More than that means you should be pairing, not reviewing asynchronously.

## When to request / perform a review

Review is appropriate when:
- A completed task touches >100 lines across >2 files
- A task changed shared infrastructure (auth, middleware, state store, office manager)
- A task added a new HTTP endpoint or cm subcommand (API surface)
- The author themselves flags "I'm not sure about X, please review"

Review is NOT appropriate for:
- Typo fixes, docs-only changes, style-only cleanups
- Urgent hotfixes where the clock matters more than perfection (use a follow-up review)
- Work that's still mid-flight (wait for `cm task complete`)

## The workflow

1. **Get a review request.** Either the author sends `cm message <your-sessionId> "Please review task A-127 on agent/bolt"` or the manager assigns you via leader tasks.

2. **Read the task and the author's summary first.** Before looking at code:
   ```bash
   cm task list --agent bolt --status completed
   cm memory query A-127   # may surface the author's decisions/notes
   ```
   You need to know the goal before you judge the execution.

3. **Fetch their branch without leaving your worktree.** You do NOT check out their branch -- you read it from your own worktree using `-C` or `git log`:
   ```bash
   git fetch
   git log --oneline agent/bolt -10
   git log agent/bolt -p -- internal/web/auth/manager.go   # full diff on one file
   git diff main..agent/bolt                                # the whole branch delta
   ```

4. **Run their tests, from your worktree, against their branch.** Use a detached checkout in a *separate* temp worktree so you don't disturb your own:
   ```bash
   review_dir=$(mktemp -d)
   git worktree add --detach "$review_dir" agent/bolt
   ( cd "$review_dir" && go test ./... )
   git worktree remove --force "$review_dir"
   ```
   (See `git-advanced` skill for worktree mechanics.) If their tests don't pass on their own branch, that's a must-fix before any other review.

5. **Read the diff through the review lens** -- see the checklist below.

6. **Write focused feedback.** Cap at 2 must-fix and 2 nice-to-have:
   ```
   cm message <author-sessionId> "Reviewed task A-127.

   MUST FIX (blocks merge):
   1. internal/web/auth/manager.go:84 -- Secure cookie flag still reads the
      global isHTTPS, not r.TLS. Fix the same way the plan described, per-request.
   2. The new isRequestSecure helper is not covered by tests. Add at least
      one case for r.TLS != nil and one for X-Forwarded-Proto.

   NICE TO HAVE (do if time):
   - isRequestSecure could take a *http.Request instead of bool params -- tidier.
   - The new cookie helper duplicates the expiration constant from session.go.

   Overall this is close. Ship once the two must-fixes are done."
   ```

7. **Wait for revisions.** The author fixes, re-runs tests, then messages you back.

8. **Approve or escalate.** If the revised branch passes your must-fixes and tests, reply:
   ```
   cm message <author-sessionId> "Approved. Forge can merge agent/bolt."
   ```
   If you and the author disagree, escalate via:
   ```
   cm message <manager-sessionId> "Disagreement on task A-127 approach. Author wants X, I think Y. Can you decide?"
   ```
   Do NOT go back and forth more than twice. Escalation is faster than endless message threads.

## The review checklist

Check each in order. Stop at the first must-fix category that applies.

### 1. Does it do what the task asked?

Compare the task description to the diff. If the diff doesn't match the task, that's a must-fix.

### 2. Do the tests pass, and do they test the right thing?

- All existing tests green? Must-fix if not.
- New behavior has new tests? Must-fix if not (per the "every new behavior should have a corresponding test" principle).
- Tests use `t.TempDir` / `t.Setenv` and don't leak state? Check.

### 3. Does it follow in-repo patterns?

Check the diff against the canonical patterns in these skills:
- `go-http-server` for new endpoints
- `go-http-client` for new outbound calls
- `go-errors` for error wrapping
- `go-slog` for log-line style
- `go-testing` for test shape

Divergence is a nice-to-have UNLESS it breaks existing patterns (e.g. a new handler that doesn't use `decodeBody`/`ok`/`WriteError`).

### 4. Is there anything scary?

- New mutex fields, new goroutines, new channels -- does it pass `go test -race`?
- New shared state -- is the access pattern locked?
- New destructive commands (`rm -rf`, `git --force`, `os.Remove`) -- are they gated?
- Logging secrets or request bodies by mistake? Must-fix.
- SQL injection? Path traversal? Auth bypass? Must-fix.

### 5. Is it reasonable size / scope?

- Is this one logical change or five tangled together? Suggest splitting if the diff spans unrelated subsystems.
- Did they add 1000 lines to fix a bug that could have been 10? Ask why before assuming waste -- there may be a good reason.

## Anti-patterns

- **Nitpicking formatting.** `gofmt` handles this -- review is for meaning, not whitespace.
- **"While you're in there, also refactor X."** Scope creep. Leave a follow-up task via `cm task self-add`.
- **Silent approval.** Always reply explicitly. A silent review stalls the whole team.
- **More than 2+2 comments.** If you have 5 must-fix issues, the author needs a different task, not a review.
- **Reviewing code you don't understand.** If you can't explain what it does, you can't review it. Ask the author, or ask the manager to assign a more-qualified reviewer.
- **Re-running the whole RALPH loop during review.** You're reviewing, not iterating. Don't start new work.
- **Checking out their branch into your own worktree.** That dirties your state. Use `git worktree add --detach` into a temp dir.

## Verification

After a review cycle:
- The author's branch passes `go test ./...` on a fresh `--detach` checkout.
- Your review comments map 1:1 to concrete file:line references (or to a well-defined concept like "missing tests for X").
- You wrote `cm memory append` noting what you reviewed and the outcome, so your history reflects the review effort.
