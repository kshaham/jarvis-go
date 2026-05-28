# Contributing to jarvis

Thanks for considering a contribution. This guide covers everything from
"how do I get the thing running" to "what does a clean PR look like."

## 🛠️ Dev setup

Prerequisites:

- **Go 1.25+** — `brew install go` (macOS) or `asdf install go 1.25.0`
- **Node 20+** — only required if you'll touch `internal/web/spa/`
- **`screen`** binary on `$PATH` (required by PTY-mode sessions; pre-installed on macOS, `apt install screen` on Debian/Ubuntu)
- **`claude` CLI** on `$PATH` — required by the agent runtime to actually spawn sessions

Optional:

- **Docker + Docker Compose** — only if you want to run the bundled visual-verification headless browser
- **`golangci-lint`** — `brew install golangci-lint` for the same checks CI runs

Then:

```bash
git clone git@github.com:kshaham/jarvis.git
cd jarvis
make build                       # produces ./jarvis (+ ./cm, ./jarvis-mcp)
./scripts/install-skills.sh      # symlinks the 54 bundled skills into ~/.claude/skills/
./jarvis web --port 3555         # boots; open http://127.0.0.1:3555
```

For the SPA:

```bash
cd internal/web/spa
npm ci
npm run dev                      # Vite dev server with proxy to :3555
# or:
npm run build                    # production build → internal/web/static/public/react/
```

## 🧪 Tests

```bash
go test ./... -race -count=1 -timeout 180s    # full suite, race-flagged
go test ./internal/peers/...                  # one package
go test -run TestRelayInbound ./...           # one test
```

The SPA has no test suite yet (the mobile app does — see the companion repo).

## 🌳 Branching + commits

- Branch off `main`. Naming: `feature/<slug>`, `fix/<slug>`, `refactor/<slug>`, `docs/<slug>`, `chore/<slug>`.
- Conventional commits: `feat: add X`, `fix: handle Y`, `refactor: extract Z`, `docs:`, `test:`, `chore:`.
- Keep commits atomic. Run `go vet ./... && go build ./... && go test ./...` before pushing.
- One PR = one logical change. Resist the urge to "fix this while I'm here" in the same PR unless it's a tiny obvious cleanup.

## ✅ PR checklist

- [ ] `go vet ./...` clean
- [ ] `go build ./...` clean
- [ ] `go test ./... -race` clean (or annotated flakes documented)
- [ ] SPA `npx tsc --noEmit && npm run build` clean if you touched `internal/web/spa/`
- [ ] No new `/Users/…`, hostname, or credential strings in the diff
- [ ] Commit messages explain *why*, not just *what* (the diff already shows what)
- [ ] If the PR adds a new API endpoint, the relevant `useOfficeData.ts` / `usePeers.ts` hook is updated and a screen consumes it (or there's an issue tracking that)

## 🧭 Architecture in one paragraph

`jarvis` is a Go daemon that orchestrates Claude Code agents (the "office"). Operators interact via a CLI (`cm`), a React SPA (`/office-v2`), and a companion React Native mobile app. The daemon manages agent lifecycles, persists a knowledge library to `~/.jarvis/`, and exposes an HTTP + SSE API. Deep dive in [`README.md`](README.md#-architecture) and the agent-runtime walkthrough in [`EXPLAINER.md`](EXPLAINER.md).

## 🔌 Adding a new feature

A typical "new screen with one new API endpoint" PR touches:

1. `internal/office/<thing>.go` — domain logic
2. `internal/web/handler/<thing>.go` — HTTP handler
3. `internal/web/server.go` — route wiring
4. `internal/web/spa/src/api/types.ts` — TS type for the response
5. `internal/web/spa/src/hooks/useOfficeData.ts` — TanStack Query hook + SSE invalidation
6. `internal/web/spa/src/screens/<Thing>Screen.tsx` — UI
7. `internal/web/spa/src/App.tsx` — route registration
8. `internal/web/spa/src/components/AppShell.tsx` — left-rail entry

The SPA's design tokens live in `internal/web/spa/src/styles/tokens.css` and motion primitives in `internal/web/spa/src/components/motion/`. Every screen should source from those, not redefine its own.

## 🤝 Code review

Reviewers expect:

- Clear problem statement in the PR description
- Test plan (commands run, screenshots if UI)
- Honest "what's still TODO / out of scope" callout

Things reviewers will push back on:

- Mocking the database in tests (we got burned by mock/prod drift; integration tests use a real temp dir)
- Adding error-handling for impossible cases ("framework guarantees this is non-nil")
- Backwards-compat shims when the codebase has only one caller — just change the caller

## 🆘 Where to get help

- 💬 Open a GitHub Discussion
- 🐛 File an issue with `bug_report.md` template
- ✨ Request a feature with `feature_request.md` template
- 🔒 Security issues — see [`SECURITY.md`](SECURITY.md), do not file public issues

Thanks again 🙏
