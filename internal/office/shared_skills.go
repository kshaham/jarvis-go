package office

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Shared-skill management.
//
// Agents can create / update / delete entries in ~/.claude/skills/
// (the location the Claude CLI auto-discovers SKILL.md files from)
// via cm CLI commands. The file I/O + path-traversal-prevention
// logic lives here so the handler stays narrow.
//
// Path-traversal defence: skill names are validated by the caller
// against safeName(), but the file operations re-check that the
// resolved skill directory stays within the skills root. Defence in
// depth — a future caller bypassing safeName() shouldn't be able
// to escape /.claude/skills/.

// ErrSkillPathTraversal is returned by skill helpers when the
// resolved path escapes the skills root. Treated as a 400 by the
// handler (the operator's name validation should have caught it
// first; this is the backstop).
var ErrSkillPathTraversal = errors.New("invalid skill name (path traversal)")

// sharedSkillsRoot returns the root directory the Claude CLI scans
// for shared skills. Pinned to ~/.claude/skills regardless of the
// jarvis HOME override.
func sharedSkillsRoot() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude", "skills")
}

// WriteSharedSkill creates or updates ~/.claude/skills/<name>/SKILL.md
// with the given body. Returns the full path to the SKILL.md file,
// a boolean indicating whether the file existed prior to this call
// (true → updated, false → created), and any error.
//
// Errors:
//   - ErrSkillPathTraversal when the resolved skill directory
//     escapes the skills root.
//   - Wrapped I/O errors for mkdir / write failures.
func WriteSharedSkill(name, content string) (path string, updated bool, err error) {
	skillDir := filepath.Join(sharedSkillsRoot(), name)
	if !isWithinSkillsRoot(skillDir) {
		return "", false, ErrSkillPathTraversal
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create skill dir: %w", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	_, statErr := os.Stat(skillPath)
	updated = statErr == nil
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		return "", false, fmt.Errorf("write skill file: %w", err)
	}
	return skillPath, updated, nil
}

// DeleteSharedSkill removes ~/.claude/skills/<name> recursively.
// Returns ErrSkillPathTraversal when the resolved path escapes
// the skills root, or a wrapped I/O error.
func DeleteSharedSkill(name string) error {
	skillPath := filepath.Join(sharedSkillsRoot(), name)
	if !isWithinSkillsRoot(skillPath) {
		return ErrSkillPathTraversal
	}
	if err := os.RemoveAll(skillPath); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	return nil
}

// isWithinSkillsRoot checks that the absolute resolution of path
// stays under the absolute resolution of sharedSkillsRoot. Trailing
// path separator matters — without it, /.claude/skillsXY would
// match /.claude/skills.
func isWithinSkillsRoot(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(sharedSkillsRoot())
	if err != nil {
		return false
	}
	// Allow the skills root itself (empty name) — caller decides
	// whether that's meaningful.
	if abs == absRoot {
		return true
	}
	return len(abs) > len(absRoot) && abs[:len(absRoot)+1] == absRoot+string(filepath.Separator)
}
