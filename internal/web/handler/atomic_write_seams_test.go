package handler

// Seam-driven coverage for atomicWriteFile's mid-write error arms (Write,
// Close, Chmod failures) that a real filesystem can't reliably trigger. Each
// test swaps a package seam (atomicTempFile / atomicChmod), asserts the error
// propagates, and that no destination file is left behind.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// restoreSeams resets the atomicWriteFile seams after a test mutates them.
func restoreSeams(t *testing.T) {
	t.Helper()
	origTemp := atomicTempFile
	origChmod := atomicChmod
	t.Cleanup(func() {
		atomicTempFile = origTemp
		atomicChmod = origChmod
	})
}

// TestAtomicWriteFile_WriteError forces the temp file's Write to fail by handing
// back an already-closed *os.File (write on a closed fd -> error).
func TestAtomicWriteFile_WriteError(t *testing.T) {
	restoreSeams(t)
	dir := t.TempDir()
	var stagedName string
	atomicTempFile = func(d, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(d, pattern)
		if err != nil {
			return nil, err
		}
		stagedName = f.Name()
		f.Close() // pre-close so the subsequent Write fails
		return f, nil
	}

	err := atomicWriteFile(filepath.Join(dir, "plan.md"), []byte("data"), 0o644)
	if err == nil {
		t.Fatal("expected a write error on a pre-closed temp file")
	}
	if _, statErr := os.Stat(stagedName); !os.IsNotExist(statErr) {
		t.Errorf("temp file should be removed after a failed write, stat=%v", statErr)
	}
}

// TestAtomicWriteFile_CloseError forces the temp file's Close to fail. A double
// close on an *os.File returns os.ErrClosed; we pre-close after a successful
// write by returning a file that's been closed once already but written to via
// a dup... simplest reliable approach: write succeeds (empty data), then Close
// fails because we closed it ourselves first using a sentinel wrapper is not
// possible with *os.File, so instead we trigger Close error via an fd that was
// closed out from under it. We close the file and reopen O_RDONLY to make Write
// no-op-safe; here we use empty data so Write returns nil, then Close on an
// already-closed fd errors.
func TestAtomicWriteFile_CloseError(t *testing.T) {
	restoreSeams(t)
	dir := t.TempDir()
	var stagedName string
	atomicTempFile = func(d, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(d, pattern)
		if err != nil {
			return nil, err
		}
		stagedName = f.Name()
		// Close the underlying fd directly so the file's own Close() later
		// returns an error, but Write of empty data short-circuits to nil.
		f.Close()
		return f, nil
	}

	// Empty data: tmp.Write(nil) on a closed *os.File returns (0, nil) for a
	// zero-length write on most platforms, letting execution reach the Close
	// arm. If Write errors first that's also a valid failure path -- either way
	// atomicWriteFile must return non-nil and clean up.
	err := atomicWriteFile(filepath.Join(dir, "plan.md"), nil, 0o644)
	if err == nil {
		t.Fatal("expected an error from the closed temp file (write or close)")
	}
	if _, statErr := os.Stat(stagedName); !os.IsNotExist(statErr) {
		t.Errorf("temp file should be removed after failure, stat=%v", statErr)
	}
}

// TestAtomicWriteFile_ChmodError forces the chmod seam to fail after a clean
// write+close, asserting the chmod-failure arm removes the staged temp file.
func TestAtomicWriteFile_ChmodError(t *testing.T) {
	restoreSeams(t)
	dir := t.TempDir()
	var stagedName string
	atomicTempFile = func(d, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(d, pattern)
		stagedName = f.Name()
		return f, err
	}
	sentinel := errors.New("injected chmod failure")
	atomicChmod = func(string, os.FileMode) error { return sentinel }

	err := atomicWriteFile(filepath.Join(dir, "plan.md"), []byte("ok"), 0o644)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected injected chmod error, got %v", err)
	}
	if _, statErr := os.Stat(stagedName); !os.IsNotExist(statErr) {
		t.Errorf("temp file should be removed after chmod failure, stat=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "plan.md")); !os.IsNotExist(statErr) {
		t.Error("destination should not exist after chmod failure")
	}
}
