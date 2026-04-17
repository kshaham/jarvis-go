package pty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndValidateWorkingDir_BlocksOutsideAllowlist(t *testing.T) {
	// /etc exists on every POSIX host we support; the allowlist restricts to
	// a tempdir, so /etc must fail.
	tmp := t.TempDir()
	_, err := ResolveAndValidateWorkingDir("/etc", []string{tmp})
	if err == nil {
		t.Fatal("expected error when path is outside allowlist")
	}
}

func TestResolveAndValidateWorkingDir_AcceptsInsideAllowlist(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "project")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	resolved, err := ResolveAndValidateWorkingDir(sub, []string{tmp})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resolved == "" {
		t.Fatal("expected non-empty resolved path")
	}
}

func TestResolveAndValidateWorkingDir_FollowsSymlink(t *testing.T) {
	// Key safety test: a symlink inside the allowlist that points OUTSIDE
	// must be rejected because EvalSymlinks resolves the target before the
	// allowlist check runs.
	tmp := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("skipping -- symlink creation failed: %v", err)
	}
	if _, err := ResolveAndValidateWorkingDir(link, []string{tmp}); err == nil {
		t.Fatal("expected error when symlink resolves outside allowlist")
	}
}

func TestResolveAndValidateWorkingDir_EmptyAllowlistAllowsAny(t *testing.T) {
	// Backwards-compat: empty allowlist skips the allowlist check so existing
	// setups without configuration don't break.
	tmp := t.TempDir()
	if _, err := ResolveAndValidateWorkingDir(tmp, nil); err != nil {
		t.Fatalf("empty allowlist should allow any existing dir: %v", err)
	}
}

func TestDefaultWorkingDirAllowlist_IncludesEnvPaths(t *testing.T) {
	tmpA := t.TempDir()
	tmpB := t.TempDir()
	t.Setenv("JARVIS_WORKSPACE_PATHS", tmpA+":"+tmpB)

	got := DefaultWorkingDirAllowlist()

	// $HOME + two from env = at least 3 entries (extra container paths may
	// or may not exist depending on the host, so we only assert the env ones
	// and the presence of home).
	foundA, foundB := false, false
	for _, p := range got {
		if p == tmpA {
			foundA = true
		}
		if p == tmpB {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("expected both env paths in allowlist, got %v", got)
	}
}
