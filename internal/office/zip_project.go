package office

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Uploaded-ZIP-project flow. Sibling to new_project.go and
// github_project.go: where PrepareNewProject scaffolds an empty
// /app/workspace/<slug> and PrepareGitHubProject clones a remote repo,
// PrepareZipProject extracts an operator-supplied .zip codebase into
// /app/workspace/<slug> so a team can start from code they already have
// on disk. The rest of the spawn flow then treats it as a normal
// new project (PrepareNewProject is idempotent and the CLAUDE.md seed
// never clobbers an existing file).
//
// Security: the slug is validated with the same ProjectNameRe the
// new-project flow uses, and extraction is hardened against the classic
// archive attacks -- zip-slip (path traversal via "../" or absolute
// entry names), symlink escape, and zip-bombs (unbounded total size or
// entry count). Nothing is ever written outside the target directory.

// ErrProjectExists is returned by PrepareZipProject when the target
// project directory already exists and is non-empty. The handler maps
// it to 409 so the operator picks a fresh project name rather than
// silently merging an upload into existing files.
var ErrProjectExists = errors.New("project already exists -- choose a different name")

// ErrZipTooLarge is returned when an archive's uncompressed contents
// exceed maxExtractedBytes or maxExtractedFiles (zip-bomb guard).
var ErrZipTooLarge = errors.New("archive is too large to extract (size or file-count limit exceeded)")

// Extraction limits. These bound the uncompressed footprint so a small
// malicious zip can't fill the disk. Generous enough for any real
// source tree (the 500 MB upload cap already bounds the compressed
// side; this bounds the decompressed side). Package vars (not consts)
// only so tests can shrink them to exercise the zip-bomb guards;
// production never reassigns them.
var (
	maxExtractedBytes = int64(2) << 30 // 2 GiB total uncompressed
	maxExtractedFiles = 50_000         // entry count
)

// PrepareZipProject validates a new-project slug, extracts zipPath into
// NewProjectRoot/<slug> (hardened against zip-slip / symlink / zip-bomb),
// strips a single shared top-level directory if the archive has one
// (GitHub "Download ZIP" wraps everything in "<repo>-<branch>/"), runs a
// best-effort git init + initial commit so per-agent worktrees can branch
// from a baseline, and broadcasts office:projectCreated.
//
// Returns the resolved target directory path on success -- the caller
// spawns the team as a NewProject with this slug. Pass broadcast=nil and
// run=nil from tests / contexts without an SSE broker or real git.
func PrepareZipProject(projectName, zipPath string, run GitRunner, broadcast SSEBroadcaster) (string, error) {
	if !ProjectNameRe.MatchString(projectName) {
		return "", ErrInvalidProjectName
	}
	target := filepath.Join(NewProjectRoot, projectName)

	// Refuse to extract into a populated directory: the upload flow is
	// "new project", and merging into an existing tree would be
	// surprising (and could shadow files the operator forgot about).
	if entries, statErr := os.ReadDir(target); statErr == nil && len(entries) > 0 {
		return "", ErrProjectExists
	}
	if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
		return "", fmt.Errorf("failed to create project directory: %w", mkErr)
	}

	if extractErr := extractZip(zipPath, target); extractErr != nil {
		// Clean up a half-extracted tree so a retry with the same name
		// isn't blocked by ErrProjectExists.
		os.RemoveAll(target)
		return "", extractErr
	}

	stripSingleRoot(target)
	gitInitProject(target, run)

	if broadcast != nil {
		broadcast("office:projectCreated", map[string]interface{}{
			"name": projectName,
			"path": target,
		})
		slog.Info("zip project created", "name", projectName, "path", target)
	}
	return target, nil
}

