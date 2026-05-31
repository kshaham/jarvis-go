package office

import (
	"os"
	"path/filepath"
	"testing"
)

// installFakeGit prepends a temp dir holding a fake `git` executable to
// PATH for the duration of the test. The script body decides the exit
// code per subcommand. runGit resolves "git" through PATH at exec time,
// so this lets tests force the git-subprocess failure branches in
// worktree.go (worktree add/remove, branch -D, rev-parse) without
// needing a real broken repo.
func installFakeGit(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	gitPath := filepath.Join(dir, "git")
	if err := os.WriteFile(gitPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeGitFailWorktreeAdd makes `git worktree add` fail (exit 1) while
// rev-parse and all other subcommands succeed (exit 0). This drives the
// "Failed to create worktree -- using shared directory" fallback branch.
const fakeGitFailWorktreeAdd = `#!/bin/sh
case "$1 $2" in
  "worktree add") echo "fatal: simulated worktree add failure" 1>&2; exit 1 ;;
  "rev-parse "*) exit 0 ;;
  "rev-parse") exit 0 ;;
esac
case "$1" in
  rev-parse) exit 0 ;;
esac
exit 0
`

func TestSetupWorktrees_WorktreeAddFails(t *testing.T) {
	installFakeGit(t, fakeGitFailWorktreeAdd)
	dir := t.TempDir()
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)

	// rev-parse succeeds (fake git => "is a repo"), but worktree add
	// fails, so the worker falls back to the shared working dir.
	if paths["vex"] != dir {
		t.Errorf("worker should fall back to shared dir on worktree-add failure, got %s", paths["vex"])
	}
	// Strategic agent always stays on the working dir.
	if paths["lead"] != dir {
		t.Errorf("strategic agent should stay on working dir, got %s", paths["lead"])
	}
}

func TestSetupAllAgentWorktrees_WorktreeAddFails(t *testing.T) {
	installFakeGit(t, fakeGitFailWorktreeAdd)
	dir := t.TempDir()
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}

	// SetupWorktreesWithHooks delegates to setupAllAgentWorktrees, which
	// has the same worktree-add fallback path.
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})
	// Worktree add failed => agent falls back to shared workingDir =>
	// hooks injection skipped (settings file should not exist).
	if paths["vex"] != dir {
		t.Errorf("agent should fall back to shared dir on worktree-add failure, got %s", paths["vex"])
	}
	settings := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings should be skipped on shared-dir fallback, err=%v", err)
	}
}

// fakeGitFailRemove makes `git worktree remove` and `git branch` fail
// while rev-parse and `worktree add` succeed. This drives both the
// "Failed to remove worktree" and "Failed to delete branch" branches in
// CleanupWorktrees. Worktree add must succeed so cleanup has a worktree
// path to attempt removing.
const fakeGitFailRemove = `#!/bin/sh
case "$1 $2" in
  "worktree remove") echo "fatal: simulated remove failure" 1>&2; exit 1 ;;
esac
case "$1" in
  branch) echo "fatal: simulated branch -D failure" 1>&2; exit 1 ;;
esac
exit 0
`

func TestCleanupWorktrees_RemoveAndBranchFail(t *testing.T) {
	installFakeGit(t, fakeGitFailRemove)
	dir := t.TempDir()
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	// CleanupWorktrees should not panic even when both git remove and
	// branch delete fail; it logs and moves on. Strategic agents are
	// skipped entirely.
	CleanupWorktrees(dir, members)
}

// fakeGitFailAll makes EVERY git invocation fail. rev-parse fails too,
// so SetupWorktrees takes the "not a git repo" fallback for all agents.
const fakeGitFailAll = `#!/bin/sh
echo "fatal: simulated total git failure" 1>&2
exit 128
`

func TestSetupWorktrees_RevParseFails(t *testing.T) {
	installFakeGit(t, fakeGitFailAll)
	dir := t.TempDir()
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)
	// rev-parse failed => treated as not-a-repo => every agent on shared dir.
	for _, m := range members {
		if paths[m.ID] != dir {
			t.Errorf("%s should fall back to shared dir, got %s", m.ID, paths[m.ID])
		}
	}
}

func TestSetupAllAgentWorktrees_RevParseFails(t *testing.T) {
	installFakeGit(t, fakeGitFailAll)
	dir := t.TempDir()
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})
	if paths["vex"] != dir {
		t.Errorf("agent should fall back to shared dir on rev-parse failure, got %s", paths["vex"])
	}
}
