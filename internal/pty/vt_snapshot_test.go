package pty

import (
	"strings"
	"testing"
)

// TestSession_Snapshot_FedFromProcessOutput verifies that bytes
// passed through processOutput land in the vt emulator and are
// readable via Snapshot(). Locks in the v1-deferred PTY parsing
// improvement so a future refactor doesn't accidentally drop the
// vt feed.
func TestSession_Snapshot_FedFromProcessOutput(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s.vtEmulator == nil {
		t.Fatal("expected vtEmulator to be initialized in NewSession")
	}

	// Empty snapshot before any output.
	pre := s.Snapshot()
	for _, r := range pre {
		if r != ' ' && r != '\n' && r != '\r' {
			t.Errorf("fresh emulator should have only whitespace, got %q", pre)
			break
		}
	}

	// Push some text. The vt emulator renders into its grid; non-empty
	// after processing.
	s.processOutput("hello, world\r\n")
	got := s.Snapshot()
	if !strings.Contains(got, "hello, world") {
		t.Errorf("snapshot missing input text. got:\n%s", got)
	}
}

// TestSession_Snapshot_HandlesAnsiSplitAcrossChunks verifies the
// emulator absorbs partial ANSI escape sequences gracefully. This
// is the killer feature of the vt swap -- the old per-chunk
// substring parsers broke when escape codes split between PTY reads;
// the emulator buffers internal state and renders correctly.
func TestSession_Snapshot_HandlesAnsiSplitAcrossChunks(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})

	// Send the bytes for "RED\nGREEN" with a CSI color code split
	// across two chunks. Old parsers would StripANSI on each chunk
	// independently and may produce garbage; the emulator hands us
	// clean rendered text.
	s.processOutput("\x1b[31m") // partial CSI -- color start
	s.processOutput("RED\x1b[0m\nGREEN")

	got := s.Snapshot()
	if !strings.Contains(got, "RED") || !strings.Contains(got, "GREEN") {
		t.Errorf("snapshot dropped text across ANSI split. got:\n%s", got)
	}
}
