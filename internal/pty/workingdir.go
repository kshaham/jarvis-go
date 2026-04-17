package pty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrWorkingDirOutsideAllowlist is returned when the resolved working
// directory (after symlink evaluation) is not within any allowlist entry.
var ErrWorkingDirOutsideAllowlist = errors.New("working directory is outside the configured allowlist")

// ResolveAndValidateWorkingDir canonicalizes the user-supplied path (absolute
// + symlinks evaluated) and verifies the result lives inside one of the
// allowlisted prefixes. Also confirms the path exists and is a directory.
//
// The symlink evaluation step is the critical safety check: without it, a
// user who can create a symlink /tmp/x -> /etc can pass /tmp/x as their
// working directory and start a session at the root filesystem.
//
// If allowlist is empty the check is skipped and only existence+directory
// are verified (single-user localhost default behavior).
func ResolveAndValidateWorkingDir(raw string, allowlist []string) (string, error) {
	if raw == "" {
		return "", errors.New("working directory is empty")
	}
	// Expand $HOME prefix manually so `~` isn't required in the raw input.
	if strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, raw[2:])
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q: %w", raw, err)
	}

	// Resolve symlinks to defeat `/tmp/link-to-etc` style bypasses. If the
	// path doesn't exist EvalSymlinks fails, which we treat as an error
	// because a missing working directory is not a valid session state.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q: %w", raw, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("cannot stat %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}

	if len(allowlist) == 0 {
		return resolved, nil
	}
	for _, prefix := range allowlist {
		if prefix == "" {
			continue
		}
		absPrefix, err := filepath.Abs(prefix)
		if err != nil {
			continue
		}
		// EvalSymlinks on the prefix so /var -> /private/var on macOS matches
		// consistently with the resolved path.
		if p, err := filepath.EvalSymlinks(absPrefix); err == nil {
			absPrefix = p
		}
		rel, err := filepath.Rel(absPrefix, resolved)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return resolved, nil
		}
	}
	return "", ErrWorkingDirOutsideAllowlist
}

// DefaultWorkingDirAllowlist returns sensible defaults for the allowlist
// when none has been configured: the user's home directory and common
// project parents. These are permissive enough not to break existing setups.
func DefaultWorkingDirAllowlist() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	return []string{home}
}
