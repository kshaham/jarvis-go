package office

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wtInitRepo creates a fresh git repo in a temp dir with one commit on
// TestSafeWorktreeID covers the path-traversal guard: only plain slugs are
// safe to use as a .worktrees/ path component and a git branch suffix.
func TestSafeWorktreeID(t *testing.T) {
	safe := []string{"backend", "agent-1", "Lead_2", "a.b-c"}
	for _, id := range safe {
		if !safeWorktreeID(id) {
			t.Errorf("expected %q to be safe", id)
		}
	}
	unsafe := []string{"", ".", "..", "../../etc", "a/../../../tmp/x", "a/b", "has space", "x\ny", "semi;colon"}
	for _, id := range unsafe {
		if safeWorktreeID(id) {
			t.Errorf("expected %q to be REJECTED", id)
		}
	}
}

// HEAD so `git worktree add` has a base. Returns the repo dir. Skips the
// test if git is unavailable (CI without git).
func wtInitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runs := [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"checkout", "-b", "main"},
	}
	for _, args := range runs {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	// Need at least one commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestSetupWorktrees_NotAGitRepo(t *testing.T) {
	dir := t.TempDir() // plain dir, no git
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "bolt", Name: "Bolt", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)
	// Non-repo: every agent falls back to the shared working dir.
	for _, m := range members {
		if paths[m.ID] != dir {
			t.Errorf("expected %s fallback to %s, got %s", m.ID, dir, paths[m.ID])
		}
	}
}

func TestSetupWorktrees_RealRepo(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)

	// Strategic agent stays on the main working dir.
	if paths["lead"] != dir {
		t.Errorf("strategic agent should stay on working dir, got %s", paths["lead"])
	}
	// Worker gets a private worktree under .worktrees/.
	wantWorker := filepath.Join(dir, ".worktrees", "vex")
	if paths["vex"] != wantWorker {
		t.Errorf("worker worktree path = %s, want %s", paths["vex"], wantWorker)
	}
	if info, err := os.Stat(wantWorker); err != nil || !info.IsDir() {
		t.Fatalf("worker worktree dir not created: err=%v", err)
	}
	// The branch agent/vex must exist.
	cmd := exec.Command("git", "branch", "--list", "agent/vex")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "agent/vex") {
		t.Errorf("expected branch agent/vex to exist, branches: %s", out)
	}
	// .gitignore should now contain .worktrees.
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), ".worktrees") {
		t.Errorf(".gitignore should contain .worktrees, got %q", gi)
	}
}

func TestSetupWorktrees_ReusesExisting(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	first := SetupWorktrees(dir, members)
	// Drop a marker file in the worktree, then re-run: should reuse it (marker survives).
	marker := filepath.Join(first["vex"], "marker.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := SetupWorktrees(dir, members)
	if second["vex"] != first["vex"] {
		t.Errorf("reuse should return same path, got %s vs %s", second["vex"], first["vex"])
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("reused worktree lost its marker file: %v", err)
	}
}

func TestCleanupWorktrees_RemovesWorktreeAndBranch(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktrees(dir, members)
	worker := paths["vex"]
	if _, err := os.Stat(worker); err != nil {
		t.Fatalf("setup did not create worktree: %v", err)
	}

	CleanupWorktrees(dir, members)

	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be removed, stat err=%v", err)
	}
	// Branch should be deleted.
	cmd := exec.Command("git", "branch", "--list", "agent/vex")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "agent/vex") {
		t.Errorf("branch agent/vex should be deleted, got %s", out)
	}
}

func TestSetupWorktreesWithHooks_NoDaemonURL(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "", // empty => skip settings injection but still create worktrees
		Members:    members,
	})
	// Even strategic-style, hooks mode gives every member a private worktree.
	want := filepath.Join(dir, ".worktrees", "vex")
	if paths["vex"] != want {
		t.Errorf("hooks worktree path = %s, want %s", paths["vex"], want)
	}
	// No settings file should be written when daemonURL is empty.
	settings := filepath.Join(want, ".claude", "settings.local.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings.local.json should NOT exist without daemonURL, err=%v", err)
	}
}

func TestSetupWorktreesWithHooks_WritesSettingsAndCommands(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir:  dir,
		DaemonURL:   "http://localhost:9999",
		Members:     members,
		SpawnInfo:   []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
		EccDisabled: true,
	})
	cwd := paths["vex"]
	settings := filepath.Join(cwd, ".claude", "settings.local.json")
	data, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("settings.local.json should be written: %v", err)
	}
	if !strings.Contains(string(data), "sess-vex") {
		t.Errorf("settings should embed the sessionID, got %s", data)
	}
	// Office slash commands should be installed.
	cmdsDir := filepath.Join(cwd, ".claude", "commands")
	entries, err := os.ReadDir(cmdsDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected office commands written to %s, err=%v entries=%d", cmdsDir, err, len(entries))
	}
}

