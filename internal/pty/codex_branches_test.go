package pty

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// errAfterReader yields `data` once, then returns a non-EOF error. Used to
// drive consumeJSONL's scanner-error drain branch.
type errAfterReader struct {
	data []byte
	done bool
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("synthetic mid-stream read error")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		r.done = true
	}
	return n, nil
}

// TestCodexConsumeJSONL_ScanErrorDrains feeds a single line larger than the 4MB
// scanner token cap so sc.Scan stops with bufio.ErrTooLong; consumeJSONL must
// then drain the rest of the reader (codex_session.go:440-443) and return
// sawCompleted=false without hanging.
func TestCodexConsumeJSONL_ScanErrorDrains(t *testing.T) {
	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	// 5MB single line with no newline -> exceeds the 4MB buffer cap.
	huge := strings.Repeat("a", 5*1024*1024)
	// Append trailing bytes after the (unterminated) huge token so there is
	// something to drain after the scan error.
	r := &errAfterReader{data: []byte(huge)}
	if s.consumeJSONL(r) {
		t.Error("an oversized line should not register a completed turn")
	}
}

// TestCodexConsumeJSONL_ReadErrorDrains drives the scan-error branch via a
// reader that errors mid-stream after a valid line.
func TestCodexConsumeJSONL_ReadErrorDrains(t *testing.T) {
	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	// A valid line then the reader errors (no newline terminator on the error).
	r := &errAfterReader{data: []byte(`{"type":"turn.started"}` + "\n" + "partial-no-newline")}
	// turn.started sets working but no turn.completed -> sawCompleted=false.
	if s.consumeJSONL(r) {
		t.Error("a mid-stream read error without turn.completed should yield false")
	}
}

// TestCodexSession_Worker_ParentCancelEmitsExit cancels the PARENT context
// (not Stop) so the worker goroutine returns with isStopped=false and its defer
// emits EventExit (codex_session.go:167-169).
func TestCodexSession_Worker_ParentCancelEmitsExit(t *testing.T) {
	s := NewCodexSession(SessionConfig{ID: "worker-exit", Model: "gpt-5.5"})
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Cancel the parent ctx; the worker observes ctx.Done() and returns. Because
	// Stop() was never called, isStopped stays false and the defer emits Exit.
	cancel()

	var sawExit bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawExit {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventExit); ok {
				sawExit = true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !sawExit {
		t.Error("a parent-context cancel (no Stop) should make the worker emit EventExit on exit")
	}
}

// TestCodexSession_RunTurn_StderrErrorClassified runs a codex turn whose stub
// writes an "error"-bearing line to stderr, exercising the stderr scanner's
// error-classification branch (codex_session.go:268-275).
func TestCodexSession_RunTurn_StderrErrorClassified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Stub: emit one stderr line containing "error" + "unauthorized", then a
	// clean completed turn on stdout.
	body := "#!/bin/sh\n" +
		"echo 'fatal error: unauthorized rate limit' 1>&2\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"th-stderr\"}'\n" +
		"printf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	bin := stubbin.Install(t, "codex-stderr.sh", body)

	s := NewCodexSession(SessionConfig{
		ID:              "codex-stderr",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "go")

	// Wait for the turn's EventTokens (proves the turn ran and stderr was drained).
	var sawTokens bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawTokens {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventTokens); ok {
				sawTokens = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawTokens {
		t.Error("expected the turn to complete with EventTokens despite stderr noise")
	}
}

// TestCodexSession_RunTurn_StderrTooLongDrains runs a turn whose stub emits a
// >1MB single stderr line, tripping the stderr scanner's ErrTooLong path and
// the io.Copy drain (codex_session.go:281-283). The turn still completes.
func TestCodexSession_RunTurn_StderrTooLongDrains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Emit ~2MB of 'x' to stderr with no newline (single oversized token), then
	// a clean stdout turn. yes|head builds the bytes without a giant literal.
	body := "#!/bin/sh\n" +
		"head -c 2000000 /dev/zero | tr '\\0' 'x' 1>&2\n" +
		"printf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	bin := stubbin.Install(t, "codex-bigstderr.sh", body)

	s := NewCodexSession(SessionConfig{
		ID:              "codex-bigstderr",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "go")

	var sawTokens bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !sawTokens {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventTokens); ok {
				sawTokens = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawTokens {
		t.Error("the turn should complete even when stderr emits an oversized line")
	}
}

// TestCodexSession_RunTurn_StderrBenignClassified runs a turn whose stub
// writes a benign (no error/unauthorized/rate keyword) stderr line, exercising
// the Debug else-branch of the stderr classifier (codex_session.go:273-275).
func TestCodexSession_RunTurn_StderrBenignClassified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	body := "#!/bin/sh\n" +
		"echo 'just a progress note, nothing alarming' 1>&2\n" +
		"printf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	bin := stubbin.Install(t, "codex-benign-stderr.sh", body)

	s := NewCodexSession(SessionConfig{
		ID:              "codex-benign",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "go")

	var sawTokens bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawTokens {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventTokens); ok {
				sawTokens = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawTokens {
		t.Error("the turn should complete; benign stderr goes to the Debug branch")
	}
}

// TestCodexSession_Stop_RemoveCodexHomeError makes CODEX_HOME a path whose
// parent is read-only so os.RemoveAll fails inside Stop, exercising the warn
// branch (codex_session.go:616-618) without panicking.
func TestCodexSession_Stop_RemoveCodexHomeError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix permissions and a non-root user")
	}
	parent := t.TempDir()
	home := parent + "/home"
	if err := os.MkdirAll(home+"/sub", 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Make `home` itself non-writable AND non-executable so RemoveAll can't
	// traverse/unlink its child -> RemoveAll returns an error.
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatalf("chmod home: %v", err)
	}
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o755)
		_ = os.Chmod(home, 0o755)
	})

	s := NewCodexSession(SessionConfig{ID: "codex-rmfail", CodexHome: home})
	// Stop without Start: cancelFunc is nil, worker never ran, so Stop reaches
	// the RemoveAll(codexHome) call directly.
	s.Stop() // must not panic despite the RemoveAll error
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped", s.GetStatus())
	}
}

// TestCodexSession_AddOutputObserver_NilMapInit covers the defensive nil-map
// init in AddOutputObserver (codex_session.go:699-701) by forcing the map nil.
func TestCodexSession_AddOutputObserver_NilMapInit(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.observerMu.Lock()
	s.outputObservers = nil // simulate a post-Stop / hand-built session
	s.observerMu.Unlock()

	var got string
	unsub := s.AddOutputObserver(func(d string) { got = d })
	s.notifyOutputObservers("hello")
	unsub()
	if got != "hello" {
		t.Errorf("observer added after a nil map saw %q, want 'hello'", got)
	}
}
