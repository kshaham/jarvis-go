package office

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/claudeman/jarvis-go/internal/claudehooks"
)

// Git subprocess timeouts. A hung git (ssh-key prompt, .git/index
// lock contention, network filesystem stall) was previously a 12h
// killer because exec.Command without a context blocks forever.
//
// gitProbeTimeout covers cheap reads (rev-parse, status). Worktree
// add/remove sometimes need to clone or remove large trees, so they
// get a more generous gitWorktreeOpTimeout. Both well over the
// expected p99 — these are circuit-breakers, not deadlines.
const (
	gitProbeTimeout       = 30 * time.Second
	gitWorktreeOpTimeout  = 5 * time.Minute
	gitCommitOpTimeout    = 60 * time.Second
)

// runGit runs git with the supplied timeout + working directory.
// Returns the command output (always, even on failure, for logging)
// and the error. Any timeout surfaces as context.DeadlineExceeded
// wrapped in *exec.ExitError so callers' existing err-handling
// branches still fire.
func runGit(timeout time.Duration, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// HookSpawnInfo carries the per-agent data needed to write the
// .claude/settings.local.json that wires Claude Code hooks to the
// daemon. Built by the office spawn handler once it knows the
// per-agent sessionID and passed into SetupWorktreesWithHooks for
// the (worktreePath, sessionID) pairing.
type HookSpawnInfo struct {
	ConfigID  string // matches TeamMemberConfig.ID
	SessionID string // assigned by the spawn handler
}

// SetupWorktrees creates isolated git worktrees for worker-tier agents.
// Strategic agents stay on the main branch (workingDir).
// Returns a map of configID -> worktree path.
//
// Use SetupWorktreesWithHooks instead when officeSessionMode == "hooks"
// so each agent's settings.local.json gets written alongside its
// worktree (or workingDir for strategic agents).
func SetupWorktrees(workingDir string, members []*TeamMemberConfig) map[string]string {
	paths := make(map[string]string, len(members))

	// Check if we're in a git repo (timeout-bounded so a hung git
	// can't wedge the whole team-spawn pipeline).
	if _, err := runGit(gitProbeTimeout, workingDir, "rev-parse", "--git-dir"); err != nil {
		slog.Warn("Working directory is not a git repo -- all agents will share the same directory",
			"dir", workingDir, "err", err)
		for _, m := range members {
			paths[m.ID] = workingDir
		}
		return paths
	}

	worktreeDir := filepath.Join(workingDir, ".worktrees")

	// Ensure .worktrees is gitignored.
	gitignorePath := filepath.Join(workingDir, ".gitignore")
	ensureWorktreesGitignored(gitignorePath, workingDir)

	// Create worktree base directory.
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		slog.Warn("Could not create worktree directory -- falling back to shared directory",
			"err", err)
		for _, m := range members {
			paths[m.ID] = workingDir
		}
		return paths
	}

	for _, config := range members {
		// Strategic-tier agents work on the main branch.
		if config.Tier == "strategic" {
			paths[config.ID] = workingDir
			continue
		}

		worktreePath := filepath.Join(worktreeDir, config.ID)
		branchName := "agent/" + config.ID

		// If worktree already exists, reuse it.
		if info, err := os.Stat(worktreePath); err == nil && info.IsDir() {
			paths[config.ID] = worktreePath
			slog.Info("Reusing existing worktree", "agent", config.Name, "path", worktreePath)
			continue
		}

		// Create a new worktree with a fresh branch from current HEAD.
		output, err := runGit(gitWorktreeOpTimeout, workingDir, "worktree", "add", "-B", branchName, worktreePath)
		if err != nil {
			slog.Warn("Failed to create worktree -- using shared directory",
				"agent", config.Name,
				"err", err,
				"output", string(output))
			paths[config.ID] = workingDir
			continue
		}

		paths[config.ID] = worktreePath
		slog.Info("Created worktree",
			"agent", config.Name,
			"path", worktreePath,
			"branch", branchName)
	}

	return paths
}

