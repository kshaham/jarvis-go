package integrations

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewMonitor(t *testing.T) {
	m := NewMonitor()
	if m == nil {
		t.Fatal("nil monitor")
	}
	if got := m.GetStatus("unknown"); got == nil {
		t.Error("GetStatus returned nil for unknown session; should return zero-value")
	}
}

func TestGetStatus_ZeroValueForUnknownSession(t *testing.T) {
	m := NewMonitor()
	st := m.GetStatus("never-registered")
	if st == nil {
		t.Fatal("nil status")
	}
	if st.Git != nil {
		t.Errorf("Git should be nil for unknown session, got %+v", st.Git)
	}
}

// RefreshGitStatus is best-tested against a real git repo because the
// branch + porcelain output formats are stable. Skip if git isn't on
// PATH (e.g., minimal CI image).
func TestRefreshGitStatus_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	// Initialise a tiny repo with one tracked, one staged, one untracked file.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-m", "first")

	// Now make 1 staged, 1 modified, 1 untracked.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "staged.txt")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMonitor()
	st := m.RefreshGitStatus("sess-001", dir)
	if st == nil {
		t.Fatal("RefreshGitStatus returned nil")
	}
	if st.Branch != "main" {
		t.Errorf("Branch=%q want main", st.Branch)
	}
	if len(st.Staged) == 0 {
		t.Errorf("expected staged files, got %+v", st.Staged)
	}
	if len(st.Untracked) == 0 {
		t.Errorf("expected untracked files, got %+v", st.Untracked)
	}
	if st.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}

	// Cached status should match.
	cached := m.GetStatus("sess-001")
	if cached.Git == nil || cached.Git.Branch != "main" {
		t.Errorf("cached status missing or wrong: %+v", cached)
	}
}

func TestRefreshGitStatus_NonGitDirReturnsEmptyButCaches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir() // not a git repo
	m := NewMonitor()
	st := m.RefreshGitStatus("sess-002", dir)
	if st == nil {
		t.Fatal("nil status")
	}
	// Branch should be empty for a non-repo (git rev-parse errors out).
	if st.Branch != "" {
		t.Errorf("expected empty branch, got %q", st.Branch)
	}
	// Cache should still hold the entry.
	if cached := m.GetStatus("sess-002"); cached.Git == nil {
		t.Error("status should be cached even when empty")
	}
}
