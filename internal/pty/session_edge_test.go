package pty

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestSession_ProcessOutput_PastedTextAutoSubmit drives the ModeClaude
// "[Pasted text" auto-submit branch of processOutput against a live PTY so the
// auto-submit goroutine writes a CR through the PTY (the previously-uncovered
// branch). Uses a stub claude binary so the PTY stays alive.
func TestSession_ProcessOutput_PastedTextAutoSubmit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// A stub that just blocks reading stdin keeps the PTY alive.
	bin := stubbin.Install(t, "claude-block.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{
		WorkingDir:       "/tmp",
		Mode:             ModeClaude,
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Wait until the PTY file is set (Start sets it synchronously, but give the
	// process a beat to be fully up).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		live := s.ptyFile != nil
		s.mu.RUnlock()
		if live {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Feed output containing the pasted-text marker. processOutput should arm
	// the auto-submit goroutine (lastAutoResponse set, CR written ~1s later).
	s.processOutput("[Pasted text #1 +50 lines]")

	s.mu.RLock()
	set := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !set {
		t.Error("a '[Pasted text' marker in Claude mode should arm the auto-submit (set lastAutoResponse)")
	}
	// Let the 1s-delayed goroutine run its Write before teardown.
	time.Sleep(1200 * time.Millisecond)
}

// TestResolveAndValidateWorkingDir_TildeExpansion covers the ~/ expansion
// branch (HOME prefix rewrite) of ResolveAndValidateWorkingDir.
func TestResolveAndValidateWorkingDir_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir available")
	}
	// Make a subdir under HOME so the resolved path exists and is a directory.
	sub, err := os.MkdirTemp(home, "jarvis-wd-test-")
	if err != nil {
		t.Skipf("cannot create tempdir under home: %v", err)
	}
	defer os.RemoveAll(sub)

	base := filepath.Base(sub)
	resolved, err := ResolveAndValidateWorkingDir("~/"+base, nil)
	if err != nil {
		t.Fatalf("tilde-expanded path should resolve: %v", err)
	}
	// resolved should be the symlink-evaluated form of sub.
	want, _ := filepath.EvalSymlinks(sub)
	if resolved != want {
		t.Errorf("resolved = %q, want %q (tilde expanded + symlinks resolved)", resolved, want)
	}
}

// TestResolveAndValidateWorkingDir_SkipsEmptyAllowlistEntries ensures empty
// prefix entries are skipped without short-circuiting the allowlist check.
func TestResolveAndValidateWorkingDir_SkipsEmptyAllowlistEntries(t *testing.T) {
	tmp := t.TempDir()
	// allowlist has an empty entry (skipped) followed by the real tmp dir.
	resolved, err := ResolveAndValidateWorkingDir(tmp, []string{"", tmp})
	if err != nil {
		t.Fatalf("expected success with a real entry after an empty one: %v", err)
	}
	if resolved == "" {
		t.Error("expected a non-empty resolved path")
	}
}

// TestSession_ProcessOutput_PastedText_Debounced verifies a second pasted-text
// marker within the 3s debounce window does NOT re-arm the auto-submit.
func TestSession_ProcessOutput_PastedText_Debounced(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// Pre-set a recent lastAutoResponse to land inside the 3s window.
	s.mu.Lock()
	s.lastAutoResponse = time.Now()
	prev := s.lastAutoResponse
	s.mu.Unlock()
	// No live PTY, so even if it matched it couldn't write; the assertion is
	// purely on the debounce guard leaving lastAutoResponse unchanged.
	s.processOutput("[Pasted text #2]")
	s.mu.RLock()
	now := s.lastAutoResponse
	s.mu.RUnlock()
	if !now.Equal(prev) {
		t.Error("a pasted-text marker inside the 3s debounce window must not re-arm auto-submit")
	}
}

// TestSession_ProcessOutput_ShellModeNoPastedSubmit confirms the pasted-text
// auto-submit is Claude-mode only (shell sessions never arm it).
func TestSession_ProcessOutput_ShellModeNoPastedSubmit(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.processOutput("[Pasted text #3]")
	if !s.lastAutoResponse.IsZero() {
		t.Error("shell mode must not arm pasted-text auto-submit")
	}
	// The text should still have been buffered.
	if !strings.Contains(s.TerminalBuffer(), "Pasted text") {
		t.Errorf("buffer = %q, want the raw output appended", s.TerminalBuffer())
	}
}