// SetupWorktreesWithHooks creates a per-agent git worktree for EVERY
// member (workers AND strategic agents -- diverging from
// SetupWorktrees, which keeps strategic agents on the main branch)
// and writes a per-agent .claude/settings.local.json into each so
// spawned `claude` processes POST hook events back to the daemon.
//
// Why force worktrees even for strategic agents: hook URLs include
// the Jarvis sessionID. If two agents share a cwd they share a
// settings file, which can only point at ONE sessionID -- the others
// get fail-open dropped at the dispatcher. Per-agent worktrees keep
// the cwd unique so every agent's hook URL routes correctly.
//
// Why we don't use Claude's `--worktree` flag (R3.1 design note):
// Claude creates worktrees AFTER process start, but settings.local.json
// must exist BEFORE process start for hooks routing to work (Claude
// reads it once at init). Letting Claude create the worktree means
// the directory is empty when settings would be read, defeating the
// entire hooks pipeline. R3.1 keeps jarvis-owned worktree creation
// and instead wires the WorktreeCreate hook (claudehooks.handlers.go)
// so any worktree Claude creates internally (Task tool, --bg) updates
// AgentState.WorktreePath for the dashboard + Phase R4 plumbing.
//
// Caller (spawnAgentSession) MUST use the returned paths as the
// per-agent spawn cwd, not workingDir. If git worktree creation
// fails for any agent, that agent gets a fallback path (workingDir)
// and is silently excluded from hooks injection (logged warning).
// WorktreeHooksConfig bundles the inputs SetupWorktreesWithHooks
// needs. P12.2 · Replaces a 5-positional-argument signature so future
// fields don't break callers (the per-team toggles list grows; each
// addition was a positional-arg shuffle pre-Phase-12).
type WorktreeHooksConfig struct {
	// WorkingDir is the team's shared parent directory. Per-agent
	// worktrees are created as siblings.
	WorkingDir string
	// DaemonURL is the jarvis HTTP endpoint each agent's
	// settings.local.json points at for hook callbacks. Empty
	// skips settings injection entirely (warning logged).
	DaemonURL string
	// Members is the team roster; each entry gets its own worktree
	// + agent/command/settings injection.
	Members []*TeamMemberConfig
	// SpawnInfo carries the per-agent (sessionId, configId) pairing
	// the hook URLs need to route. Must align with Members; missing
	// entries are skipped with a warning.
	SpawnInfo []HookSpawnInfo
	// EccDisabled toggles whether the vendored ECC bundle is layered
	// onto the standard office commands + agents. false (default) =
	// ship jarvis's office content + ECC's 3 commands + 4 agents;
	// true = office only.
	EccDisabled bool
}

