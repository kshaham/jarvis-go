package cases

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Ralph-wizard file management.
//
// Each case directory may contain a ralph-wizard/ subdirectory with
// per-agent prompt + result files. The handlers extract is mostly
// file-system walking + symlink-safe path validation; both move
// here so the HTTP layer stays narrow.

// RalphWizardFile describes one agent's wizard entry: a prompt
// file and/or a result file. Either may be empty if the
// corresponding file isn't yet on disk.
type RalphWizardFile struct {
	AgentType  string `json:"agentType"`
	PromptFile string `json:"promptFile,omitempty"`
	ResultFile string `json:"resultFile,omitempty"`
}

// Sentinel errors for ralph-wizard reads.
var (
	ErrRalphWizardDirMissing = errors.New("ralph wizard directory not found")
	ErrRalphWizardFileMissing = errors.New("file not found")
	ErrRalphWizardPathInvalid = errors.New("invalid file path")
)

// ClearRalphWizardDir removes the ralph-wizard/ subdirectory under
// caseInfo.Path, if present. Called before a fresh plan-generation
// run so prior agent prompts/results don't bleed into a new pass.
//
// Tolerant of two no-op cases:
//   - caseInfo is nil: returns nil (caller has no case to clear).
//   - wizard dir doesn't exist: returns nil (already clean).
//
// Failure to remove an existing directory is surfaced so the
// handler can log it; the new plan run may still go ahead with
// stale files, but operator should know.
func ClearRalphWizardDir(caseInfo *CaseInfo) error {
	if caseInfo == nil {
		return nil
	}
	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
	if _, err := os.Stat(wizardDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat wizard dir: %w", err)
	}
	if err := os.RemoveAll(wizardDir); err != nil {
		return fmt.Errorf("remove wizard dir: %w", err)
	}
	return nil
}

// ListRalphWizardFiles walks ralph-wizard/<agent>/ subdirectories
// of caseInfo.Path and returns one entry per agent that has at
// least one of prompt.md / result.json on disk. Skips agent dirs
// with neither file.
//
// Returns an empty slice (not nil) so the JSON encoder produces []
// rather than null. Returns ErrRalphWizardDirMissing when
// ralph-wizard/ itself doesn't exist.
func ListRalphWizardFiles(caseInfo *CaseInfo) ([]RalphWizardFile, error) {
	if caseInfo == nil {
		return nil, ErrRalphWizardDirMissing
	}
	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
	entries, err := os.ReadDir(wizardDir)
	if err != nil {
		return nil, ErrRalphWizardDirMissing
	}
	files := make([]RalphWizardFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(wizardDir, entry.Name())
		wf := RalphWizardFile{AgentType: entry.Name()}
		if _, statErr := os.Stat(filepath.Join(agentDir, "prompt.md")); statErr == nil {
			wf.PromptFile = entry.Name() + "/prompt.md"
		}
		if _, statErr := os.Stat(filepath.Join(agentDir, "result.json")); statErr == nil {
			wf.ResultFile = entry.Name() + "/result.json"
		}
		if wf.PromptFile != "" || wf.ResultFile != "" {
			files = append(files, wf)
		}
	}
	return files, nil
}

// ReadRalphWizardFile reads relPath relative to the case's
// ralph-wizard directory. Symlink-safe: EvalSymlinks resolves the
// path chain and rejects anything that escapes the wizard
// directory, even via a malicious symlink an agent might have
// dropped inside ralph-wizard/.
//
// Returns:
//   - ErrRalphWizardFileMissing when the file (or any path
//     component along the way) doesn't exist.
//   - ErrRalphWizardPathInvalid when the resolved path escapes
//     the wizard directory or when EvalSymlinks fails for any
//     reason other than os.IsNotExist.
//   - the file's raw bytes + nil error on success.
//
// Wraps the underlying os.ReadFile / EvalSymlinks errors with
// `%w` so handler-side errors.Is() checks work cleanly.
func ReadRalphWizardFile(caseInfo *CaseInfo, relPath string) ([]byte, error) {
	if caseInfo == nil {
		return nil, ErrRalphWizardDirMissing
	}
	if relPath == "" {
		return nil, fmt.Errorf("%w: empty path", ErrRalphWizardPathInvalid)
	}
	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
	fullPath := filepath.Join(wizardDir, relPath)
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRalphWizardFileMissing
		}
		return nil, fmt.Errorf("%w: %v", ErrRalphWizardPathInvalid, err)
	}
	resolvedWizard, err := filepath.EvalSymlinks(wizardDir)
	if err != nil {
		return nil, fmt.Errorf("resolve wizard dir: %w", err)
	}
	if !strings.HasPrefix(resolvedPath, resolvedWizard+string(filepath.Separator)) && resolvedPath != resolvedWizard {
		return nil, fmt.Errorf("%w: escapes wizard directory", ErrRalphWizardPathInvalid)
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRalphWizardFileMissing
		}
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}
