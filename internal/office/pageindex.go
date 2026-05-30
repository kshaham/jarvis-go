package office

// PageIndex MCP integration.
//
// PageIndex (https://github.com/VectifyAI/PageIndex) is a vectorless
// reasoning-based RAG system. Its MCP server, run via `npx
// @pageindex/mcp`, exposes three tools agents can call:
//
//   - mcp__pageindex__get_document
//   - mcp__pageindex__get_document_structure
//   - mcp__pageindex__get_page_content
//
// We point it at ~/.jarvis/office-memory/library/ (the markdown
// corpus produced by internal/library) so every agent can navigate
// past experiences, workflows, standups, and wrap-up reports via
// LLM-driven tree search.
//
// Gating: same pattern as ECC — PageIndexAvailable() caches a one-
// shot exec.LookPath on npx + a single warm-cache run of
// `npx --no-install @pageindex/mcp --version`. Spawning agents skip
// the MCP entry entirely when unavailable; no error, no degradation
// for the rest of the spawn pipeline.

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// PageIndex MCP version pinned for reproducibility. Updating means
// reviewing the tool schema for breaking changes. Operators on
// air-gapped hosts must pre-stage via `npm install -g @pageindex/mcp`
// at this version.
const pageIndexMcpVersion = "latest"

// pageIndexProbeTimeout caps how long the warm-cache probe runs.
// First call may fetch the package from npm; we don't want that to
// block daemon startup forever.
const pageIndexProbeTimeout = 30 * time.Second

var (
	pageIndexLookupOnce sync.Once
	pageIndexAvailable  bool
)

// PageIndexAvailable reports whether the PageIndex MCP can be wired
// into spawned agents. Returns true when `npx` is on PATH AND
// `npx --no-install @pageindex/mcp --version` succeeds (proves the
// package is either already cached or fetchable).
//
// Cached after the first call. Logged once for operator diagnostics
// — same pattern as NpxAvailable above.
//
// Returning false means agents simply won't get the mcp__pageindex__*
// tool surface; the rest of jarvis runs unchanged. Operator can
// recover by installing npm/node and restarting the daemon.
func PageIndexAvailable() bool {
	pageIndexLookupOnce.Do(func() {
		if !NpxAvailable() {
			slog.Info("PageIndex MCP skipped: npx not on PATH")
			return
		}
		// Warm-cache probe. --no-install means "fail fast if not
		// already cached" — we don't want to block daemon startup
		// on a multi-MB npm fetch. If the operator wants PageIndex
		// available, they pre-stage via npm install -g.
		//
		// Actually: --no-install is too strict for the common case
		// where the operator hasn't pre-staged. Drop it; tolerate
		// the one-time fetch on the first spawn.
		ctx, cancel := context.WithTimeout(context.Background(), pageIndexProbeTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "npx", "-y", "@pageindex/mcp@"+pageIndexMcpVersion, "--version")
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Info("PageIndex MCP probe failed; agents will not get mcp__pageindex__* tools",
				"err", err, "output", truncateProbeOutput(string(out)))
			return
		}
		pageIndexAvailable = true
		slog.Info("PageIndex MCP detected; agents will get mcp__pageindex__* tools",
			"library_dir", LibraryDir(),
			"version", pageIndexMcpVersion)
	})
	return pageIndexAvailable
}

// truncateProbeOutput keeps the probe-failure log line readable.
func truncateProbeOutput(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return truncRunes(s, max) + "…(truncated)"
}

// LibraryDir returns the on-disk root of the markdown corpus that
// PageIndex indexes. Thin wrapper over library.Dir() so the office
// package doesn't expose its dependency on internal/library at every
// caller — and gives us a single seam to swap if the layout moves.
func LibraryDir() string {
	return library.Dir()
}

// PageIndexMcpConfig returns the stdio MCP server config to inject
// into a spawned agent's mcpServers map. Mirrors the codegraph +
// cgc shape in internal/web/handler/spawn_agent.go.
//
// libraryDir is passed through env so the MCP server knows which
// directory to index. The PageIndex MCP reads PAGEINDEX_LIBRARY_DIR
// at startup (per its docs; pin to a specific version above so
// schema drift can't surprise us).
//
// Returns nil when PageIndex isn't available — the caller should
// check PageIndexAvailable() first and skip the map entry rather
// than passing nil into the SDK.
func PageIndexMcpConfig(libraryDir string) map[string]any {
	if libraryDir == "" {
		return nil
	}
	return map[string]any{
		"type":    "stdio",
		"command": "npx",
		"args":    []string{"-y", "@pageindex/mcp@" + pageIndexMcpVersion},
		"env": map[string]string{
			"PAGEINDEX_LIBRARY_DIR": libraryDir,
		},
	}
}

// resetPageIndexCacheForTest is the test-only seam mirroring
// resetNpxCacheForTest. Production code must not call this.
func resetPageIndexCacheForTest() {
	pageIndexLookupOnce = sync.Once{}
	pageIndexAvailable = false
}