func SetupWorktreesWithHooks(cfg WorktreeHooksConfig) map[string]string {
	paths := setupAllAgentWorktrees(cfg.WorkingDir, cfg.Members)
	if cfg.DaemonURL == "" {
		slog.Warn("hooks-mode requested but daemonURL is empty; skipping settings injection")
		return paths
	}

	infoBy := make(map[string]HookSpawnInfo, len(cfg.SpawnInfo))
	for _, info := range cfg.SpawnInfo {
		infoBy[info.ConfigID] = info
	}

	for _, m := range cfg.Members {
		info, ok := infoBy[m.ID]
		if !ok || info.SessionID == "" {
			slog.Warn("hooks-mode: missing spawnInfo for member; skipping settings injection",
				"configId", m.ID, "name", m.Name)
			continue
		}
		cwd := paths[m.ID]
		if cwd == "" || cwd == cfg.WorkingDir {
			// Worktree creation failed for this agent -- they fell back
			// to the shared workingDir. Skip hooks injection rather than
			// stamp a settings file that would collide with other agents
			// (or the operator's own .claude/settings.local.json).
			slog.Warn("hooks-mode: agent on shared workingDir; skipping hooks injection (agent runs as legacy ModeClaude)",
				"agent", m.Name)
			continue
		}

		data, err := claudehooks.BuildSettings(claudehooks.SettingsParams{
			SessionID: info.SessionID,
			DaemonURL: cfg.DaemonURL,
		})
		if err != nil {
			slog.Warn("hooks-mode: BuildSettings failed; skipping injection",
				"agent", m.Name, "err", err)
			continue
		}
		if err := claudehooks.WriteSettings(cwd, data); err != nil {
			if errors.Is(err, claudehooks.ErrOperatorOwned) {
				slog.Warn("hooks-mode: operator-authored .claude/settings.local.json detected; leaving it alone (agent will run without hooks)",
					"agent", m.Name, "cwd", cwd)
				continue
			}
			slog.Warn("hooks-mode: WriteSettings failed", "agent", m.Name, "err", err)
			continue
		}
		slog.Info("hooks-mode: wrote settings.local.json",
			"agent", m.Name, "cwd", cwd, "sessionId", info.SessionID)

		// R2.1 · install office slash commands (/standup, /memory,
		// /handoff, etc.) so the agent doesn't have to remember the
		// `cm ...` syntax. P10 · layer the 3 ECC commands on top
		// when ECC is enabled for the team (i.e. !cfg.EccDisabled).
		cmds := BuildOfficeCommands()
		if !cfg.EccDisabled {
			cmds = append(cmds, BuildECCCommands()...)
		}
		if n, err := WriteCommands(cwd, cmds); err != nil {
			slog.Warn("R2.1: WriteCommands failed", "agent", m.Name, "err", err)
		} else if n > 0 {
			slog.Info("R2.1: wrote office slash commands",
				"agent", m.Name, "count", n,
				"commands", CommandNamesForLogging(cmds),
				"eccDisabled", cfg.EccDisabled)
		}
		// R2.2 · install Task-tool subagent definitions so the
		// office-aware reviewer / researcher / test-runner are
		// available without operator setup. P10 · layer ECC's 4 agents
		// (code-architect, security-reviewer, pr-test-analyzer,
		// code-simplifier) on top when ECC is enabled.
		agents := BuildOfficeAgents()
		if !cfg.EccDisabled {
			agents = append(agents, BuildECCAgents()...)
		}
		if n, err := WriteAgents(cwd, agents); err != nil {
			slog.Warn("R2.2: WriteAgents failed", "agent", m.Name, "err", err)
		} else if n > 0 {
			slog.Info("R2.2: wrote office subagents",
				"agent", m.Name, "count", n,
				"agents", AgentNamesForLogging(agents),
				"eccDisabled", cfg.EccDisabled)
		}
		// R2.3 · install per-session statusline + output-style so
		// Claude's terminal renders office state (current task, paused
		// flag, blocker count) at the bottom of every prompt.
		if err := WriteStatusLine(cwd, StatusLineParams{
			SessionID: info.SessionID,
			DaemonURL: cfg.DaemonURL,
		}); err != nil {
			slog.Warn("R2.3: WriteStatusLine failed", "agent", m.Name, "err", err)
		}
	}
	return paths
}

// setupAllAgentWorktrees is SetupWorktrees minus the
// strategic-stays-on-main optimization. Every member gets a private
// worktree on `agent/<id>` so per-cwd settings injection works.
// Falls back to workingDir per-agent on any error (same fallback
// pattern as SetupWorktrees).
func setupAllAgentWorktrees(workingDir string, members []*TeamMemberConfig) map[string]string {
	paths := make(map[string]string, len(members))

	if _, err := runGit(gitProbeTimeout, workingDir, "rev-parse", "--git-dir"); err != nil {
		slog.Warn("Working directory is not a git repo -- hooks-mode falling back to shared workingDir for all agents",
			"dir", workingDir, "err", err)
		for _, m := range members {
			paths[m.ID] = workingDir
		}
		return paths
	}

	worktreeDir := filepath.Join(workingDir, ".worktrees")
	gitignorePath := filepath.Join(workingDir, ".gitignore")
	ensureWorktreesGitignored(gitignorePath, workingDir)

	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		slog.Warn("Could not create worktree directory -- hooks-mode falling back to shared workingDir",
			"err", err)
		for _, m := range members {
			paths[m.ID] = workingDir
		}
		return paths
	}

	for _, config := range members {
		worktreePath := filepath.Join(worktreeDir, config.ID)
		branchName := "agent/" + config.ID

		if info, err := os.Stat(worktreePath); err == nil && info.IsDir() {
			paths[config.ID] = worktreePath
			slog.Info("Reusing existing worktree", "agent", config.Name, "path", worktreePath)
			continue
		}

		output, err := runGit(gitWorktreeOpTimeout, workingDir, "worktree", "add", "-B", branchName, worktreePath)
		if err != nil {
			slog.Warn("Failed to create worktree -- using shared workingDir for this agent",
				"agent", config.Name, "err", err, "output", string(output))
			paths[config.ID] = workingDir
			continue
		}

		paths[config.ID] = worktreePath
		slog.Info("Created worktree (hooks-mode)",
			"agent", config.Name, "path", worktreePath, "branch", branchName)
	}
	return paths
}

