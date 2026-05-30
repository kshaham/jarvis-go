// Package secpath collects path-resolution helpers that enforce
// security boundaries before the resolved path is fed to os.Open /
// os.ReadFile / similar filesystem APIs.
//
// Lives in its own package (not in internal/strs) because the
// helpers depend on path/filepath and produce errors with
// HTTP-mappable sentinels — heavier than the trivial string utilities
// in strs. Imported by handlers + any future surface that resolves
// agent-supplied or operator-supplied paths.
package secpath

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrPathAbsolute signals that the caller supplied an absolute path
// where a workdir-relative path was required. Handlers map to HTTP
// 400. Reject early so an operator can't smuggle "/etc/passwd"
// through an endpoint that intends to scope reads inside a session's
// working directory.
var ErrPathAbsolute = errors.New("absolute paths not allowed")

// ErrPathTraversal signals that the resolved absolute path escaped
// the supplied root directory. Handlers map to HTTP 403 because the
// input was syntactically valid but semantically out of bounds.
//
// Defense in depth: catches both "../etc" style escapes and any
// symlink-based jumps that filepath.Abs canonicalised across the
// root boundary.
var ErrPathTraversal = errors.New("path traversal not allowed")

// ErrInvalidPath signals that filepath.Abs rejected the input
// (extremely rare in practice — usually a malformed string with
// embedded NULs or similar). Handlers map to HTTP 400.
var ErrInvalidPath = errors.New("invalid path")

// ErrRootUnresolvable signals that filepath.Abs failed on the root
// directory itself. Handlers map to HTTP 500 because the failure is
// server-side state (e.g., cwd vanished), not a bad user request.
var ErrRootUnresolvable = errors.New("failed to resolve root directory")

// ResolveUnderRoot resolves rel relative to root and verifies the
// result stays strictly inside root.
//
// Behavior:
//   - rel MUST be a relative path. Absolute rel returns ErrPathAbsolute.
//   - root is resolved via filepath.Abs. Failure returns
//     ErrRootUnresolvable so callers can map to 500.
//   - The joined path is then resolved via filepath.Abs. Failure
//     returns ErrInvalidPath.
//   - The resolved absolute path must lie inside (or equal to) the
//     resolved root. Otherwise returns ErrPathTraversal.
//   - Defense-in-depth: filepath.Rel is also checked for a ".."
//     prefix — guards against the rare case where the prefix check
//     passes but the relative form still encodes traversal (typical
//     on Windows volume-relative paths).
//
// Returns the resolved absolute path on success.
func ResolveUnderRoot(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", ErrPathAbsolute
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", ErrRootUnresolvable
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, rel))
	if err != nil {
		return "", ErrInvalidPath
	}
	if !pathInsideRoot(absPath, absRoot) {
		return "", ErrPathTraversal
	}
	// Belt-and-braces: filepath.Rel catches a ".." that survived the
	// prefix check (rare on POSIX, more common on Windows).
	relPath, err := filepath.Rel(absRoot, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", ErrPathTraversal
	}
	// Symlink hardening: the checks above only canonicalize the STRING. A
	// symlink planted inside root (root/link -> /etc/passwd or -> ../../secret),
	// which agents can routinely create in their workspace, resolves lexically
	// inside root but points outside. Dereference symlinks in BOTH the root and
	// the target (resolving the deepest existing ancestor so a not-yet-existing
	// file still gets its existing ancestor symlinks followed) and re-verify
	// containment. Resolving both the same way keeps them consistent even when
	// the root is itself a symlink (e.g. macOS /var -> /private/var).
	realRoot := evalSymlinksBestEffort(absRoot)
	realPath := evalSymlinksBestEffort(absPath)
	if !pathInsideRoot(realPath, realRoot) {
		return "", ErrPathTraversal
	}
	return absPath, nil
}

// evalSymlinksBestEffort returns p with symlinks dereferenced. If p doesn't
// exist, it resolves the longest existing prefix of p via filepath.EvalSymlinks
// and re-attaches the remaining (non-existent) suffix lexically — so a path for
// a file about to be created still has its existing ancestor symlinks followed.
func evalSymlinksBestEffort(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p // reached the filesystem root; nothing more to resolve
	}
	return filepath.Join(evalSymlinksBestEffort(parent), filepath.Base(p))
}

// pathInsideRoot reports whether absPath is the root itself or a
// descendant of it. Uses filepath.Separator to honour Windows
// backslashes when the binary runs there.
func pathInsideRoot(absPath, absRoot string) bool {
	if absPath == absRoot {
		return true
	}
	return strings.HasPrefix(absPath, absRoot+string(filepath.Separator))
}
