# 🔐 Security Policy

Thanks for taking the time to look at the security of `jarvis-go`. This
document covers how to report vulnerabilities, what's in scope, and what
you can expect from us.

## 📌 Supported versions

`jarvis-go` is pre-1.0 and ships from `main`. Only the **latest commit
on `main`** is supported for security fixes. If you've forked an older
revision, rebase before reporting.

| Version | Supported |
|---|---|
| `main` (HEAD) | ✅ |
| Any other branch / tag | ❌ |

## 🐛 Reporting a vulnerability

**Do not file public GitHub issues for security bugs.** Use one of the
private channels below:

1. **GitHub private vulnerability report** (preferred) — go to the
   repo's `Security` tab and click `Report a vulnerability`.
2. **Email** — send to the address listed on the repo owner's GitHub
   profile. PGP key on request.

Please include:

- A description of the issue and the impact you've assessed
- Steps to reproduce (smallest possible repro)
- The commit SHA you tested against
- Any proof-of-concept code or screenshots (avoid sharing live tokens —
  redact)
- Your preferred credit name for the advisory (or "anonymous")

## ⏱️ What to expect

| Stage | Target |
|---|---|
| First acknowledgement | within 72 hours |
| Triage + severity assessment | within 7 days |
| Fix in `main` for high/critical | within 30 days |
| Public advisory + credit | after a patched release is available |

These are targets, not guarantees — this is a side project, so timelines
flex. We'll keep you in the loop.

## 🎯 Scope

### In scope

- Remote code execution, authentication bypass, privilege escalation in
  the `jarvis` web server
- Path traversal, SSRF, or sandbox escape in the MCP tool surface or
  agent runtime
- CSRF, XSS, or session-fixation in the office dashboard
- Secret exposure (logs, error pages, SSE payloads, agent prompts)
- Bugs that let one agent session read or mutate another agent's state
  beyond the documented sharing model
- Supply-chain issues in `go.mod` / `package.json` dependencies

### Out of scope

- Anything that requires the operator to deliberately misconfigure the
  daemon (e.g. running it on a public IP without `--https` + auth)
- Findings against `claude` itself — report those to Anthropic
- Vulnerabilities in the bundled `skills/` content (these are prompts,
  not executable code)
- DoS by an authenticated operator on their own server
- Issues only reproducible on a fork that's diverged from `main`

## 🤝 Safe harbor

We will not pursue legal action against good-faith researchers who:

- Make a reasonable effort to avoid privacy violations, destruction of
  data, or service degradation
- Give us reasonable time to fix before public disclosure
- Don't exfiltrate data beyond what's needed to demonstrate the issue
- Don't use the issue against systems you don't own or have explicit
  permission to test

If you're acting in good faith, you're welcome here. Thanks for helping
keep `jarvis-go` safe.
