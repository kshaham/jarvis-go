// Package stubbin installs throwaway shell-script "binaries" into
// test-managed temp directories so unit tests can stub external CLI
// dependencies (claude, codegraph, etc.) without touching the host's
// real binaries.
//
// Why a dedicated package: the same 10-line pattern (write a #!/bin/sh
// script + chmod +x + register cleanup) had been copy-pasted across
// five test files in three packages. P7.4.13 audit fix.
//
// Constraints kept intentionally narrow:
//   - Unix-only. Windows tests using these helpers must `t.Skip` first;
//     each consumer already does so the package itself doesn't check.
//   - No deps beyond stdlib `os`, `path/filepath`, `testing`. Safe to
//     import from any test package without creating cycles.
package stubbin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Install writes `body` to a fresh executable file inside a t.TempDir
// and returns its absolute path. The caller swaps the package-level
// `binary` variable under test to this path (and registers a cleanup
// to restore). The script body should start with a #! line and
// produce whatever output the test expects on stdout/stderr.
func Install(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("stubbin.Install: write %s: %v", path, err)
	}
	return path
}

// EchoStub returns a script that prints `output` to stdout verbatim
// and exits 0. The most common form — used wherever a test needs the
// CLI to emit a fixed JSON payload or text response. Single-quotes in
// `output` are escaped to survive the shell's single-quoted string
// literal.
func EchoStub(t *testing.T, output string) string {
	t.Helper()
	escaped := strings.ReplaceAll(output, "'", "'\\''")
	body := "#!/bin/sh\nprintf %s '" + escaped + "'\n"
	return Install(t, "stub-echo.sh", body)
}

// EchoStubReadStdin is like EchoStub but reads-and-discards stdin
// before emitting output. Useful for stubs that mimic the `claude`
// CLI's habit of consuming the prompt from stdin before responding.
func EchoStubReadStdin(t *testing.T, output string) string {
	t.Helper()
	escaped := strings.ReplaceAll(output, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null\nprintf %s '" + escaped + "'\n"
	return Install(t, "stub-echo-stdin.sh", body)
}

// SleepStub returns a script that sleeps for `seconds` then exits 0.
// Used to drive timeout-path tests.
func SleepStub(t *testing.T, seconds int) string {
	t.Helper()
	body := "#!/bin/sh\nsleep " + itoa(seconds) + "\n"
	return Install(t, "stub-sleep.sh", body)
}

// FailStub returns a script that prints `stderr` to stderr and exits
// `code`. Drives error-path tests.
func FailStub(t *testing.T, code int, stderrMsg string) string {
	t.Helper()
	escaped := strings.ReplaceAll(stderrMsg, "'", "'\\''")
	body := "#!/bin/sh\nprintf %s '" + escaped + "' 1>&2\nexit " + itoa(code) + "\n"
	return Install(t, "stub-fail.sh", body)
}

// itoa is a tiny strconv.Itoa to avoid importing strconv (keeps the
// package's dep surface minimal — purely cosmetic).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
