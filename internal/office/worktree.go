package office

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SetupWorktrees creates isolated git worktrees for worker-tier agents.
// Strategic agents stay on the main branch (workingDir).
// Returns a map of configID -> worktree path.
func SetupWorktrees(workingDir string, members []*TeamMemberConfig) map[string]string {
	paths := make(map[string]string, len(members))

	// Check if we're in a git repo.
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		slog.Warn("Working directory is not a git repo -- all agents will share the same directory",
			"dir", workingDir)
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
		cmd := exec.Command("git", "worktree", "add", "-B", branchName, worktreePath)
		cmd.Dir = workingDir
		output, err := cmd.CombinedOutput()
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

// CleanupWorktrees removes all agent worktrees and their branches.
func CleanupWorktrees(workingDir string, members []*TeamMemberConfig) {
	worktreeDir := filepath.Join(workingDir, ".worktrees")

	for _, config := range members {
		if config.Tier == "strategic" {
			continue
		}

		worktreePath := filepath.Join(worktreeDir, config.ID)
		branchName := "agent/" + config.ID

		// Remove worktree.
		cmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
		cmd.Dir = workingDir
		if output, err := cmd.CombinedOutput(); err != nil {
			slog.Warn("Failed to remove worktree",
				"agent", config.Name,
				"err", err,
				"output", string(output))
		} else {
			slog.Info("Removed worktree", "agent", config.Name, "path", worktreePath)
		}

		// Delete branch.
		cmd = exec.Command("git", "branch", "-D", branchName)
		cmd.Dir = workingDir
		if output, err := cmd.CombinedOutput(); err != nil {
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
	defer f.Close()

	if _, err := f.WriteString("\n# Agent worktrees\n.worktrees/\n"); err != nil {
		slog.Warn("Could not write to .gitignore", "err", err)
		return
	}

	// Stage and commit the .gitignore change.
	addCmd := exec.Command("git", "add", ".gitignore")
	addCmd.Dir = workingDir
	if err := addCmd.Run(); err != nil {
		return
	}

	commitCmd := exec.Command("git", "commit", "-m", "chore: gitignore agent worktrees")
	commitCmd.Dir = workingDir
	_ = commitCmd.Run() // May fail if nothing to commit; that's fine.

	slog.Info("Added .worktrees/ to .gitignore")
}
