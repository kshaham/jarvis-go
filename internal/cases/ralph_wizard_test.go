package cases

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newRalphCase builds a case directory with a ralph-wizard subdir
// containing per-agent prompt/result files according to the spec.
// Returns the *CaseInfo pointer pointing at the case root.
func newRalphCase(t *testing.T, spec map[string][]string) *CaseInfo {
	t.Helper()
	root := t.TempDir()
	wizard := filepath.Join(root, "ralph-wizard")
	for agent, files := range spec {
		agentDir := filepath.Join(wizard, agent)
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(agentDir, f), []byte("body-"+agent+"-"+f), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &CaseInfo{Name: "test", Path: root}
}

func TestListRalphWizardFiles_NilCaseReturnsErr(t *testing.T) {
	_, err := ListRalphWizardFiles(nil)
	if !errors.Is(err, ErrRalphWizardDirMissing) {
		t.Errorf("expected ErrRalphWizardDirMissing, got %v", err)
	}
}

func TestListRalphWizardFiles_MissingDirReturnsErr(t *testing.T) {
	root := t.TempDir() // no ralph-wizard subdir
	_, err := ListRalphWizardFiles(&CaseInfo{Name: "n", Path: root})
	if !errors.Is(err, ErrRalphWizardDirMissing) {
		t.Errorf("expected ErrRalphWizardDirMissing, got %v", err)
	}
}

func TestListRalphWizardFiles_OnlyAgentsWithFiles(t *testing.T) {
	info := newRalphCase(t, map[string][]string{
		"alpha": {"prompt.md", "result.json"},
		"beta":  {"prompt.md"},                  // only prompt
		"gamma": {"result.json"},                // only result
		"empty": {},                             // neither — should be skipped
		"noise": {"random.txt"},                 // unrelated file — skipped
	})
	files, err := ListRalphWizardFiles(info)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	idx := map[string]RalphWizardFile{}
	for _, f := range files {
		idx[f.AgentType] = f
	}
	if idx["alpha"].PromptFile != "alpha/prompt.md" || idx["alpha"].ResultFile != "alpha/result.json" {
		t.Errorf("alpha: %+v", idx["alpha"])
	}
	if idx["beta"].PromptFile != "beta/prompt.md" || idx["beta"].ResultFile != "" {
		t.Errorf("beta: %+v", idx["beta"])
	}
	if idx["gamma"].ResultFile != "gamma/result.json" {
		t.Errorf("gamma: %+v", idx["gamma"])
	}
	if _, has := idx["empty"]; has {
		t.Errorf("empty agent should be skipped: %+v", idx["empty"])
	}
	if _, has := idx["noise"]; has {
		t.Errorf("noise agent should be skipped: %+v", idx["noise"])
	}
}

func TestReadRalphWizardFile_HappyPath(t *testing.T) {
	info := newRalphCase(t, map[string][]string{"alpha": {"prompt.md"}})
	data, err := ReadRalphWizardFile(info, "alpha/prompt.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(data) != "body-alpha-prompt.md" {
		t.Errorf("body=%q", data)
	}
}

func TestReadRalphWizardFile_EmptyPathRejected(t *testing.T) {
	info := newRalphCase(t, map[string][]string{"alpha": {"prompt.md"}})
	_, err := ReadRalphWizardFile(info, "")
	if !errors.Is(err, ErrRalphWizardPathInvalid) {
		t.Errorf("expected ErrRalphWizardPathInvalid, got %v", err)
	}
}

func TestReadRalphWizardFile_MissingFileReturnsErr(t *testing.T) {
	info := newRalphCase(t, map[string][]string{"alpha": {"prompt.md"}})
	_, err := ReadRalphWizardFile(info, "alpha/ghost.md")
	if !errors.Is(err, ErrRalphWizardFileMissing) {
		t.Errorf("expected ErrRalphWizardFileMissing, got %v", err)
	}
}

func TestReadRalphWizardFile_SymlinkEscapeRejected(t *testing.T) {
	info := newRalphCase(t, map[string][]string{"alpha": {"prompt.md"}})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Drop a symlink inside ralph-wizard pointing outside.
	link := filepath.Join(info.Path, "ralph-wizard", "escape")
	if err := os.Symlink(filepath.Join(outside, "secret"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := ReadRalphWizardFile(info, "escape")
	if !errors.Is(err, ErrRalphWizardPathInvalid) {
		t.Errorf("expected ErrRalphWizardPathInvalid for symlink escape, got %v", err)
	}
}

func TestReadRalphWizardFile_NilCase(t *testing.T) {
	_, err := ReadRalphWizardFile(nil, "anything")
	if !errors.Is(err, ErrRalphWizardDirMissing) {
		t.Errorf("expected ErrRalphWizardDirMissing, got %v", err)
	}
}

func TestClearRalphWizardDir_NilCaseNoOp(t *testing.T) {
	if err := ClearRalphWizardDir(nil); err != nil {
		t.Errorf("nil case should return nil, got %v", err)
	}
}

func TestClearRalphWizardDir_MissingDirNoOp(t *testing.T) {
	root := t.TempDir() // no ralph-wizard subdir
	if err := ClearRalphWizardDir(&CaseInfo{Name: "n", Path: root}); err != nil {
		t.Errorf("missing dir should return nil, got %v", err)
	}
}

func TestClearRalphWizardDir_RemovesContents(t *testing.T) {
	info := newRalphCase(t, map[string][]string{
		"alpha": {"prompt.md", "result.json"},
	})
	if err := ClearRalphWizardDir(info); err != nil {
		t.Fatalf("err: %v", err)
	}
	wizard := filepath.Join(info.Path, "ralph-wizard")
	if _, err := os.Stat(wizard); !os.IsNotExist(err) {
		t.Errorf("wizard dir should be removed, stat err=%v", err)
	}
}

func TestClearRalphWizardDir_IdempotentSecondCall(t *testing.T) {
	// Calling twice in a row must not fail — the second call hits
	// the "missing dir" no-op branch.
	info := newRalphCase(t, map[string][]string{"alpha": {"prompt.md"}})
	if err := ClearRalphWizardDir(info); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := ClearRalphWizardDir(info); err != nil {
		t.Errorf("second call should be no-op, got %v", err)
	}
}
