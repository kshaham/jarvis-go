package office

// Headroom MCP integration.
//
// Headroom (https://github.com/chopratejas/headroom) is a local-first
// context-compression layer for AI agents. Its MCP server, run via
// `headroom mcp serve`, exposes three tools agents can call:
//
//   - mcp__headroom__headroom_compress  -- compress a blob (tool output,
//     log, search result, file) before it lands in context
//   - mcp__headroom__headroom_retrieve  -- reversible: fetch the original
//     of a previously compressed blob (CCR; originals stored locally)
//   - mcp__headroom__headroom_stats     -- per-session token-savings stats
//
// This is complementary to CodeGraphContext: CGC reduces what an agent
// READS from the codebase; Headroom compresses the tool outputs, logs,
// and conversation that still flow into context on long perpetual runs.
//
// Gating: same graceful-degrade posture as PageIndex/codegraph --
// HeadroomAvailable() caches a one-shot exec.LookPath + a `headroom
// --version` probe. Spawning agents skip the MCP entry entirely when
// unavailable; no error, no degradation for the rest of the pipeline.

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// headroomProbeTimeout caps the availability probe so a slow/hung
// `headroom --version` can't block daemon startup.
const headroomProbeTimeout = 15 * time.Second

// Availability is memoized behind a mutex (not sync.Once) so the test seam
// resetHeadroomCacheForTest can clear it without a data race: the idle-monitor
// goroutine reads HeadroomAvailable() every tick (via maybeRunHeadroomLearn),
// and resetting a sync.Once value concurrently with that read is a race the
// detector flags. All access to the two fields below goes through headroomMu.
var (
	headroomMu        sync.Mutex
	headroomProbed    bool // set true ONLY after a successful detection (cached permanently)
	headroomAvailable bool
	headroomLoggedNeg bool // suppress repeated "unavailable" logs while a negative result retries
)

// HeadroomAvailable reports whether the Headroom MCP can be wired into
// spawned agents. Returns true when `headroom` is on PATH AND
// `headroom --version` succeeds. Memoized after the first call; logged
// once for operator diagnostics (same intent as PageIndexAvailable).
//
// Returning false means agents simply won't get the mcp__headroom__*
// tool surface; the rest of jarvis runs unchanged.
func HeadroomAvailable() bool {
	headroomMu.Lock()
	defer headroomMu.Unlock()
	if headroomProbed {
		return headroomAvailable
	}
	// Only a SUCCESSFUL detection is cached (headroomProbed=true below). A
	// negative result is NOT cached, so a transient failure -- the binary not
	// yet on PATH during container startup, an installer mid-update, a flaky
	// `--version` -- self-heals on the next call instead of disabling Headroom
	// for the whole process lifetime. The retry cost is just a cheap PATH lookup
	// (and a subprocess only in the rare on-PATH-but-failing case); the repeated
	// log is suppressed after the first negative via headroomLoggedNeg.
	logNeg := func(msg string, args ...any) {
		if !headroomLoggedNeg {
			headroomLoggedNeg = true
			slog.Info(msg, args...)
		}
	}
	if _, err := exec.LookPath("headroom"); err != nil {
		logNeg("Headroom MCP skipped: `headroom` not on PATH")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), headroomProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "headroom", "--version")
	if out, err := cmd.CombinedOutput(); err != nil {
		logNeg("Headroom MCP probe failed; agents will not get mcp__headroom__* tools",
			"err", err, "output", truncateProbeOutput(string(out)))
		return false
	}
	headroomProbed = true
	headroomAvailable = true
	// Don't call HeadroomHome() here -- it does filesystem I/O (UserHomeDir +
	// MkdirAll) and this runs under headroomMu, which the idle monitor contends
	// on every tick. The CCR home is logged lazily by HeadroomMcpConfig instead.
	slog.Info("Headroom MCP detected; agents will get mcp__headroom__* tools")
	return true
}

// HeadroomHome returns the on-disk root for Headroom's reversible
// compression store (CCR originals), shared across a team's agents so a
// teammate can retrieve another agent's compressed original. Returns "" when
// the home directory can't be resolved or created (matching snapshotDir /
// WrapUpReportsDir) so callers fall back to Headroom's own defaults rather
// than a cwd-relative path.
func HeadroomHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".jarvis", "headroom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// HeadroomMcpConfig returns the stdio MCP server config to inject into a
// spawned agent's mcpServers map. Mirrors the codegraph + pageindex shape
// in internal/web/handler/spawn_agent.go. Returns nil when Headroom isn't
// available -- the caller should check HeadroomAvailable() first and skip
// the map entry rather than passing nil into the SDK.
func HeadroomMcpConfig() map[string]any {
	if !HeadroomAvailable() {
		return nil
	}
	cfg := map[string]any{
		"type":    "stdio",
		"command": "headroom",
		"args":    []string{"mcp", "serve"},
	}
	// Best-effort: point the CCR store at a shared per-jarvis directory so
	// originals persist and dedup across a team's agents. Only set it when we
	// have a real absolute path -- otherwise let Headroom use its own default
	// rather than exporting a cwd-relative HEADROOM_HOME.
	if home := HeadroomHome(); home != "" {
		cfg["env"] = map[string]string{"HEADROOM_HOME": home}
	} else {
		// HeadroomHome() == "" means UserHomeDir/MkdirAll failed -- a real (if
		// rare) FS problem. Surface it so the silent fallback to Headroom's own
		// default store isn't mistaken for the intended shared-CCR config.
		slog.Warn("Headroom CCR store: shared HEADROOM_HOME unavailable; using Headroom's default",
			"reason", "could not resolve or create ~/.jarvis/headroom")
	}
	return cfg
}

// resetHeadroomCacheForTest is the test-only seam mirroring
// resetPageIndexCacheForTest. Production code must not call this.
func resetHeadroomCacheForTest() {
	headroomMu.Lock()
	defer headroomMu.Unlock()
	headroomProbed = false
	headroomAvailable = false
	headroomLoggedNeg = false
}
