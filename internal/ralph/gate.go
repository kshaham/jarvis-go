package ralph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gateDefaultTimeout bounds how long the objective verify command may run
// before it is killed and treated as a failure. A verify command that hangs
// must not wedge the loop.
const gateDefaultTimeout = 10 * time.Minute

// gateOutputTailBytes caps how much of the verify command's combined output we
// retain as evidence. The tail is the useful part (test summary / first
// failure), and keeping the whole stream would bloat state files and events.
const gateOutputTailBytes = 4096

// GateResult is the outcome of one objective gate check. It is the ground-truth
// signal the loop-exit decision consults instead of the agent's self-asserted
// completion phrase.
type GateResult struct {
	// Pass is true only when the verify command exited 0.
	Pass bool `json:"pass"`
	// Skipped is true when no verify command could be determined for the
	// working directory. A skipped gate is NOT a pass -- callers decide whether
	// an un-gateable project may exit the loop, but they must do so knowingly.
	Skipped bool `json:"skipped"`
	// Command is the resolved verify command (for evidence / display).
	Command string `json:"command"`
	// ExitCode is the process exit code (-1 when the command could not start or
	// was killed by timeout).
	ExitCode int `json:"exitCode"`
	// Summary is the tail of combined stdout+stderr, capped at
	// gateOutputTailBytes.
	Summary string `json:"summary"`
	// TimedOut is true when the command was killed for exceeding the timeout.
	TimedOut bool `json:"timedOut"`
	// Err carries a non-execution error (command failed to start, etc.). A
	// non-zero exit from the verify command itself is reported via Pass=false +
	// ExitCode, not Err.
	Err string `json:"err,omitempty"`
	// CheckedAt records when the check ran.
	CheckedAt time.Time `json:"checkedAt"`
}

// ObjectiveGate is an objective verifier that can FAIL loop work without a human
// in the room. Unlike completion phrases or self-graded todo checkmarks, it runs
// a real command and returns its process exit code as ground truth. This is the
// loop-engineering "objective gate" -- the spine that prevents the Ralph Wiggum
// failure (a loop exiting on half-done work because the agent said "done").
type ObjectiveGate interface {
	// Check runs the verify command in workingDir and reports the result. It is
	// expected to be slow (it may run a test suite); callers must invoke it off
	// the hot path (e.g. in a goroutine), never while holding a tracker lock.
	Check(workingDir string) GateResult
}

// BasicCommandGate runs a single shell-free verify command and reports its exit
// code. When Command is empty it auto-detects an appropriate command from the
// working directory (go.mod -> go test, package.json -> npm test, etc.).
type BasicCommandGate struct {
	// Command is the explicit verify command, space-split into argv. When empty,
	// the gate auto-detects from the working directory at Check time.
	Command string
	// Timeout overrides gateDefaultTimeout when > 0.
	Timeout time.Duration
}

// NewBasicCommandGate builds a gate from an explicit command string (may be
// empty to enable auto-detection).
func NewBasicCommandGate(command string) *BasicCommandGate {
	return &BasicCommandGate{Command: command}
}

// Check implements ObjectiveGate.
func (g *BasicCommandGate) Check(workingDir string) GateResult {
	res := GateResult{CheckedAt: time.Now(), ExitCode: -1}

	argv := splitCommand(g.Command)
	if len(argv) == 0 {
		argv = DetectVerifyCommand(workingDir)
	}
	if len(argv) == 0 {
		res.Skipped = true
		res.Summary = "no verify command configured and none auto-detected"
		return res
	}
	res.Command = strings.Join(argv, " ")

	timeout := g.Timeout
	if timeout <= 0 {
		timeout = gateDefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	res.Summary = tailString(string(out), gateOutputTailBytes)

	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Err = "verify command timed out"
		return res
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
			return res
		}
		// Command could not start (binary missing, etc.). Treat as a skip rather
		// than a hard fail so a misconfigured verify command does not wedge every
		// loop -- but record the error so operators can see why.
		res.Skipped = true
		res.Err = err.Error()
		return res
	}
	res.Pass = true
	res.ExitCode = 0
	return res
}

// DetectVerifyCommand infers a sensible verify command from the files present in
// workingDir. Returns nil when no convention matches. Kept deliberately small --
// the explicit Config.VerifyCommand is the escape hatch for anything unusual.
func DetectVerifyCommand(workingDir string) []string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(workingDir, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return []string{"go", "test", "./...", "-count=1"}
	case exists("Cargo.toml"):
		return []string{"cargo", "test"}
	case exists("package.json"):
		return []string{"npm", "test", "--silent"}
	case exists("pyproject.toml"), exists("setup.py"), exists("pytest.ini"):
		return []string{"pytest", "-q"}
	case exists("Makefile"):
		return []string{"make", "test"}
	default:
		return nil
	}
}

// splitCommand splits a verify command string into argv on whitespace. It does
// NOT support shell quoting/operators -- a verify command is a single program
// invocation, not a shell pipeline. Configure a wrapper script for anything
// more complex.
func splitCommand(s string) []string {
	return strings.Fields(strings.TrimSpace(s))
}

// tailString returns the last max bytes of s, prefixed with an elision marker
// when truncated. Splitting on a rune boundary is not required for evidence
// display, but we avoid cutting mid-rune to keep the output valid UTF-8.
func tailString(s string, max int) string {
	if len(s) <= max {
		return strings.TrimSpace(s)
	}
	tail := s[len(s)-max:]
	// Advance to the next rune boundary so we don't emit a partial rune.
	for len(tail) > 0 && !utf8RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return "...(truncated)...\n" + strings.TrimSpace(tail)
}

// utf8RuneStart reports whether b is the first byte of a UTF-8 rune (i.e. not a
// continuation byte 0b10xxxxxx).
func utf8RuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