// CleanupHookSettings removes any Jarvis-written
// .claude/settings.local.json files from the team's per-agent
// directories. Called from the team teardown path. Operator-authored
// files are left alone (RemoveSettings checks the marker). Mirrors
// setupAllAgentWorktrees -- every agent (regardless of tier) gets a
// {workingDir}/.worktrees/{id}/ candidate path.
func CleanupHookSettings(workingDir string, members []*TeamMemberConfig) {
	for _, m := range members {
		cwd := filepath.Join(workingDir, ".worktrees", m.ID)
		if err := claudehooks.RemoveSettings(cwd); err != nil && !errors.Is(err, claudehooks.ErrOperatorOwned) {
			slog.Debug("CleanupHookSettings: remove failed", "agent", m.Name, "err", err)
		}
	}
}

// CleanupWorktrees removes all agent worktrees and their branches.
func CleanupWorktrees(workingDir string, members []*TeamMemberConfig) {
	worktreeDir := filepath.Join(workingDir, ".worktrees")

	for _, config := range members {
		if config.Tier == "strategic" {
			continue
		}

		worktreePath := filepath.Join(worktreeDir, config.ID)
		branchName := "agent/" + config.ID

		// Remove worktree (timeout-bounded; git can stall on a busy
		// .git/index lock during heavy team churn).
		if output, err := runGit(gitWorktreeOpTimeout, workingDir, "worktree", "remove", worktreePath, "--force"); err != nil {
			slog.Warn("Failed to remove worktree",
				"agent", config.Name,
				"err", err,
				"output", string(output))
		} else {
			slog.Info("Removed worktree", "agent", config.Name, "path", worktreePath)
		}

		// Delete branch.
		if output, err := runGit(gitProbeTimeout, workingDir, "branch", "-D", branchName); err != nil {
			slog.Debug("Failed to delete branch (may not exist)",
				"branch", branchName,
				"err", err,
				"output", string(output))
		}
	}
}

// ensureWorktreesGitignored adds .worktrees/ to .gitignore if not already present.
func ensureWorktreesGitignored(gitignorePath, workingDir string) {
	content, _ := os.ReadFile(gitignorePath)
	if strings.Contains(string(content), ".worktrees") {
		return
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("Could not update .gitignore for worktrees", "err", err)
		return
	}
	// Log close errors so a flush-failure on an unusual filesystem
	// (read-only mount, fuse misbehaving, etc.) surfaces rather than
	// vanishing into defer.
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("Could not close .gitignore after append", "path", gitignorePath, "err", cerr)
		}
	}()

	if _, err := f.WriteString("\n# Agent worktrees\n.worktrees/\n"); err != nil {
		slog.Warn("Could not write to .gitignore", "err", err)
		return
	}

	// Stage and commit the .gitignore change.
	if _, err := runGit(gitProbeTimeout, workingDir, "add", ".gitignore"); err != nil {
		return
	}
	// May fail if nothing to commit; that's fine.
	_, _ = runGit(gitCommitOpTimeout, workingDir, "commit", "-m", "chore: gitignore agent worktrees")

	slog.Info("Added .worktrees/ to .gitignore")
}
