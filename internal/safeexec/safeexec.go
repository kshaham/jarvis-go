// Package safeexec resolves user-supplied command strings to absolute
// binary paths under a strict allowlist + system-prefix policy. The
// /api/run HTTP handler is the canonical consumer; CLI tools or
// scheduled tasks that need the same shape can import directly.
//
// Two complementary defences:
//
//  1. AllowedCommands enumerates the basename of every binary that
//     callers may invoke. `env` is deliberately excluded — it would
//     leak ANTHROPIC_API_KEY and other provider credentials.
//  2. AllowedBinaryPrefixes constrains the resolved absolute path
//     to a fixed set of system directories so a malicious
//     PATH=/tmp:/usr/bin shim can't smuggle a binary that happens
//     to share an allowlisted name.
//
// The package also exposes a small regex covering shell
// metacharacters that callers should reject before passing the
// command to Resolve.
package safeexec

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// AllowedCommands is the basename allowlist. Mutate by replacement,
// not by append, so concurrent callers always see a consistent set.
var AllowedCommands = []string{
	"git", "npm", "go", "ls", "cat", "head", "tail",
	"grep", "find", "wc", "sort", "diff", "echo",
	"pwd", "which", "node", "npx", "tsc",
}

// AllowedBinaryPrefixes constrains where resolved binaries may live.
// Adding new prefixes here is a security-policy change — review the
// caller's threat model before doing so.
var AllowedBinaryPrefixes = []string{
	"/usr/bin/",
	"/usr/local/bin/",
	"/opt/homebrew/bin/",
	"/bin/",
	"/sbin/",
	"/usr/sbin/",
	// Common Go toolchain installs
	"/usr/local/go/bin/",
	"/opt/homebrew/opt/go/bin/",
}

// shellMetachars matches characters that, if forwarded to a shell,
// enable command injection. The /api/run handler rejects any command
// string containing one of these before calling Resolve.
//
// NOT a substitute for using exec.Command directly with a slice of
// args (which never touches a shell) — this is a belt-and-braces
// check against accidental misuse on call sites that build shell
// strings.
var shellMetachars = regexp.MustCompile(`[;|&$` + "`" + `><(){}]`)

// ContainsShellMetachars reports whether s contains any character
// that would let a shell re-parse the input. True means reject.
func ContainsShellMetachars(s string) bool {
	return shellMetachars.MatchString(s)
}

// Resolve parses cmd into a binary + argv, verifies the binary's
// basename is in AllowedCommands, looks it up via exec.LookPath, and
// confirms the resolved absolute path lives under one of
// AllowedBinaryPrefixes. Returns (absPath, args, true) on success,
// otherwise ("", nil, false) — single boolean to keep the call site
// terse, since every failure maps to the same HTTP 400 anyway.
//
// Empty cmd returns ok=false (caller decides whether that's a 400 or
// a no-op).
func Resolve(cmd string) (absPath string, args []string, ok bool) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", nil, false
	}
	base := filepath.Base(parts[0])
	if !isAllowedCommand(base) {
		return "", nil, false
	}
	abs, err := exec.LookPath(base)
	if err != nil {
		return "", nil, false
	}
	if !isAllowedPrefix(abs) {
		return "", nil, false
	}
	return abs, parts[1:], true
}

// isAllowedCommand reports whether base is in the AllowedCommands
// allowlist. Linear scan is fine — the slice is < 30 entries.
func isAllowedCommand(base string) bool {
	for _, a := range AllowedCommands {
		if base == a {
			return true
		}
	}
	return false
}

// isAllowedPrefix reports whether abs lives under one of the
// AllowedBinaryPrefixes entries. Prefix match is sufficient here
// because the prefixes all end with "/" so a sibling like
// "/usr/binnotbin/git" can't match.
func isAllowedPrefix(abs string) bool {
	for _, p := range AllowedBinaryPrefixes {
		if strings.HasPrefix(abs, p) {
			return true
		}
	}
	return false
}
