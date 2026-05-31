package office

import (
	"os/exec"
	"testing"
)

// TestPgrepChildren_NonOneExitError covers the "pgrep failed with a
// non-1 exit code" branch: anything other than exit 1 is a real error.
func TestPgrepChildren_NonOneExitError(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	// `sh -c "exit 2"` produces a non-1 exit code, which is treated as a
	// real pgrep failure (not "no matches").
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 2")
	}
	if _, err := pgrepChildren(1234); err == nil {
		t.Fatal("expected error for non-1 pgrep exit code")
	}
}

// TestCollectDescendants_WalksTree drives the BFS walk: the stub returns one
// child for the root and no children for the child, so collectDescendants
// returns exactly that child (and the visited-set prevents re-walking).
func TestCollectDescendants_WalksTree(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	// The last arg to `pgrep -P <pid>` is the parent PID string. Return a
	// child only for the root (5000); everything else returns no matches
	// (exit 1 via `false`-like behavior emulated by empty stdout + exit 1).
	execCommand = func(name string, args ...string) *exec.Cmd {
		parent := ""
		if len(args) > 0 {
			parent = args[len(args)-1]
		}
		if parent == "5000" {
			return exec.Command("printf", "%s", "6001\n")
		}
		// No children: emit nothing and exit 1 (pgrep "no matches").
		return exec.Command("sh", "-c", "exit 1")
	}
	got := collectDescendants(5000)
	if len(got) != 1 || got[0] != 6001 {
		t.Fatalf("expected [6001], got %v", got)
	}
}

// TestCollectDescendants_PgrepErrorStops drives the err-return branch: when
// pgrep fails (non-1 exit), collectDescendants returns what it has so far.
func TestCollectDescendants_PgrepErrorStops(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 2") // real failure
	}
	got := collectDescendants(7000)
	if len(got) != 0 {
		t.Errorf("expected empty descendants when pgrep errors, got %v", got)
	}
}
