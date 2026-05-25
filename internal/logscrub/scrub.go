// Package logscrub redacts well-known credential patterns from text
// before it's emitted to logs, error messages, or HTTP responses.
//
// Why a dedicated package: jarvis shells out to `claude` (oneshot,
// ultrareview, spawn-remote, claude session ops) and the CLI's stderr
// often contains `Authorization: Bearer <token>` lines from Anthropic's
// debug output on auth failures. That stderr gets captured into error
// wrappers and forwarded into slog. If the daemon's logs are ever
// shipped to a central log store (Loki, Datadog, etc.) the OAuth
// token leaks across the trust boundary. Centralising the regex here
// makes it easy to extend with new patterns (API keys, session
// cookies) without a repo-wide grep.
//
// Patterns are intentionally conservative — false positives are
// preferable to false negatives for a credential scrubber.
package logscrub

import "regexp"

// bearerTokenRe matches RFC 6750 Bearer tokens. The {10,} lower bound
// avoids matching the literal word "Bearer" followed by a short
// placeholder; real tokens are at least 16 characters in every API
// we've seen.
var bearerTokenRe = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]{10,}`)

// anthropicAPIKeyRe matches Anthropic API keys (sk-ant-*). These can
// appear in stderr when the CLI prints config diagnostics.
var anthropicAPIKeyRe = regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)

// agentTokenRe matches the JARVIS_AGENT_TOKEN environment-variable
// form (`JARVIS_AGENT_TOKEN=<value>`). The CLI subprocess inherits
// this token; if its stderr ever echoes the env (e.g., `claude
// --print-env`, crash dumps, or accidental env-dump in scripted
// invocations), the raw token would otherwise land in jarvis's slog.
var agentTokenRe = regexp.MustCompile(`JARVIS_AGENT_TOKEN=[A-Za-z0-9_\-\.]+`)

// genericEnvSecretRe is a defense-in-depth catch-all for env-var
// assignments whose name ends in a sensitive suffix (underscore-
// separated to avoid false positives like `MONKEY=`). Matches names
// like AWS_SECRET_ACCESS_KEY, DB_PASSWORD, GITHUB_TOKEN, OPENAI_API_KEY,
// MY_CUSTOM_CREDENTIAL. Value class is greedy on common base64 /
// hex / dot / dash characters; stops at whitespace.
var genericEnvSecretRe = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*_(?:SECRET|TOKEN|PASSWORD|KEY|CREDENTIAL))=[A-Za-z0-9_\-\./+=]+`)

// Bearer redacts Authorization-header style Bearer tokens. Safe to
// apply to arbitrary user-supplied text — non-Bearer content passes
// through unchanged.
func Bearer(s string) string {
	return bearerTokenRe.ReplaceAllString(s, "Bearer [REDACTED]")
}

// All applies every known credential pattern. Use this at the
// outermost log/error boundary; cheaper than calling individual
// scrubbers at each site. Order matters: the generic env-secret
// regex runs first so it claims the full `KEY=VALUE` line before
// the narrower value-shape regexes (sk-ant-, Bearer) get a chance
// to leave behind a partial leak.
func All(s string) string {
	s = genericEnvSecretRe.ReplaceAllString(s, "$1=[REDACTED]")
	s = agentTokenRe.ReplaceAllString(s, "JARVIS_AGENT_TOKEN=[REDACTED]")
	s = Bearer(s)
	s = anthropicAPIKeyRe.ReplaceAllString(s, "sk-ant-[REDACTED]")
	return s
}