// extractZip unpacks zipPath into target with full path-traversal,
// symlink, and zip-bomb protection. Directories are created; regular
// files are written with the archive's permission bits; symlinks and
// other irregular entries are skipped (never followed).
func extractZip(zipPath, target string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	cleanTarget := filepath.Clean(target)
	var totalBytes int64
	var fileCount int

	for _, f := range zr.File {
		// Resolve the entry path and verify it stays within target.
		dest, ok := safeJoin(cleanTarget, f.Name)
		if !ok {
			return fmt.Errorf("unsafe entry path in archive: %q", f.Name)
		}

		info := f.FileInfo()
		switch {
		case info.IsDir():
			if mkErr := os.MkdirAll(dest, 0o755); mkErr != nil {
				return fmt.Errorf("mkdir %q: %w", dest, mkErr)
			}
			continue
		case info.Mode()&os.ModeSymlink != 0:
			// Never materialize symlinks from an untrusted archive: a
			// link could point outside target and later be written
			// through. Skip silently.
			continue
		case !info.Mode().IsRegular():
			// Devices, fifos, etc. -- skip.
			continue
		}

		fileCount++
		if fileCount > maxExtractedFiles {
			return ErrZipTooLarge
		}
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(dest), mkErr)
		}
		written, copyErr := copyZipEntry(f, dest, maxExtractedBytes-totalBytes)
		if copyErr != nil {
			return copyErr
		}
		totalBytes += written
	}
	return nil
}

// copyZipEntry streams one archive entry to dest, refusing to write more
// than budget bytes (the remaining global uncompressed allowance). It
// returns the number of bytes written.
func copyZipEntry(f *zip.File, dest string, budget int64) (int64, error) {
	if budget <= 0 {
		return 0, ErrZipTooLarge
	}
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("open entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", dest, err)
	}
	defer out.Close()

	// LimitReader caps a single entry at budget+1 so we can detect an
	// overrun: if the entry alone would blow the remaining budget the
	// copy reads one byte past it and we bail.
	written, err := io.Copy(out, io.LimitReader(rc, budget+1))
	if err != nil {
		return written, fmt.Errorf("write %q: %w", dest, err)
	}
	if written > budget {
		return written, ErrZipTooLarge
	}
	return written, nil
}

// safeJoin resolves entry name under base and reports whether the result
// stays inside base. It rejects (rather than silently clamps) absolute
// paths and any "../" traversal, so a malformed/malicious archive surfaces
// a clear error instead of having entries quietly relocated.
func safeJoin(base, name string) (string, bool) {
	// Zip entries always use forward slashes; normalize any backslashes a
	// Windows-built archive might carry before cleaning.
	clean := filepath.Clean(strings.ReplaceAll(name, "\\", "/"))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	dest := filepath.Join(base, clean)
	// Defense in depth: confirm the joined path is still contained.
	rel, err := filepath.Rel(base, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return dest, true
}

// stripSingleRoot collapses a lone top-level directory up into target,
// so an archive shaped like "<repo>-main/<files>" (the GitHub download
// layout) lands its code directly at the project root. No-op when the
// archive has multiple top-level entries or a top-level file.
func stripSingleRoot(target string) {
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return
	}
	root := filepath.Join(target, entries[0].Name())
	inner, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range inner {
		from := filepath.Join(root, e.Name())
		to := filepath.Join(target, e.Name())
		if _, exists := os.Stat(to); exists == nil {
			// A name collision with the wrapper dir itself -- bail out
			// of the optimization rather than risk clobbering.
			return
		}
		if mvErr := os.Rename(from, to); mvErr != nil {
			slog.Warn("zip project: strip-root rename failed", "from", from, "err", mvErr)
			return
		}
	}
	os.Remove(root)
}

// gitInitProject best-effort initializes a git repo with one commit when
// the extracted tree has no .git, so SetupWorktreesWithHooks can create
// per-agent worktrees from a baseline. Failures are logged and swallowed
// -- the worktree code already falls back to the shared dir.
func gitInitProject(target string, run GitRunner) {
	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		return // archive already carried a repo
	}
	if run == nil {
		run = DefaultGitRunner
	}
	ctx := context.Background()
	steps := [][]string{
		{"-C", target, "init"},
		{"-C", target, "add", "-A"},
		{"-C", target, "-c", "user.name=Jarvis", "-c", "user.email=jarvis@local",
			"commit", "-m", "Initial commit from uploaded codebase", "--allow-empty"},
	}
	for _, args := range steps {
		if err := run(ctx, "", args...); err != nil {
			slog.Warn("zip project: git init step failed", "args", args[0], "err", err)
			return
		}
	}
}
