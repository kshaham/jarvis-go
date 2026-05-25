---
name: office-onboarding
description: First-pass project exploration for office agents that produces a structured memory artifact instead of dumping a giant directory tree into context. Use this on your very first task in a new project, or after a long break.
---

# Office Onboarding

When you first land in a project (or come back after a long break) you need a mental model of the codebase. The naive way -- `ls -R`, then `cat` a bunch of files -- burns 30%+ of your context before you've done any real work.

This skill is the disciplined way: capture the essentials in a structured `onboarding` section in your memory, then refer back to it instead of re-deriving.

## When to run this

- Your very first task in a new project.
- After more than 24 hours away from a project.
- After a major refactor by another agent that changed the structure.

If you already have an `onboarding` section in memory and the project hasn't changed much, **read it instead of re-running this skill**:

```
cm memory section onboarding
```

## The pass

Run these in order. After each, write the relevant fact to the `onboarding` section. **Do not save the raw output to context** -- transcribe the relevant line and discard.

### 1. Tech stack

```bash
ls -1                                         # top-level files only
test -f package.json && cat package.json | head -30
test -f go.mod && head -5 go.mod
test -f Cargo.toml && head -10 Cargo.toml
test -f pyproject.toml && head -20 pyproject.toml
test -f Dockerfile && head -10 Dockerfile
```

Write to memory: language, framework, top 3-5 dependencies, container runtime.

### 2. Entry points

```bash
test -f README.md && head -50 README.md       # build/run instructions live here
test -d cmd && ls cmd/                        # Go: per-binary main packages
test -d src && ls -1 src/ | head -20         # JS/TS: top-level source
test -f main.go && grep -n "^func main" main.go
test -f index.ts && head -10 index.ts
```

Write to memory: where the binary/server starts, what port it listens on.

### 3. Build and test commands

Look for the canonical commands. **Prefer reading docs over guessing.**

```bash
test -f Makefile && grep -E "^[a-z][a-z-]*:" Makefile | head -10
test -f package.json && grep -A 20 '"scripts"' package.json
test -f justfile && head -20 justfile
```

Write to memory:
- Build command (e.g. `go build ./...`, `npm run build`, `cargo build`)
- Test command (e.g. `go test ./...`, `npm test`, `pytest`)
- Lint command (e.g. `golangci-lint run`, `npm run lint`)
- Run-locally command (e.g. `go run ./cmd/foo`, `npm run dev`)

### 4. Key directories

```bash
find . -maxdepth 2 -type d -not -path '*/.*' -not -path '*/node_modules*' \
  -not -path '*/vendor*' -not -path '*/dist*' | head -30
```

Write to memory: 5-10 most important directories with one-line purpose each. Examples:
- `internal/web/handler/` -- HTTP handlers
- `internal/office/` -- office agent orchestration
- `cmd/jarvis/` -- main binary entry point

### 5. Existing patterns to imitate

For **your role**, find the canonical example to copy from. Use `office-focused-search` -- do NOT browse.

- Backend: find one existing handler / repository / service that does something similar to your task. Note its file path.
- Frontend: find one existing page/component that uses the same UI patterns. Note its file path.
- QA: find the existing test pattern (table-driven? mocha? pytest fixtures?). Note one example test file path.

### 6. Recent activity

```bash
git log --oneline -20
git log --oneline -5 --all                    # what teammates are doing
```

Write to memory: 1-2 lines describing what the team has been working on lately. This will be stale within hours -- don't dwell.

## The artifact

After the pass, your `onboarding` section should look like this. Aim for under 1500 chars total:

```
cm memory section onboarding "STACK: Go 1.22, chi router, sqlx, Postgres 15. Frontend: vanilla JS + xterm.js. Container: Docker compose.

ENTRY: cmd/jarvis/main.go -> internal/web/server.go on :3555 (HTTPS :3556).

BUILD: go build ./...
TEST:  go test ./...
LINT:  golangci-lint run
RUN:   docker compose up -d jarvis

KEY DIRS:
- internal/web/handler/   -- HTTP handlers
- internal/web/server.go  -- chi route registration
- internal/office/        -- agent orchestration
- cmd/cm/                 -- agent CLI
- skills/                 -- shipped agent skills

PATTERN TO IMITATE: internal/web/handler/office.go has the canonical handler shape (decodeBody + ok/WriteError).

RECENT: Team is mid-refactor of memory subsystem (sectioned schema). Voice concierge work paused."
```

Then on every subsequent task in the same project, you start with:

```
cm memory section onboarding
```

That's a few hundred bytes of context vs. the 5000+ bytes of `ls -R` output you would have read otherwise.

## Verification

After onboarding, you should be able to answer these without reading any files:

- "How do I run the test suite?"
- "Where does the HTTP server start?"
- "Which file should I copy from when adding a new endpoint?"

If you can't, your onboarding section is incomplete -- add to it.
