package handler

// P8.1 · Per-member workingDir override allowlist test.
//
// The full OfficeSpawnTeam handler needs heavy fixtures (template
// store, agent token manager, provider env, etc.) that aren't worth
// standing up just to validate the rejection branch. Instead we
// exercise the public seam the spawn handler now calls (added in
// P8.1) — ctx.ResolveWorkingDir — and confirm:
//
//   1. A path inside the configured allowlist resolves cleanly.
//   2. A path outside the allowlist returns a non-nil error so the
//      spawn handler can short-circuit with HTTP 400.
//
// Together these pin the contract the per-member override now relies
// on: same allowlist, same error shape as the team-level check.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
)

func TestResolveWorkingDir_AcceptsAllowlisted(t *testing.T) {
	ctx := newSnapshotCtx(t)
	allowed := t.TempDir()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{allowed},
	})
	ctx.StateStore = store

	got, err := ctx.ResolveWorkingDir(allowed)
	if err != nil {
		t.Fatalf("ResolveWorkingDir(%q) under allowlist=%v: %v", allowed, allowed, err)
	}
	if got == "" {
		t.Errorf("expected non-empty resolved path, got empty")
	}
}

func TestResolveWorkingDir_RejectsOutsideAllowlist(t *testing.T) {
	ctx := newSnapshotCtx(t)
	allowed := t.TempDir()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{allowed},
	})
	ctx.StateStore = store

	// `/etc` is a real directory but never in any sane allowlist.
	// This is the canonical attack the P8.1 fix blocks: an operator
	// supplying `"workingDir":"/etc"` on a per-member override.
	if _, err := ctx.ResolveWorkingDir("/etc"); err == nil {
		t.Fatal("expected ResolveWorkingDir to reject /etc when not in allowlist")
	}
}

func TestResolveWorkingDir_RejectsRelativePath(t *testing.T) {
	ctx := newSnapshotCtx(t)
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{t.TempDir()},
	})
	ctx.StateStore = store

	if _, err := ctx.ResolveWorkingDir("relative/path"); err == nil {
		t.Fatal("expected rejection for relative path")
	}
}

// Belt-and-braces: assert the rejection error message contains
// "allowed" or similar so the spawn handler's wrapping
// ("invalid workingDir for member X: ...") doesn't accidentally drop
// the operator-facing reason. P8.1 design rule.
func TestResolveWorkingDir_ErrorMentionsAllowlistOrPath(t *testing.T) {
	ctx := newSnapshotCtx(t)
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{t.TempDir()},
	})
	ctx.StateStore = store

	_, err := ctx.ResolveWorkingDir("/etc")
	if err == nil {
		t.Fatal("expected error for /etc")
	}
	msg := err.Error()
	if !strings.Contains(msg, "allow") && !strings.Contains(msg, "/etc") {
		t.Errorf("error message should mention allowlist or rejected path; got: %q", msg)
	}
}
