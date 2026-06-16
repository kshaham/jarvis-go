package office

// headroom_learn.go — periodic failure-mining sweep that turns the team's
// failed/aborted agent sessions into durable corrections in the project's
// CLAUDE.md / AGENTS.md, via `headroom learn`.
//
// This complements memify (internal/library): memify clusters COMPLETED,
// high-outcome tasks into reusable workflows; `headroom learn` mines
// FAILED sessions for root-cause corrections (wrong file paths, bad search
// scope, command pitfalls, large-file reads). The two write to different
// sinks and don't overlap: memify -> ~/.claude/skills + library; headroom
// learn -> a marker-delimited "## Headroom Learned Patterns" section of the
// project's CLAUDE.md (idempotent; never clobbers manual edits).
//
// Because `--apply` writes into the operator's repo, the whole sweep is
// OPT-IN (default off) and gated on HeadroomAvailable(). The idle-monitor
// tick calls maybeRunHeadroomLearn once per tick; a wall-clock gate keeps
// the subprocess to a few runs a day.

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// headroomLearnInterval is the minimum wall-clock spacing between learn
// sweeps. Matches memifyInterval — failure corrections don't need to be
// fresher than a few times a day, and the subprocess parses session logs.
const headroomLearnInterval = 6 * time.Hour

// headroomLearnTimeout caps the `headroom learn` subprocess so a hung run
// can't leak a process or block the goroutine forever.
const headroomLearnTimeout = 5 * time.Minute

// HeadroomLearnResult is the parsed outcome of one learn sweep.
type HeadroomLearnResult struct {
	Output  string // tail of combined output (for logging/broadcast)
	Applied bool   // whether --apply was passed (corrections written)
}

// headroomLearnRunner is the seam that actually runs `headroom learn`.
// Overridable in tests so the sweep logic is exercisable without the
// `headroom` binary or touching a real CLAUDE.md.
var headroomLearnRunner = runHeadroomLearnCmd

// SetHeadroomLearnEnabled toggles the opt-in failure-mining sweep. Off by
// default because it writes corrections into the project's CLAUDE.md.
func (m *Manager) SetHeadroomLearnEnabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.headroomLearnEnabled = v
}

// IsHeadroomLearnEnabled reports whether the sweep is enabled.
func (m *Manager) IsHeadroomLearnEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.headroomLearnEnabled
}

// maybeRunHeadroomLearn runs a `headroom learn` sweep when enabled, the
// binary is present, a working directory is known, and the gate timer has
// expired. Safe to call every idle tick. Runs the subprocess in a safego
// goroutine so a panic/hang can't take down the monitor.
func (m *Manager) maybeRunHeadroomLearn() {
	if !HeadroomAvailable() {
		return
	}
	m.mu.Lock()
	if !m.headroomLearnEnabled {
		m.mu.Unlock()
		return
	}
	due := m.headroomLearnLastRunAt.IsZero() || time.Since(m.headroomLearnLastRunAt) >= headroomLearnInterval
	if !due {
		m.mu.Unlock()
		return
	}
	workingDir := m.lastWorkingDir
	if workingDir == "" {
		m.mu.Unlock()
		return
	}
	m.headroomLearnLastRunAt = time.Now()
	m.mu.Unlock()

	safego.Go("office.headroomLearn", func() { m.runHeadroomLearnTick(workingDir) })
}

// runHeadroomLearnTick is the actual work, separated so tests can call it
// directly without poking the gate timer.
func (m *Manager) runHeadroomLearnTick(workingDir string) {
	res, err := headroomLearnRunner(workingDir)
	if err != nil {
		slog.Warn("headroom learn sweep failed", "err", err, "dir", workingDir)
		return
	}
	slog.Info("headroom learn sweep completed", "dir", workingDir, "applied", res.Applied)
	m.broadcast("office:headroomLearnRan", map[string]interface{}{
		"workingDir": workingDir,
		"applied":    res.Applied,
		"summary":    res.Output,
		"timestamp":  time.Now().UnixMilli(),
	})
}

// runHeadroomLearnCmd is the production runner: `headroom learn --project
// <dir> --apply`. --apply writes the marker-delimited corrections section
// into the project's CLAUDE.md (idempotent). Default headroom behavior is
// dry-run; we apply because the whole sweep is already opt-in.
func runHeadroomLearnCmd(workingDir string) (HeadroomLearnResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), headroomLearnTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "headroom", "learn", "--project", workingDir, "--apply")
	out, err := cmd.CombinedOutput()
	res := HeadroomLearnResult{Output: tailLearnOutput(string(out)), Applied: true}
	if err != nil {
		return res, err
	}
	return res, nil
}

// tailLearnOutput trims the captured output to a bounded tail for
// logging/broadcast.
func tailLearnOutput(s string) string {
	const max = 2000
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return "…(truncated)…\n" + s[len(s)-max:]
}
