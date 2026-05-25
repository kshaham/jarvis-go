// Package strs collects tiny zero-dep string helpers shared across
// internal packages. Lives here (not in stdlib's strings or in a
// random office helper file) so multiple consumers can import without
// creating a cycle. P11.3 extracted firstNonEmpty after the audit
// flagged it duplicated between internal/office/ecc.go and
// internal/web/handler/office.go.
package strs

import (
	"regexp"
	"strings"
)

// SessionIDPattern is the canonical jarvis sessionId shape — 8-128
// chars of [A-Za-z0-9_-]. Used by:
//   - the claude-session ops handler (resolveClaudeSessionID)
//   - the claudehooks dispatcher (HandleEvent)
//   - anywhere else that accepts a sessionId from an untrusted source.
//
// Anchored on both ends so `../`, NUL bytes, shell metacharacters,
// and overly-short placeholder values all fail validation cleanly.
var SessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// IsValidSessionID returns true when s matches SessionIDPattern.
// Empty input returns false.
func IsValidSessionID(s string) bool {
	return SessionIDPattern.MatchString(s)
}

// IsSafeRelativePath returns true when p is safe to use as a
// relative file path: no parent-dir traversal (".."), no
// absolute-path prefix, no NUL bytes. Empty paths return true —
// callers separately decide whether an empty value is acceptable
// for their use case.
//
// Used by jarvis HTTP handlers that accept arbitrary file paths
// from agents (file locks, screenshot uploads, etc.) to reject
// inputs that would let an attacker escape the agent's worktree.
// NOT a substitute for path-allowlist enforcement at the next
// layer down — this is the input-validation step, not the
// authorisation step.
func IsSafeRelativePath(p string) bool {
	if strings.ContainsRune(p, 0) {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return false
	}
	return true
}

// FirstNonEmpty returns the first argument whose TrimSpace value is
// non-empty. Returns "" when every input is empty / whitespace.
// Useful for layered defaults (try arg, fall back to env, fall back
// to literal) without writing the same `if a != "" { return a }`
// ladder at every call site.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
