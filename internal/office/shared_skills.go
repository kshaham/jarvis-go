package office

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// sharedSkillsMu serializes the mutating skill helpers. The idle-monitor skill
// promotion path (memify_tick.go -> WriteSharedSkill) and concurrent HTTP
// skill-create calls would otherwise race on the same SKILL.md: a TOCTOU on the
// stat/write and two writers clobbering each other. All writes/deletes hold it.
var sharedSkillsMu sync.Mutex

// sharedSkillsRoot returns the root directory the Claude CLI scans for shared
// skills (~/.claude/skills), or "" when the home directory can't be resolved.
// Callers MUST treat "" as a hard error -- returning a cwd-relative
// ".claude/skills" (the old silent behavior) would write skills under whatever
// directory the process happens to run in.
func sharedSkillsRoot() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return ""
	}
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
	sharedSkillsMu.Lock()
	defer sharedSkillsMu.Unlock()

	root := sharedSkillsRoot()
	if root == "" {
		return "", false, errors.New("cannot resolve home directory for shared skills")
	}
	skillDir := filepath.Join(root, name)
	if !isWithinSkillsRoot(skillDir) {
		return "", false, ErrSkillPathTraversal
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create skill dir: %w", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	_, statErr := os.Stat(skillPath)
	updated = statErr == nil
	// Atomic write: tmp + rename so an interrupted write (disk full, crash)
	// can't leave a partially-written SKILL.md that the frontmatter parser
	// would then read as valid-but-incomplete.
	tmp := skillPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", false, fmt.Errorf("write skill file: %w", err)
	}
	if err := os.Rename(tmp, skillPath); err != nil {
		_ = os.Remove(tmp)
		return "", false, fmt.Errorf("finalize skill file: %w", err)
	}
	return skillPath, updated, nil
}

// DeleteSharedSkill removes ~/.claude/skills/<name> recursively.
// Returns ErrSkillPathTraversal when the resolved path escapes
// the skills root, or a wrapped I/O error.
func DeleteSharedSkill(name string) error {
	sharedSkillsMu.Lock()
	defer sharedSkillsMu.Unlock()

	root := sharedSkillsRoot()
	if root == "" {
		return errors.New("cannot resolve home directory for shared skills")
	}
	skillPath := filepath.Join(root, name)
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
	root := sharedSkillsRoot()
	if root == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
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
