# jarvis-spa · React island

The React island that serves jarvis's `/office-v2` route. Vite + TypeScript
+ Tailwind + Tanstack Query, with a thin shadcn-style component layer.

## Why it exists

The classic `/office` page is a ~9KB hand-rolled vanilla-JS dashboard. It
works, but state cascades, re-renders, and string-built HTML have grown
load-bearing. The React island is an incremental migration target: every
session ports one screen, the classic UI stays canonical until parity is
reached.

## Build

```bash
cd internal/web/spa
npm install          # one-time
npm run dev          # localhost:5174, proxies /api + /sse to :3555
npm run build        # → ../static/public/react/
npm run typecheck
```

The build output lands at `internal/web/static/public/react/` and is
embedded via the existing `//go:embed all:public` directive — no extra
Go-side wiring beyond the `/office-v2` route in `server.go`.

## Fallback when not built

If `internal/web/static/public/react/` doesn't exist (fresh checkout, no
`npm install` run yet), `/office-v2` serves a styled placeholder page
with the build instructions instead of returning a 404. The check lives
in `static.FileExists()` (`internal/web/static/embed.go`) and is covered
by `embed_test.go` for both the dev (filesystem) and prod (embedded FS)
code paths.

## What's in the box

- **Tanstack Query** for SSE-aware caching (mirrors the mobile app's
  pattern). `useOfficeStatus()` polls every 30s and invalidates on
  `office:memberUpdated` / `office:teamSpawned` / `office:teamKilled`.
- **One ported screen**: `AgentsScreen` — agent grid using `AgentCard` +
  `StatusPill` + `TeamHeader`.
- **Auth**: reuses the classic UI's session cookie via `credentials:
  'include'`. CSRF token pulled from `<meta name="csrf-token">` on the
  index.html shell.
- **Tailwind theme**: mirrors `--ds-status-*` and `--ds-radius-*` from
  the classic `design-tokens.css` so the two UIs visually align.

## Adding a new screen

1. Add the API type to `src/api/types.ts`.
2. Add the endpoint method (`fetchJSON('/api/office/...')`) — no wrapping
   service layer; queryFn does the call directly.
3. Add a `useXxx` hook in `src/hooks/` that wraps `useQuery` + SSE
   invalidation.
4. Build the screen in `src/screens/`. Compose existing components
   from `src/components/` before adding new ones.

## Out of scope (current)

- shadcn-ui CLI init — components are written in-house against Tailwind
  + Radix primitives. Run `npx shadcn@latest init` later if the team
  wants the full CLI workflow.
- Routing — single-screen island for now. Wire `react-router-dom` when
  the second screen lands.
- Tests — none yet. Add `vitest` + `@testing-library/react` when the
  component count justifies it.
