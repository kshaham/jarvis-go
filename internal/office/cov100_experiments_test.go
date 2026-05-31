package office

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// failWriteCloser fails on Write (or Close) so RecordExperiment's disk-full
// branches can be exercised honestly.
type failWriteCloser struct {
	failWrite bool
	failClose bool
}

func (f *failWriteCloser) Write(p []byte) (int, error) {
	if f.failWrite {
		return 0, errors.New("write failed: disk full")
	}
	return len(p), nil
}
func (f *failWriteCloser) Close() error {
	if f.failClose {
		return errors.New("close failed: EIO")
	}
	return nil
}

// withExperimentsHome points HOME at a temp dir with the office-memory dir
// present so RecordExperiment reaches the open step.
func withExperimentsHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".jarvis", "office-memory"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestRecordExperiment_WriteFails injects a writer whose Write fails.
func TestRecordExperiment_WriteFails(t *testing.T) {
	withExperimentsHome(t)
	orig := experimentsOpenFile
	t.Cleanup(func() { experimentsOpenFile = orig })
	experimentsOpenFile = func(string) (io.WriteCloser, error) {
		return &failWriteCloser{failWrite: true}, nil
	}
	if err := RecordExperiment(ExperimentRecord{TeamTemplate: "t"}); err == nil {
		t.Fatal("expected write error")
	}
}

// TestRecordExperiment_CloseFails injects a writer whose Close fails after a
// successful write.
func TestRecordExperiment_CloseFails(t *testing.T) {
	withExperimentsHome(t)
	orig := experimentsOpenFile
	t.Cleanup(func() { experimentsOpenFile = orig })
	experimentsOpenFile = func(string) (io.WriteCloser, error) {
		return &failWriteCloser{failClose: true}, nil
	}
	err := RecordExperiment(ExperimentRecord{TeamTemplate: "t"})
	if err == nil {
		t.Fatal("expected close error")
	}
}

// TestRecordExperiment_OpenSeamError injects an open failure.
func TestRecordExperiment_OpenSeamError(t *testing.T) {
	withExperimentsHome(t)
	orig := experimentsOpenFile
	t.Cleanup(func() { experimentsOpenFile = orig })
	experimentsOpenFile = func(string) (io.WriteCloser, error) {
		return nil, errors.New("open failed")
	}
	if err := RecordExperiment(ExperimentRecord{TeamTemplate: "t"}); err == nil {
		t.Fatal("expected open error")
	}
}

// TestRecordExperiment_MkdirFails makes office-memory's parent a file so
// MkdirAll(filepath.Dir(path)) fails.
func TestRecordExperiment_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	jarvis := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(jarvis, 0o755); err != nil {
		t.Fatal(err)
	}
	// office-memory is a FILE → MkdirAll(office-memory) returns ENOTDIR.
	if err := os.WriteFile(filepath.Join(jarvis, "office-memory"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RecordExperiment(ExperimentRecord{TeamTemplate: "t"})
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

// TestRecordExperiment_OpenFails makes experiments.jsonl a directory so
// os.OpenFile fails.
func TestRecordExperiment_OpenFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// experiments.jsonl as a directory → OpenFile(O_WRONLY) fails (EISDIR).
	if err := os.MkdirAll(filepath.Join(dir, "experiments.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := RecordExperiment(ExperimentRecord{TeamTemplate: "t"})
	if err == nil {
		t.Fatal("expected OpenFile error when path is a directory")
	}
}
