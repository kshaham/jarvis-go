---
name: rtk
description: Understand how rtk compresses your shell command output before it reaches your context, and when to bypass it.
---

# rtk — bash output compression

Every bash command you run goes through an **rtk PreToolUse hook** that rewrites the invocation to filter output by 60–90% before it lands in your context window. You type `git status`; the hook runs `rtk git status` under the covers; you see a compact status summary instead of 30 lines of untracked-files noise. Exit codes, stderr, and failure detail are preserved.

This happens transparently — no flag, no wrapper, no extra thinking needed. Just keep writing normal commands.

## Why this matters to you

- **Your context lasts way longer.** On a 30-minute session where you'd normally have burned ~120k tokens on bash output, you're now at ~24k. That's another ~96k you can spend on actual reasoning and code.
- **Failures still give you the raw output.** rtk's "tee mode" detects non-zero exit codes and returns the full unfiltered stream, so stack traces, test failures, and compilation errors aren't suppressed.
- **Standard `cm` commands are not touched.** `cm task`, `cm skill`, `cm memory`, `cm dev`, `cm lock` — none of these are wrapped. You still get full output from Jarvis tooling.

## Commands rtk filters (partial list, 100+)

| Category | Commands |
|---|---|
| Files | `ls`, `tree`, `cat`, `read`, `grep`, `rg`, `find`, `diff` |
| Git | `status`, `log`, `diff`, `add`, `commit`, `push`, `pull` |
| Build/test | `go test`, `cargo test`, `jest`, `pytest`, `npm test`, `playwright` |
| Lint | `eslint`, `ruff check`, `cargo clippy`, `golangci-lint`, `rubocop` |
| Package managers | `pnpm`, `pip`, `bundle`, `prisma` |
| Cloud/containers | AWS CLI, `docker`, `kubectl` |
| GitHub CLI | `gh pr list`, `gh issue list`, `gh run list` |

Your own scripts and any command rtk doesn't recognize **pass through unchanged** with full raw output.

## When to bypass rtk

Sometimes a filter hides a line you need. Three options:

```bash
# 1. Explicit opt-out for one invocation:
rtk --raw git status

# 2. Call the underlying binary directly:
/usr/bin/git status

# 3. Pipe to grep/tail/head for your own filter:
git log | grep -A2 "your specific string"
```

Prefer option 1 — it's the documented bypass. Option 2 skips rtk entirely but loses its failure-mode protections.

## When you might want `rtk --raw`

- Investigating an intermittent failure where the compressed summary says "no errors" but you have reason to distrust it.
- Copying the exact `git diff` patch for a commit message.
- Debugging a test whose output contains a specific line (numeric ID, memory address, timing) that rtk groups away.
- Reviewing raw `docker ps` fields for a container you're about to kill.

For everything else: just let rtk do its job.

## What rtk does NOT do

- Does NOT call the network. Purely local filtering.
- Does NOT log or exfiltrate your commands. No telemetry unless explicitly enabled in `~/.config/rtk/config.toml`.
- Does NOT touch `cm` / `jarvis-*` / `browser-screenshot` / `expect-cli`. Only the 100+ explicitly-supported commands get filtered.
- Does NOT silently swallow errors. Non-zero exit → raw output in tee mode.

## Troubleshooting

**I ran `git status` and the output looks normal (not compressed).** The hook may not be installed in this session's `~/.claude/settings.json`. Check with `cat ~/.claude/settings.json | grep -i PreToolUse`. If absent, ask the operator to run `rtk init --global` inside the jarvis container.

**A command I depend on is being over-compressed.** Use `rtk --raw <cmd>` for that specific run, and file a bug at https://github.com/rtk-ai/rtk/issues with a reproducer.

**rtk command not found.** The binary is expected at `/usr/local/bin/rtk` inside the jarvis container. If it's missing, bash still runs normally with raw output — nothing breaks.
