package office

import (
	"os"
	"path/filepath"
	"testing"
)

// Fake git that succeeds rev-parse + worktree add (so a worktree path
// is produced) but is otherwise a no-op. Used to drive the
// settings/commands/agents failure branches that only run AFTER a
// worktree was successfully created.
//
// We don't actually create a real git worktree; we make `git worktree
// add` a no-op success and pre-create the worktree directory ourselves
// so os.Stat(worktreePath).IsDir() is true and the reuse branch returns
// the path. That keeps the test fast and lets us pre-seed the failure
// conditions inside the worktree dir.
const fakeGitAllSucceed = `#!/bin/sh
exit 0
`

// preCreateWorktree makes <dir>/.worktrees/<id> exist as a directory so
// setupAllAgentWorktrees / SetupWorktrees take the "reuse existing"
// branch and return that path without shelling out for a real add.
func preCreateWorktree(t *testing.T, dir, id string) string {
	t.Helper()
	wt := filepath.Join(dir, ".worktrees", id)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	return wt
}

// TestSetupWorktrees_MkdirWorktreeDirFails drives the os.MkdirAll
// failure branch in SetupWorktrees (lines 81-87): planting a regular
// FILE at the .worktrees path makes MkdirAll return ENOTDIR, so every
// agent falls back to the shared working dir.
func TestSetupWorktrees_MkdirWorktreeDirFails(t *testing.T) {
	installFakeGit(t, fakeGitAllSucceed)
	dir := t.TempDir()
	// Plant a FILE where .worktrees/ should be a directory.
	if err := os.WriteFile(filepath.Join(dir, ".worktrees"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)
	for _, m := range members {
		if paths[m.ID] != dir {
			t.Errorf("%s should fall back to shared dir on mkdir failure, got %s", m.ID, paths[m.ID])
		}
	}
}

// TestSetupAllAgentWorktrees_MkdirWorktreeDirFails drives the same
// MkdirAll failure branch in setupAllAgentWorktrees (lines 299-305).
func TestSetupAllAgentWorktrees_MkdirWorktreeDirFails(t *testing.T) {
	installFakeGit(t, fakeGitAllSucceed)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".worktrees"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})
	if paths["vex"] != dir {
		t.Errorf("agent should fall back to shared dir on mkdir failure, got %s", paths["vex"])
	}
}

// TestSetupWorktreesWithHooks_OperatorOwnedSettings drives the
// ErrOperatorOwned branch (lines 220-224). We pre-create the worktree
// with an operator-authored .claude/settings.local.json (no Jarvis
// marker), so WriteSettings refuses to overwrite it and the hooks path
// skips that agent.
func TestSetupWorktreesWithHooks_OperatorOwnedSettings(t *testing.T) {
	installFakeGit(t, fakeGitAllSucceed)
	dir := t.TempDir()
	wt := preCreateWorktree(t, dir, "vex")
	// Plant an operator-owned settings file (no Jarvis marker).
	claudeDir := filepath.Join(wt, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	operatorContent := []byte(`{"hooks":{"operator":"hand-written"}}`)
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), operatorContent, 0o644); err != nil {
		t.Fatal(err)
	}

	SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}},
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})

	// The operator's file must be left untouched.
	got, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(operatorContent) {
		t.Errorf("operator settings should be preserved, got %s", got)
	}
}

// TestSetupWorktreesWithHooks_WriteSettingsHardError drives the generic
// WriteSettings error branch (lines 215-218): making the worktree's
// .claude path a regular FILE makes WriteSettings' MkdirAll fail with
// ENOTDIR (not ErrOperatorOwned). The agent is skipped without a panic.
func TestSetupWorktreesWithHooks_WriteSettingsHardError(t *testing.T) {
	installFakeGit(t, fakeGitAllSucceed)
	dir := t.TempDir()
	wt := preCreateWorktree(t, dir, "vex")
	// Plant a FILE at .claude so MkdirAll(.claude) fails (ENOTDIR).
	if err := os.WriteFile(filepath.Join(wt, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Must not panic; the agent is simply skipped.
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}},
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})
	if paths["vex"] != wt {
		t.Errorf("worktree path should be the reused dir, got %s", paths["vex"])
	}
}

// TestSetupWorktreesWithHooks_WriteCommandsAgentsFail drives the
// WriteCommands / WriteAgents / WriteStatusLine failure branches (lines
// 240/257/271). WriteSettings succeeds (creating a real .claude/ dir),
// then we make .claude/commands and .claude/agents un-createable by
// planting FILES at those paths. The whole flow must survive and still
// write the settings file.
func TestSetupWorktreesWithHooks_WriteCommandsAgentsFail(t *testing.T) {
	installFakeGit(t, fakeGitAllSucceed)
	dir := t.TempDir()
	wt := preCreateWorktree(t, dir, "vex")
	claudeDir := filepath.Join(wt, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant FILES where commands/ and agents/ dirs are expected so their
	// MkdirAll calls fail with ENOTDIR. WriteStatusLine writes
	// .claude/statusline-* / output-styles; planting a file at
	// output-styles forces its failure too.
	for _, name := range []string{"commands", "agents"} {
		if err := os.WriteFile(filepath.Join(claudeDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir:  dir,
		DaemonURL:   "http://localhost:9999",
		Members:     []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}},
		SpawnInfo:   []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
		EccDisabled: true,
	})

	// Settings should still have been written despite the command/agent
	// write failures (those are best-effort, logged not fatal).
	settings := filepath.Join(claudeDir, "settings.local.json")
	if _, err := os.Stat(settings); err != nil {
		t.Errorf("settings should be written even when commands/agents writes fail: %v", err)
	}
}

// TestCleanupHookSettings_RemoveError drives the non-OperatorOwned
// remove-failure branch in CleanupHookSettings (lines 342-344). We make
// .claude/settings.local.json a DIRECTORY so RemoveSettings' ReadFile
// returns EISDIR (not IsNotExist, not OperatorOwned), exercising the
// error log path. Must not panic.
func TestCleanupHookSettings_RemoveError(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, ".worktrees", "vex")
	claudeDir := filepath.Join(wt, ".claude")
	// Make settings.local.json a directory.
	if err := os.MkdirAll(filepath.Join(claudeDir, "settings.local.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Must not panic; logs and continues.
	CleanupHookSettings(dir, []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}})
}

// TestEnsureWorktreesGitignored_OpenFileError drives the os.OpenFile
// failure branch (lines 389-392). Making .gitignore a DIRECTORY makes
// os.OpenFile(... O_WRONLY) fail. ensureWorktreesGitignored must log and
// return without panicking.
func TestEnsureWorktreesGitignored_OpenFileError(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	if err := os.MkdirAll(gitignore, 0o755); err != nil {
		t.Fatal(err)
	}
	// Must not panic. The directory has no ".worktrees" substring in its
	// (unreadable) "content", so we reach the OpenFile attempt, which
	// fails on a directory.
	ensureWorktreesGitignored(gitignore, dir)
}
