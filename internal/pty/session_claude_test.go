package pty

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// fakeClaudeInteractive installs a stub that mimics an interactive `claude`
// CLI: it echoes its args (so the test can assert the flags Start built),
// prints `banner` to stdout, then blocks reading stdin so the PTY stays
// alive for the read loop / Resize / WriteToPTY assertions. It exits when
// the PTY is closed (Stop) — `read` returns EOF.
func fakeClaudeInteractive(t *testing.T, banner string) string {
	t.Helper()
	escaped := strings.ReplaceAll(banner, "'", "'\\''")
	body := "#!/bin/sh\n" +
		"echo \"ARGS:$*\"\n" +
		"printf '%s\\n' '" + escaped + "'\n" +
		"while read line; do :; done\n"
	return stubbin.Install(t, "claude-interactive.sh", body)
}

// TestSession_ClaudeMode_StartBuildsFlags drives the ModeClaude branch of
// Session.Start with a stub claude binary, asserting the session spawns, the
// CLI flags Start assembled reach the binary, and the read loop captures the
// banner. This is the only path that covers the ModeClaude flag-assembly in
// Start (previously only ModeShell ran).
func TestSession_ClaudeMode_StartBuildsFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeClaudeInteractive(t, "CLAUDE_BANNER_OK")

	s := NewSession(SessionConfig{
		WorkingDir:       "/tmp",
		Mode:             ModeClaude,
		Name:             "claude-flags",
		Model:            "claude-opus-4-8",
		Effort:           "high",
		ClaudeBinaryPath: bin,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if s.PID() == 0 {
		t.Error("ModeClaude session should have a non-zero PID after Start")
	}

	// Capture output via observer until the banner + ARGS line appear.
	var sb safeBuf
	done := make(chan struct{})
	unsub := s.AddOutputObserver(func(d string) {
		sb.Write(d)
		if strings.Contains(sb.String(), "CLAUDE_BANNER_OK") {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	defer unsub()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("did not observe claude banner within 8s; got %q", sb.String())
	}

	out := sb.String()
	// Start should have forwarded --dangerously-skip-permissions, --model, and
	// --effort to the binary (echoed back via ARGS:).
	for _, want := range []string{"--dangerously-skip-permissions", "--model", "claude-opus-4-8", "--effort", "high"} {
		if !strings.Contains(out, want) {
			t.Errorf("claude args missing %q; got %q", want, out)
		}
	}
}

// TestSession_ClaudeMode_OmitSkipPermissions verifies the auth-session branch
// drops --dangerously-skip-permissions.
func TestSession_ClaudeMode_OmitSkipPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeClaudeInteractive(t, "AUTH_BANNER")
	s := NewSession(SessionConfig{
		WorkingDir:          "/tmp",
		Mode:                ModeClaude,
		Name:                "claude-auth",
		ClaudeBinaryPath:    bin,
		OmitSkipPermissions: true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	var sb safeBuf
	done := make(chan struct{})
	unsub := s.AddOutputObserver(func(d string) {
		sb.Write(d)
		if strings.Contains(sb.String(), "ARGS:") {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	defer unsub()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("did not observe ARGS line; got %q", sb.String())
	}
	if strings.Contains(sb.String(), "--dangerously-skip-permissions") {
		t.Errorf("auth session must omit --dangerously-skip-permissions; got %q", sb.String())
	}
}

// TestSession_ClaudeMode_AllFlagsForwarded exercises the remaining
// conditional flag branches in Start (fallback model, from-pr, chrome,
// channels, strict mcp, exclude dynamic sections, system prompt files).
func TestSession_ClaudeMode_AllFlagsForwarded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeClaudeInteractive(t, "ALLFLAGS")
	dir := t.TempDir()
	s := NewSession(SessionConfig{
		WorkingDir:                         "/tmp",
		Mode:                               ModeClaude,
		ClaudeBinaryPath:                   bin,
		Model:                              "primary-model",
		FallbackModel:                      "fallback-model",
		FromPRNumber:                       42,
		ChromeEnabled:                      true,
		StrictMcpConfig:                    true,
		ExcludeDynamicSystemPromptSections: true,
		SystemPromptFile:                   dir + "/sys.txt",
		AppendSystemPromptFile:             dir + "/append.txt",
		Channels:                           []string{"plugin:office@local"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	var sb safeBuf
	done := make(chan struct{})
	unsub := s.AddOutputObserver(func(d string) {
		sb.Write(d)
		if strings.Contains(sb.String(), "ALLFLAGS") {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	defer unsub()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("did not observe banner; got %q", sb.String())
	}
	out := sb.String()
	for _, want := range []string{
		"--fallback-model", "fallback-model",
		"--from-pr", "42",
		"--chrome",
		"--strict-mcp-config",
		"--exclude-dynamic-system-prompt-sections",
		"--system-prompt-file",
		"--append-system-prompt-file",
		"--channels", "plugin:office@local",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected flag %q in claude args; got %q", want, out)
		}
	}
}

// TestSession_ClaudeMode_DetectAndAutoRespondMenu_LivePTY drives the
// matched-menu branch of detectAndAutoRespondMenu against a live PTY so the
// auto-response goroutine actually writes "1"+CR through writeIfPtyFileMatches.
func TestSession_ClaudeMode_DetectAndAutoRespondMenu_LivePTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeClaudeInteractive(t, "MENU_READY")
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

	// Wait for the PTY to be live (banner observed) so ptyFile is set.
	var sb safeBuf
	ready := make(chan struct{})
	unsub := s.AddOutputObserver(func(d string) {
		sb.Write(d)
		if strings.Contains(sb.String(), "MENU_READY") {
			select {
			case <-ready:
			default:
				close(ready)
			}
		}
	})
	defer unsub()
	select {
	case <-ready:
	case <-time.After(8 * time.Second):
		t.Fatalf("PTY not ready; got %q", sb.String())
	}

	// Feed a full menu signature directly to detectAndAutoRespondMenu. With a
	// live ptyFile + sessionCtx the matched branch spawns the writer goroutine.
	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")

	// lastAutoResponse must be set (proves the matched branch ran).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		set := !s.lastAutoResponse.IsZero()
		s.mu.RUnlock()
		if set {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.RLock()
	set := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !set {
		t.Error("a matched menu on a live PTY should set lastAutoResponse and write the auto-response")
	}
	// Give the writer goroutine time to write "1"+CR through the PTY before
	// the test tears down (covers writeIfPtyFileMatches success path).
	time.Sleep(900 * time.Millisecond)
}

// TestSession_WriteInkPrompt_LivePTY drives the full WriteInkPrompt chunk +
// CR-retry path on a live shell PTY (the 15.8%-covered function). It is slow
// (~4.4s of real sleeps inside WriteInkPrompt) but the only way to exercise
// the chunked write loop end-to-end.
func TestSession_WriteInkPrompt_LivePTY(t *testing.T) {
	if testing.Short() {
		t.Skip("WriteInkPrompt has multi-second real-time sleeps; skipped in -short")
	}
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, Name: "ink"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// A prompt longer than inkChunkSize would exercise multi-chunk; keep it
	// short but real. Returns true because the live PTY accepts the writes.
	if !s.WriteInkPrompt("echo INK_DONE") {
		t.Error("WriteInkPrompt on a live PTY should return true")
	}
}
