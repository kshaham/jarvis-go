# ECC integration

jarvis-go ships a curated subset of [ECC](https://github.com/affaan-m/ECC)
(Claude Code Plugin System, MIT, © Affaan Mustafa 2026). ECC is a much
larger ecosystem (~150 skills, 60+ agents, 75 commands, 14 MCP servers
shipped upstream); jarvis vendors only what complements the office
workflow. License + attribution: see [`THIRD_PARTY_LICENSES.md`](../THIRD_PARTY_LICENSES.md).

## What's vendored

### Skill bundles (`skills/ecc/<name>/SKILL.md` × 22)

| Skill | Surfaced to roles |
|---|---|
| `agent-eval` | manager |
| `agentic-engineering` | — (discoverable via `cm skill search`) |
| `api-design` | architect, backend |
| `architecture-decision-records` | architect |
| `backend-patterns` | architect, backend |
| `browser-qa` | frontend |
| `code-tour` | — |
| `codebase-onboarding` | — |
| `coding-standards` | — |
| `context-budget` | — |
| `continuous-learning` | manager |
| `deep-research` | — |
| `deployment-patterns` | backend, devops |
| `e2e-testing` | frontend, qa |
| `eval-harness` | qa |
| `frontend-design-direction` | frontend, designer |
| `git-workflow` | devops |
| `make-interfaces-feel-better` | (already in jarvis; ECC version overrides) |
| `mcp-server-patterns` | — |
| `security-review` | architect, devops |
| `tdd-workflow` | qa |
| `verification-loop` | manager, qa |

All skills land at `~/.claude/skills/<name>/` via
`scripts/install-skills.sh` (symlinks by default; `--copy` for static
installs). Each `SKILL.md` carries an inline attribution line.

### Task-tool agents (`internal/office/ecc_agents/*.md` × 4)

Wired into `BuildECCAgents()` in `internal/office/ecc.go` and laid
into every worktree's `.claude/agents/` directory at spawn time
(alongside the 3 office-native agents).

| Agent | Purpose |
|---|---|
| `code-architect` | Plans multi-step refactors before code touches |
| `security-reviewer` | Adversarial security review of a diff |
| `pr-test-analyzer` | Diffs PR → flags missing test coverage |
| `code-simplifier` | Proposes simpler alternatives to over-engineered code |

### Slash commands (`internal/office/ecc_commands/*.md` × 3)

Wired into `BuildECCCommands()`. Land at `.claude/commands/`:

| Command | What it does |
|---|---|
| `/security-scan` | Runs the security-audit skill against the agent's worktree |
| `/test-coverage` | Runs project tests and reports gaps |
| `/refactor-clean` | Proposes a structured refactor |

### MCP servers (per-spawn, opt-out)

When enabled, every spawned agent gets these three extra MCP servers
(in addition to jarvis-mcp + codegraphcontext + codegraph):

- `context7` — live library docs lookup. Spawns via
  `npx -y @upstash/context7-mcp`. No credentials needed.
- `sequential-thinking` — structured chain-of-thought for complex
  tasks. Spawns via `npx -y @modelcontextprotocol/server-sequential-thinking`.
- `shadcn` — UI component discovery (metadata only: surfaces component
  definitions, install commands, and audit checklists; doesn't itself
  run code generation). Spawns via `npx -y @jpisnice/shadcn-ui-mcp-server`.

All three require `npx` (Node 18+) on the host's PATH. Missing-Node
hosts silently skip the MCP entries instead of poisoning the spawn —
see `office.NpxAvailable()` in `internal/office/ecc.go`.

`shadcn` is a runtime MCP, not vendored content; it is not tracked in
`skills/ecc/CHECKSUMS.sha256` (that file covers only the on-disk
skill + agent + command bundle).

## Disabling per team

Spawn the team with `disableEcc: true` in the JSON request body, or
flip the "Disable ECC bundle" toggle in the mobile spawn wizard's
Tuning section. When disabled:

- The 4 ECC agents are NOT written to `.claude/agents/`.
- The 3 ECC commands are NOT written to `.claude/commands/`.
- `context7`, `sequential-thinking`, and `shadcn` MCP entries are NOT added.
- The skill recommender still surfaces ECC-named entries from the
  host's `~/.claude/skills/` (operators can `rm -rf
  ~/.claude/skills/<name>` to fully exclude — symlinks just unlink).

The flag mirrors `disableCodegraph` exactly; see
`internal/web/handler/office.go` `eccOptOut()` for the helper.

## Refreshing from upstream ECC

When pulling new versions:

1. Identify the target ECC commit / tag. Note it in this doc.
2. Re-fetch the affected `.md` files via:
   ```
   curl -fsSL https://raw.githubusercontent.com/affaan-m/ECC/main/skills/<name>/SKILL.md > skills/ecc/<name>/SKILL.md
   ```
3. Re-inject the attribution block after the closing `---`
   frontmatter line (see the `awk` snippet in the original ECC
   integration plan if mass-refreshing).
4. Run `go test ./internal/office/ -run TestECC_` — the
   `TestECC_SkillBundleOnDisk_HasMinFifteenSkillsWithAttribution`
   test will fail loud if any file lost its attribution.
5. If `THIRD_PARTY_LICENSES.md`'s copyright year drifts, update it
   too.

## Why this subset

- **Skipped** because jarvis already has its own version: most go-*
  patterns, debug, generate-tests, run-tests, design-principles
  (jarvis has its own version), explain, scan-similar-bugs (jarvis
  already has it), web-design-guidelines (jarvis already has it),
  safe-refactor (jarvis already has it), review-changes (jarvis
  already has it), security-audit (jarvis already has it).
- **Skipped** because they pull external services jarvis doesn't
  expose: every Exa / Supabase / Vercel / Railway-flavoured skill.
- **Skipped** because single-IDE-specific: ECC's Cursor / VS Code
  integration helpers.
- **Skipped** because the operator hadn't asked for them this round:
  the GitHub MCP, Playwright MCP, and Memory MCP (R3.6 PR linkage
  already covers the github use case for now).

## Operating notes

### MCP server stderr is not captured by jarvis's `logscrub.All` redaction

The Claude Agent SDK (`claude.WithStderr`) only sees the parent
`claude` subprocess's stderr — that's where jarvis applies the
`logscrub.All` bearer-token + API-key redaction (see
`internal/pty/sdk_session.go`). MCP child processes (context7,
sequential-thinking, codegraph, codegraphcontext, jarvis-mcp) write
their own stderr directly to the host. If an MCP server returns
sensitive data in an error response, that text bypasses jarvis's
redaction.

Mitigations available today:
- Operators concerned about credential leakage should tail MCP child
  processes directly (e.g. via the Claude binary's own debug
  logging) rather than relying on jarvis daemon logs as the single
  redaction chokepoint.
- Don't paste secrets into agent-visible content that could trigger
  an MCP-server error response (e.g. malformed URLs that context7
  echoes back in 4xx responses).

Long-term fix would require an upstream SDK change to expose a
per-MCP stderr hook (e.g. `WithMcpStderr(name, callback)`). Not on
jarvis's roadmap — file an upstream issue if you need it.

### `NpxAvailable()` caches its result on first call

`internal/office/ecc.go:NpxAvailable()` calls `exec.LookPath("npx")`
exactly once per process lifetime (same pattern as the codegraph
binary detection). If you install Node/npx *after* jarvis starts,
ECC's MCP servers (context7, sequential-thinking) will continue to
be skipped until you **restart the daemon**.

Confirm activation by looking for one of these log lines on startup:
- `npx detected; ECC MCP servers (context7, sequential-thinking, shadcn) will be wired`
- `npx not found on PATH; ECC MCP servers will be skipped`

The summary `ecc: 4 agents, 3 commands embedded` appears on every
boot regardless of npx availability — that just reports the embedded
content count, not the MCP wiring state.

## Cross-references

- License: `THIRD_PARTY_LICENSES.md`
- Handler opt-out helper: `internal/web/handler/office.go:eccOptOut`
- Spawn flag definition: `spawnTeamRequest.DisableEcc`
- Embedded content + parser: `internal/office/ecc.go`
- Worktree wiring: `internal/office/worktree.go:SetupWorktreesWithHooks`
- Skill recommender extensions: `internal/office/skills.go`
- Tests: `internal/office/ecc_test.go`,
  `internal/web/handler/ecc_test.go`,
  mobile `src/store/__tests__/spawnDraftStore.p10.test.ts`