func TestSetupWorktreesWithHooks_MissingSpawnInfoSkipped(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo:  nil, // missing -> skip settings injection for vex
	})
	cwd := paths["vex"]
	settings := filepath.Join(cwd, ".claude", "settings.local.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings should be skipped when spawnInfo missing, err=%v", err)
	}
}

func TestCleanupHookSettings_RemovesWrittenFile(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo:  []HookSpawnInfo{{ConfigID: "vex", SessionID: "sess-vex"}},
	})
	settings := filepath.Join(dir, ".worktrees", "vex", ".claude", "settings.local.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("precondition: settings should exist: %v", err)
	}

	CleanupHookSettings(dir, members)

	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("CleanupHookSettings should remove jarvis-written settings, err=%v", err)
	}
}

func TestEnsureWorktreesGitignored_Idempotent(t *testing.T) {
	dir := wtInitRepo(t)
	gi := filepath.Join(dir, ".gitignore")
	// Pre-seed with .worktrees already present: should be a no-op.
	if err := os.WriteFile(gi, []byte("node_modules\n.worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(gi)
	ensureWorktreesGitignored(gi, dir)
	after, _ := os.ReadFile(gi)
	if string(before) != string(after) {
		t.Errorf("idempotent call modified .gitignore: before=%q after=%q", before, after)
	}
}

func TestEnsureWorktreesGitignored_AppendsAndCommits(t *testing.T) {
	dir := wtInitRepo(t)
	gi := filepath.Join(dir, ".gitignore")
	ensureWorktreesGitignored(gi, dir)
	content, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("gitignore not created: %v", err)
	}
	if !strings.Contains(string(content), ".worktrees/") {
		t.Errorf("gitignore should contain .worktrees/, got %q", content)
	}
}

func TestSetupWorktreesWithHooks_NotAGitRepo(t *testing.T) {
	dir := t.TempDir() // no git
	members := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Tier: "worker"},
		{ID: "lead", Name: "Lead", Tier: "strategic"},
	}
	paths := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		DaemonURL:  "http://localhost:9999",
		Members:    members,
		SpawnInfo: []HookSpawnInfo{
			{ConfigID: "vex", SessionID: "sess-vex"},
			{ConfigID: "lead", SessionID: "sess-lead"},
		},
	})
	// Non-repo: every member falls back to the shared workingDir, and
	// settings injection is skipped (would collide on the shared dir).
	for _, m := range members {
		if paths[m.ID] != dir {
			t.Errorf("%s should fall back to shared dir, got %s", m.ID, paths[m.ID])
		}
	}
	settings := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings should be skipped on shared-dir fallback, err=%v", err)
	}
}

func TestSetupWorktreesWithHooks_ReusesExistingWorktree(t *testing.T) {
	dir := wtInitRepo(t)
	members := []*TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}
	// First call creates the worktree.
	first := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		Members:    members,
	})
	// Second call should reuse the same path (exercises the os.Stat reuse
	// branch in setupAllAgentWorktrees).
	second := SetupWorktreesWithHooks(WorktreeHooksConfig{
		WorkingDir: dir,
		Members:    members,
	})
	if first["vex"] != second["vex"] {
		t.Errorf("reuse expected same path: %s vs %s", first["vex"], second["vex"])
	}
}

func TestCleanupHookSettings_NonExistentNoError(t *testing.T) {
	dir := t.TempDir()
	members := []*TeamMemberConfig{{ID: "ghost", Name: "Ghost", Tier: "worker"}}
	// No worktree / settings exist; this must be a harmless no-op.
	CleanupHookSettings(dir, members)
}

func TestRunGit_Timeout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := wtInitRepo(t)
	// A successful invocation returns nil error and output.
	out, err := runGit(gitProbeTimeout, dir, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatalf("rev-parse should succeed in a repo: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output from rev-parse")
	}
	// A bogus subcommand returns an error (exit status) plus output.
	if _, err := runGit(gitProbeTimeout, dir, "definitely-not-a-git-command"); err == nil {
		t.Error("expected error from bogus git subcommand")
	}
}

func TestHooksModeMcpServers_NoCrash(t *testing.T) {
	// PageIndex may or may not be available; either way this must not panic
	// and returns nil OR a map containing "pageindex".
	got := hooksModeMcpServers()
	if got != nil {
		if _, ok := got["pageindex"]; !ok {
			t.Errorf("non-nil mcp servers map should contain pageindex key, got %v", got)
		}
	}
}
