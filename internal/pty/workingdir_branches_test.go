package pty

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// withResolveSeams temporarily overrides the ResolveAndValidateWorkingDir
// filesystem seams and restores them on cleanup. Any nil argument leaves that
// seam at its production value.
func withResolveSeams(
	t *testing.T,
	abs func(string) (string, error),
	eval func(string) (string, error),
	stat func(string) (os.FileInfo, error),
	rel func(string, string) (string, error),
) {
	t.Helper()
	oAbs, oEval, oStat, oRel := resolveAbs, resolveEvalSymlink, resolveStat, resolveRel
	if abs != nil {
		resolveAbs = abs
	}
	if eval != nil {
		resolveEvalSymlink = eval
	}
	if stat != nil {
		resolveStat = stat
	}
	if rel != nil {
		resolveRel = rel
	}
	t.Cleanup(func() {
		resolveAbs, resolveEvalSymlink, resolveStat, resolveRel = oAbs, oEval, oStat, oRel
	})
}

// TestResolveAndValidateWorkingDir_AbsError forces the filepath.Abs seam to fail
// (workingdir.go:43-45). filepath.Abs only errors when os.Getwd() fails, which
// macOS won't reproduce in-process once the wd is cached -- so we inject the
// failure through the seam and assert the wrapped "cannot resolve" error.
func TestResolveAndValidateWorkingDir_AbsError(t *testing.T) {
	sentinel := errors.New("abs boom")
	withResolveSeams(t, func(string) (string, error) { return "", sentinel }, nil, nil, nil)

	_, err := ResolveAndValidateWorkingDir("relative/path", nil)
	if err == nil {
		t.Fatal("expected error when filepath.Abs fails")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap the Abs sentinel", err)
	}
}

// TestResolveAndValidateWorkingDir_EvalSymlinksError forces the EvalSymlinks seam
// to fail after a successful Abs (workingdir.go:51-53). The non-allowlist branch
// at 56-58 (Stat) is exercised separately.
func TestResolveAndValidateWorkingDir_EvalSymlinksError(t *testing.T) {
	sentinel := errors.New("eval boom")
	withResolveSeams(t, nil, func(string) (string, error) { return "", sentinel }, nil, nil)

	_, err := ResolveAndValidateWorkingDir("/tmp", nil)
	if err == nil {
		t.Fatal("expected error when EvalSymlinks fails")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap the EvalSymlinks sentinel", err)
	}
}

// TestResolveAndValidateWorkingDir_StatError forces the os.Stat seam to fail
// (workingdir.go:56-58). In production this requires a TOCTOU race where the dir
// vanishes between EvalSymlinks and Stat; the seam makes it deterministic.
func TestResolveAndValidateWorkingDir_StatError(t *testing.T) {
	sentinel := errors.New("stat boom")
	withResolveSeams(t, nil,
		func(p string) (string, error) { return p, nil }, // EvalSymlinks succeeds
		func(string) (os.FileInfo, error) { return nil, sentinel },
		nil)

	_, err := ResolveAndValidateWorkingDir("/tmp", nil)
	if err == nil {
		t.Fatal("expected error when os.Stat fails")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap the Stat sentinel", err)
	}
}

// TestResolveAndValidateWorkingDir_NotADir covers the !IsDir() branch
// (workingdir.go:59-61) deterministically via a real regular file.
func TestResolveAndValidateWorkingDir_NotADir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ResolveAndValidateWorkingDir(file, nil)
	if err == nil {
		t.Fatal("expected error when target is a regular file, not a dir")
	}
}

// TestResolveAndValidateWorkingDir_AbsPrefixError forces filepath.Abs to fail
// only for a relative allowlist prefix (workingdir.go:71-72), so that prefix is
// skipped. The absolute target resolves fine but falls outside the (now-empty)
// effective allowlist, yielding ErrWorkingDirOutsideAllowlist.
func TestResolveAndValidateWorkingDir_AbsPrefixError(t *testing.T) {
	target := t.TempDir()
	prefixSentinel := errors.New("prefix abs boom")

	// Abs succeeds for the absolute target but fails for the relative prefix.
	withResolveSeams(t, func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return filepath.Clean(p), nil
		}
		return "", prefixSentinel
	}, nil, nil, nil)

	_, err := ResolveAndValidateWorkingDir(target, []string{"relative-prefix"})
	if !errors.Is(err, ErrWorkingDirOutsideAllowlist) {
		t.Errorf("error = %v, want ErrWorkingDirOutsideAllowlist (the only prefix was un-absolutizable)", err)
	}
}

// TestResolveAndValidateWorkingDir_RelError forces filepath.Rel to fail for the
// allowlist prefix (workingdir.go:80-81), so the prefix is skipped and the call
// rejects with ErrWorkingDirOutsideAllowlist. filepath.Rel can't be made to
// error between two absolute unix paths, so we inject through the seam.
func TestResolveAndValidateWorkingDir_RelError(t *testing.T) {
	target := t.TempDir()
	relSentinel := errors.New("rel boom")
	withResolveSeams(t, nil, nil, nil,
		func(string, string) (string, error) { return "", relSentinel })

	// Use the target's own parent as the prefix so that, absent the Rel error,
	// it WOULD be allowed -- proving the rejection comes from the forced Rel
	// failure and not from a genuine outside-allowlist result.
	parent := filepath.Dir(target)
	_, err := ResolveAndValidateWorkingDir(target, []string{parent})
	if !errors.Is(err, ErrWorkingDirOutsideAllowlist) {
		t.Errorf("error = %v, want ErrWorkingDirOutsideAllowlist when Rel errors for every prefix", err)
	}
}

// TestResolveAndValidateWorkingDir_EmptyPrefixSkipped covers the `prefix == ""`
// continue arm (workingdir.go:67-69): an empty allowlist entry is skipped, and a
// valid one alongside it still admits the target.
func TestResolveAndValidateWorkingDir_EmptyPrefixSkipped(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	resolved, err := ResolveAndValidateWorkingDir(sub, []string{"", tmp})
	if err != nil {
		t.Fatalf("empty prefix should be skipped, valid prefix should admit: %v", err)
	}
	if resolved == "" {
		t.Fatal("expected a non-empty resolved path")
	}
}
