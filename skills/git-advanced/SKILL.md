---
name: git-advanced
description: Advanced git for office agents -- git worktree (the feature jarvis uses for agent isolation), git bisect for regression hunting, git cherry-pick across branches, git reflog recovery, interactive rebase, conflict resolution playbook, and git stash discipline. Read this when office-protocols isn't enough.
---

# Git Advanced

office-protocols covers the task workflow: ack, start, progress, complete. This skill covers the git mechanics underneath -- the commands that actually move code around. Every worker-tier agent in jarvis-go lives in a git worktree on its own branch (`agent/<id>`), so you must be fluent in worktree operations at minimum.

## Cardinal rule

> Every dangerous git command has a non-dangerous alternative. Prefer the non-dangerous one. If you're about to type `--force`, `--hard`, or `--orphan`, stop and look for another path first.

## git worktree -- the jarvis isolation primitive

Each worker agent gets its own checkout + branch via `internal/office/worktree.go`:

```bash
git worktree list                          # see all worktrees
git worktree add -B agent/vex ~/.worktrees/vex   # create worktree + branch
git worktree remove ~/.worktrees/vex             # clean removal
git worktree remove --force ~/.worktrees/vex     # if there are uncommitted changes
```

**You should rarely run these directly** -- the office manager creates and cleans worktrees for you. But when you need to:

- **Inspect another agent's work**: `git -C ~/.worktrees/<agent-id> log --oneline -10`. Don't cd into their worktree, use `-C` to stay in yours.
- **Compare two agents' branches**: `git log agent/bolt..agent/vex` shows what bolt has that vex doesn't.
- **Clean up orphan worktrees**: `git worktree prune` removes stale metadata if a worktree dir was deleted manually.

## git bisect -- regression hunting

A test passes on commit A, fails on commit B. `git bisect` finds the exact commit that introduced the failure in O(log N) steps.

Manual bisect:
```bash
git bisect start
git bisect bad HEAD            # current is broken
git bisect good <known-good>   # last known good commit
# git checks out the midpoint
<run the failing test>
git bisect bad                 # or `good`, based on result
# ... repeat ~log2(N) times
git bisect reset               # return to original HEAD
```

**Automated bisect** is the real win. `git bisect run` executes a script that returns 0 (good) or non-zero (bad):
```bash
git bisect start HEAD <known-good>
git bisect run go test -run TestSpecificRegression ./internal/office/
git bisect reset
```

Exit 125 means "skip this commit" (the test can't run here, e.g. build broken). 0 = good, 1-124 or 126+ = bad.

## git cherry-pick -- moving one commit to another branch

When a fix you wrote on `agent/bolt` also needs to land on `main`:
```bash
git -C ~/.worktrees/forge fetch
git -C ~/.worktrees/forge cherry-pick <sha-from-bolts-branch>
```

For a range:
```bash
git cherry-pick A..B      # NOT including A, through B
git cherry-pick A^..B     # INCLUDING A, through B
```

If there's a conflict, `git cherry-pick --continue` after resolving, or `git cherry-pick --abort` to back out.

## git reflog -- recovery from "oh no"

`git reflog` shows every HEAD movement, even ones lost by `reset --hard`, `rebase`, or `branch -D`. If you just destroyed work:
```bash
git reflog                                    # find the lost commit's sha
git checkout -b recovery <lost-sha>           # save it to a new branch
```

Reflog entries expire after ~90 days. They're your insurance policy.

## Interactive rebase -- rewriting history

Cleaning up 5 ugly commits into 1 clean commit:
```bash
git rebase -i HEAD~5
```

This opens your editor with lines like:
```
pick a1b2c3d First step
pick e4f5g6h Fix typo
pick h7i8j9k WIP
pick k0l1m2n Fix the bug
pick n3o4p5q Tests
```

Change `pick` to:
- `r` / `reword` -- keep commit, change message
- `s` / `squash` -- merge into the previous commit, combine messages
- `f` / `fixup` -- squash, discard message
- `d` / `drop` -- throw out this commit entirely
- reorder lines to reorder commits

**Never rebase a branch others have pulled.** Agents on `agent/<id>` don't push, so this is safe here -- but it's a habit to protect.

## Conflict resolution playbook

When `rebase`, `cherry-pick`, or `merge` leaves conflicts:

1. `git status` -- shows which files conflict.
2. Open each file, find `<<<<<<< HEAD` / `=======` / `>>>>>>> other`.
3. Choose: keep yours, keep theirs, or merge both. **Read both sides** before choosing.
4. `git add <file>` to mark resolved.
5. `git rebase --continue` (or `cherry-pick --continue` / `merge --continue`).
6. If it's going badly: `git rebase --abort` returns to pre-rebase state.

For more than a few conflicts, stop and escalate via `cm message` to the manager. Mechanical resolution of 20 conflicts is how regressions are born.

## git stash -- temporary shelf

When you need to switch context without committing:
```bash
git stash push -m "mid-login-refactor"   # save + clear working dir
git stash list                           # see all stashes
git stash pop                            # apply most recent, remove from stack
git stash apply stash@{2}                # apply a specific stash, keep in stack
git stash drop stash@{0}                 # delete a stash
```

**Untracked files are NOT stashed by default.** Use `git stash push -u` to include them. `git stash push -a` also includes ignored files (rarely what you want).

Worker agents should prefer **committing work-in-progress** on their own branch over stashing. Stash is ephemeral and survives poorly across restarts; commits on `agent/<id>` persist.

## git reset -- the dangerous one

| Command | What it does | Safety |
|---|---|---|
| `git reset` | Unstage files | safe |
| `git reset --soft HEAD~1` | Uncommit, keep changes staged | safe |
| `git reset --mixed HEAD~1` | Uncommit, keep changes unstaged (default) | safe |
| `git reset --hard HEAD~1` | Uncommit, **discard changes** | dangerous |
| `git reset --hard origin/main` | **Replace your work with remote's** | very dangerous |

Never `--hard` without a reflog safety check first: `git reflog | head -20` to confirm you can recover.

## git log -- search the history

```bash
git log --oneline -20                     # recent commits, one per line
git log --grep "login"                    # by commit message
git log -S "SpecificFunction"             # by code change (added/removed "SpecificFunction")
git log -p -- internal/office/kickstart.go  # full diff history of a file
git blame -L 100,150 internal/web/server.go  # who wrote lines 100-150
```

`-S` (pickaxe) is underused -- it finds commits that changed the number of occurrences of a string. Perfect for "when was this function introduced?"

## Anti-patterns

- **`git push --force`** on a shared branch. Use `--force-with-lease` if you must force.
- **`git reset --hard` without checking reflog first.** You can't undo without reflog.
- **`git clean -fdx`** by habit. It deletes untracked files including `.env` and IDE state.
- **Rebasing after someone else has pulled.** Rewrites history they already have.
- **`git checkout .`** to "revert" -- same as `--hard` but silently. Use `git restore` instead for clarity.
- **Committing directly on a worktree you don't own.** Stay on `agent/<your-id>`.
- **`git commit --amend` on a pushed commit.** Amend is for *local* tidying only.

## Verification

After any destructive-seeming operation:
1. `git reflog | head -5` -- verify your previous state is recoverable.
2. `git status` -- verify the working tree matches your mental model.
3. `git log --oneline -5` -- verify the history is what you intended.

If any of those surprise you, `git reflog` back to safety before continuing.
