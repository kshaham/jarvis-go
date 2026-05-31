package handler

// Shared stub-binary installers for handler tests that need a real SDK or
// Codex session lifecycle without a live Anthropic/OpenAI subprocess. The
// scripts impersonate `claude` (stream-json) and `codex exec --json`,
// emit a fixed event stream, then exit — closing stdout so the session's
// reader treats it as session end and closes the events channel.

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// skipIfNoStubShell skips on platforms where #!/bin/sh stubs won't run.
func skipIfNoStubShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
}

// installFakeClaudeStub installs a #!/bin/sh stub that impersonates the
// `claude` CLI in session/stream-json mode for the Go SDK. It drains stdin in
// the background (so the SDK's stdin writes never block), prints the supplied
// stream-json `payload`, then exits — closing stdout so the SDK reader ends
// the session.
func installFakeClaudeStub(t *testing.T, payload string) string {
	t.Helper()
	skipIfNoStubShell(t)
	escaped := strings.ReplaceAll(payload, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null &\nprintf '%s' '" + escaped + "'\n"
	return stubbin.Install(t, "fakeclaude.sh", body)
}

// installFakeCodexStub installs a #!/bin/sh stub that impersonates
// `codex exec --json`. It ignores its args/stdin and prints the supplied JSONL
// `stream`, then exits 0 (a clean turn).
func installFakeCodexStub(t *testing.T, stream string) string {
	t.Helper()
	skipIfNoStubShell(t)
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	body := "#!/bin/sh\nprintf '%s' '" + escaped + "'\n"
	return stubbin.Install(t, "codex.sh", body)
}
