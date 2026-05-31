package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSavePersistedHandoffs_NoHomeReturnsError drives the empty-path branch:
// with HOME unset, handoffsPath() returns "" and savePersistedHandoffs reports
// "no home dir".
func TestSavePersistedHandoffs_NoHomeReturnsError(t *testing.T) {
	t.Setenv("HOME", "")
	if err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}}); err == nil {
		t.Error("expected an error when HOME is unset")
	}
}

// TestSavePersistedHandoffs_MkdirFails drives the MkdirAll error branch: the
// office-memory path's parent is a regular file, so MkdirAll fails.
func TestSavePersistedHandoffs_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make $HOME/.jarvis a file so MkdirAll(.jarvis/office-memory) fails.
	if err := os.WriteFile(filepath.Join(home, ".jarvis"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := savePersistedHandoffs([]HandoffRequest{{ID: "h1"}}); err == nil {
		t.Error("expected MkdirAll error when .jarvis is a file")
	}
}

// TestHandoffStoreAppend_SaveErrorLogged drives the append() persist-error
// branch: the audit append succeeds in memory but the disk write fails (the
// office-memory dir is blocked by a file), so the slog.Warn fires.
func TestHandoffStoreAppend_SaveErrorLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Block the office-memory directory with a file at .jarvis so the
	// MkdirAll inside savePersistedHandoffs fails.
	if err := os.WriteFile(filepath.Join(home, ".jarvis"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &handoffStore{}
	// Append must not panic even though the disk persist fails.
	s.append(HandoffRequest{ID: "h-audit", Status: HandoffExpired})

	// The in-memory audit log should still hold the entry.
	if got := s.snapshot(); len(got) != 1 || got[0].ID != "h-audit" {
		t.Errorf("expected in-memory audit entry retained despite save failure, got %v", got)
	}
}
