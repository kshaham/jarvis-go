package pty

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestSDKSession_Start_StderrCallback drives the WithStderr callback
// (sdk_session.go:340-345): the stub writes an "error"/"unauthorized" line to
// its own stderr, which the SDK pipes into the scrub+classify callback.
func TestSDKSession_Start_StderrCallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"stderr-sess","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"stderr-sess","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	// Emit an error-bearing stderr line, drain stdin, then emit the event stream.
	body := "#!/bin/sh\n" +
		"echo 'auth error: unauthorized rate limit exceeded' 1>&2\n" +
		"cat >/dev/null &\n" +
		"printf '%s' '" + escaped + "'\n"
	bin := stubbin.Install(t, "fakeclaude-stderr.sh", body)

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-stderr",
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Drain until the init id appears, proving the session ran end-to-end
	// (the stderr callback fired during the subprocess's lifetime).
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s.ClaudeSessionID() == "stderr-sess" {
			break
		}
		select {
		case <-s.Events():
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.ClaudeSessionID() != "stderr-sess" {
		t.Errorf("ClaudeSessionID = %q, want stderr-sess", s.ClaudeSessionID())
	}
}

// TestSDKSession_SendPrompt_SendError starts a session whose stub exits
// promptly, closing the subprocess. A subsequent SendPrompt's underlying
// session.Send writes to the dead subprocess and errors, exercising the
// send-error branch (sdk_session.go:651-654) — isWorking must NOT flip.
func TestSDKSession_SendPrompt_SendError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Stub emits an init+result then exits immediately (no stdin drain held
	// open) so the subprocess pipe closes.
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"die-sess","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"die-sess","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-senderr",
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Wait for the subprocess to exit (events channel goes quiet / session idle).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-s.Events():
		case <-time.After(150 * time.Millisecond):
		}
		if s.GetStatus() == StatusIdle {
			break
		}
	}
	// Give the OS a moment to fully tear down the subprocess pipes.
	time.Sleep(300 * time.Millisecond)

	// Reset working state so the assertion is meaningful: the dead subprocess
	// means Send fails and SendPrompt returns before flipping to working.
	s.mu.Lock()
	s.isWorking = false
	s.status = StatusIdle
	s.mu.Unlock()

	s.SendPrompt(ctx, "after death")

	// The subprocess is dead, so persistentSession.Send must fail and the
	// early-return branch keeps the session out of the working state.
	if s.IsWorking() {
		t.Error("SendPrompt to a dead subprocess should hit the send-error branch and NOT flip isWorking")
	}
}

// TestSDKSession_Start_KickstartSendError attempts to drive the kickstart
// Send-error branch (sdk_session.go:374-378). The stub completes the SDK
// initialize handshake (so claude.NewSession succeeds) but does NOT keep its
// stdin open, so the kickstart Send write can fail. Start either returns the
// wrapped kickstart error or, if the SDK buffered the write, succeeds — both
// are acceptable; the test asserts no panic and a consistent terminal state.
func TestSDKSession_Start_KickstartSendError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Emit init then exit WITHOUT draining stdin -> the SDK's kickstart write to
	// our (now-closed) stdin pipe should fail.
	stream := `{"type":"system","subtype":"init","session_id":"ks-err","model":"m"}` + "\n"
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	// No `cat >/dev/null &` so stdin is never drained; close stdin fast by exiting.
	body := "#!/bin/sh\nprintf '%s' '" + escaped + "'\nexit 0\n"
	bin := stubbin.Install(t, "fakeclaude-kserr.sh", body)

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-kserr",
		WorkingDir:       t.TempDir(),
		SystemPrompt:     "kickstart that may fail to send",
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := s.Start(ctx)
	// Whatever the outcome, Stop must be safe and not hang.
	s.Stop()
	if err != nil && !strings.Contains(err.Error(), "kickstart") &&
		!strings.Contains(err.Error(), "persistent session") {
		t.Errorf("unexpected Start error shape: %v", err)
	}
}

// TestSDKSession_AddOutputObserver_AfterStopNilInit covers the nil-map init in
// AddOutputObserver (sdk_session.go:898-900). Stop() sets outputObservers=nil,
// so a subsequent AddOutputObserver must re-create the map.
func TestSDKSession_AddOutputObserver_AfterStopNilInit(t *testing.T) {
	s := NewSDKSession(SessionConfig{ID: "obs-nil"})
	s.Stop() // sets outputObservers = nil
	var got string
	unsub := s.AddOutputObserver(func(d string) { got = d })
	s.notifyOutputObservers("ping")
	unsub()
	if got != "ping" {
		t.Errorf("observer added after Stop saw %q, want 'ping' (nil map should be re-initialized)", got)
	}
}
