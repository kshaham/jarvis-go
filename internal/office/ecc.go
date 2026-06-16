package office

// P10 · ECC integration. ECC is a Claude Code plugin/configuration
// system (https://github.com/affaan-m/ECC, MIT). jarvis vendors a
// curated subset of its content:
//
//   - ~20 skill bundles under skills/ecc/<name>/ (shipped to
//     ~/.claude/skills via scripts/install-skills.sh)
//   - 4 Task-tool agents (embedded here, surfaced via BuildECCAgents)
//   - 3 slash commands (embedded here, surfaced via BuildECCCommands)
//
// The whole bundle gates on the spawn-time `disableEcc` flag —
// operators who don't want ECC content set it to true. Mirrors the
// `disableCodegraph` pattern from Phase 6.3.
//
// MIT attribution lives in THIRD_PARTY_LICENSES.md plus an inline
// `> Sourced from ...` line in each embedded .md file.

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kshaham/jarvis/internal/strs"
)

//go:embed ecc_agents/*.md
var eccAgentsFS embed.FS

//go:embed ecc_commands/*.md
var eccCommandsFS embed.FS

// P11.2.8 · Parse-once memoisation. SetupWorktreesWithHooks calls
// BuildECCAgents/Commands inside the per-member spawn loop; without
// memoisation an N-member team re-parses every embedded file N times.
// sync.OnceValue is goroutine-safe + returns the same slice on every
// call, so we hand callers a defensive copy to keep the cached value
// immutable.
var (
	eccAgentsOnce   = sync.OnceValue(func() []ClaudeAgent { return parseEmbeddedAgents(eccAgentsFS, "ecc_agents") })
	eccCommandsOnce = sync.OnceValue(func() []ClaudeCommand { return parseEmbeddedCommands(eccCommandsFS, "ecc_commands") })
)

// BuildECCAgents parses every embedded ECC agent .md file into a
// ClaudeAgent record. Memoised — first call parses, subsequent calls
// return a defensive copy of the cached slice so the per-member
// spawn loop doesn't repeat the parse for every team member.
func BuildECCAgents() []ClaudeAgent {
	cached := eccAgentsOnce()
	out := make([]ClaudeAgent, len(cached))
	copy(out, cached)
	return out
}

// BuildECCCommands parses every embedded ECC command .md file into a
// ClaudeCommand record. The file's basename (without .md) becomes the
// command name; YAML frontmatter populates the metadata fields.
// Memoised on the same pattern as BuildECCAgents.
func BuildECCCommands() []ClaudeCommand {
	cached := eccCommandsOnce()
	out := make([]ClaudeCommand, len(cached))
	copy(out, cached)
	return out
}

// parseEmbeddedAgents walks the given embed.FS subtree and decodes
// each .md file. Frontmatter parsing is intentionally lenient — ECC
// uses both `tools: ["A","B"]` and `tools: [A, B]` shapes; we accept
// either and ignore unknown keys. P11.1.2 sanitization: any
// frontmatter `name` that escapes a basename (contains a path
// separator or `..`) is rejected with a WARN — closes a path-traversal
// risk if a future caller supplies an untrusted embed source.
func parseEmbeddedAgents(efs embed.FS, root string) []ClaudeAgent {
	entries, err := fs.ReadDir(efs, root)
	if err != nil {
		slog.Warn("ecc: read embedded agents dir failed", "root", root, "err", err)
		return nil
	}
	out := make([]ClaudeAgent, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		data, rerr := efs.ReadFile(filepath.Join(root, ent.Name()))
		if rerr != nil {
			slog.Warn("ecc: read embedded agent failed", "file", ent.Name(), "err", rerr)
			continue
		}
		fm, body := splitFrontmatter(string(data))
		rawName := strs.FirstNonEmpty(fm["name"], strings.TrimSuffix(ent.Name(), ".md"))
		safeName, ok := safeBasename(rawName)
		if !ok {
			slog.Warn("ecc: rejecting embedded agent with unsafe name (path traversal attempt or invalid characters)",
				"file", ent.Name(), "rawName", rawName)
			continue
		}
		out = append(out, ClaudeAgent{
			Name:        safeName,
			Description: fm["description"],
			Tools:       splitToolList(fm["tools"]),
			Model:       fm["model"],
			Body:        body,
		})
	}
	return out
}

// parseEmbeddedCommands mirrors parseEmbeddedAgents but produces
// ClaudeCommand records. Both share the same frontmatter shape on
// ECC's side; differences are just which fields jarvis cares about
// downstream (commands carry argument-hint, agents don't). Same
// name-sanitization guard as parseEmbeddedAgents (P11.1.2).
func parseEmbeddedCommands(efs embed.FS, root string) []ClaudeCommand {
	entries, err := fs.ReadDir(efs, root)
	if err != nil {
		slog.Warn("ecc: read embedded commands dir failed", "root", root, "err", err)
		return nil
	}
	out := make([]ClaudeCommand, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		data, rerr := efs.ReadFile(filepath.Join(root, ent.Name()))
		if rerr != nil {
			slog.Warn("ecc: read embedded command failed", "file", ent.Name(), "err", rerr)
			continue
		}
		fm, body := splitFrontmatter(string(data))
		// Commands derive name from the file basename (no frontmatter
		// override). Still sanitize because the basename comes from
		// the embed.FS entry; defence-in-depth.
		rawName := strings.TrimSuffix(ent.Name(), ".md")
		safeName, ok := safeBasename(rawName)
		if !ok {
			slog.Warn("ecc: rejecting embedded command with unsafe name",
				"file", ent.Name(), "rawName", rawName)
			continue
		}
		out = append(out, ClaudeCommand{
			Name:         safeName,
			Description:  fm["description"],
			AllowedTools: splitToolList(strs.FirstNonEmpty(fm["allowed-tools"], fm["tools"])),
			ArgumentHint: fm["argument-hint"],
			Model:        fm["model"],
			Body:         body,
		})
	}
	return out
}

// safeBasename returns (name, true) when `name` is a single filename
// component (no `/`, no `\`, no `..`, no leading `.`). Any other shape
// returns ("", false) so callers can drop the entry instead of letting
// it propagate to a downstream filesystem write. P11.1.2.
func safeBasename(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if name != filepath.Base(name) {
		return "", false
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	if name == "." || name == ".." {
		return "", false
	}
	if strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}

// splitFrontmatter returns (key-value map of YAML frontmatter, body
// markdown). Tolerant: missing frontmatter returns empty map + full
// body. Multi-line YAML values aren't supported (ECC's frontmatter
// only uses single-line scalars + flow-style arrays).
//
// P11.2.6 · CRLF normalisation. Windows-checked-in files use `\r\n`;
// without this normalisation the trailing `\r` makes `strings.TrimSpace`
// return `"---\r"` which != `"---"`, silently dropping the whole
// frontmatter. Future cross-platform vendoring would corrupt every
// embedded file's metadata without this fix.
func splitFrontmatter(s string) (map[string]string, string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	out := map[string]string{}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return out, s
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return out, s
	}
	for _, line := range lines[1:end] {
		if i := strings.Index(line, ":"); i > 0 {
			key := strings.TrimSpace(line[:i])
			if key == "" {
				continue // a whitespace-only key (e.g. "   : value") is malformed; skip it
			}
			val := strings.TrimSpace(line[i+1:])
			out[key] = val
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	return out, strings.TrimLeft(body, "\n")
}

// splitToolList parses ECC's `tools` field. Accepts three flavours
// observed in the wild:
//   - `[Read, Write, Edit]`     (bracketed bare names)
//   - `["Read", "Write"]`       (bracketed + quoted)
//   - `Read, Write, Edit`       (no brackets)
//
// Returns nil for empty.
func splitToolList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------------
// MCP server availability check (P10.4)
// ---------------------------------------------------------------------

// npxLookupOnce + npxAvailable cache the `npx` PATH check. ECC's
// Context7 + Sequential Thinking MCP servers both spawn via `npx -y
// <package>`; missing npx means we shouldn't add them to the spawn
// map (the SDK would try and fail mid-handshake).
var (
	npxLookupOnce sync.Once
	npxAvailable  bool
)

// NpxAvailable reports whether `npx` is on PATH. Cached after the
// first call. Logged once with the resolved path for operator
// diagnostics. NOTE: if npx is installed AFTER the daemon starts,
// restart the daemon for ECC MCP servers to activate.
func NpxAvailable() bool {
	npxLookupOnce.Do(func() {
		if path, err := exec.LookPath("npx"); err == nil {
			npxAvailable = true
			slog.Info("npx detected; ECC MCP servers (context7, sequential-thinking) will be wired",
				"path", path)
		} else {
			slog.Info("npx not found on PATH; ECC MCP servers will be skipped",
				"err", err)
		}
	})
	return npxAvailable
}

// resetNpxCacheForTest is the test-only seam mirroring
// resetCodegraphCacheForTest in codegraph_test.go. Production code
// must not call this.
func resetNpxCacheForTest() {
	npxLookupOnce = sync.Once{}
	npxAvailable = false
}

// resetECCMemoForTest clears the OnceValue caches so tests can
// re-exercise the parse path (e.g. CRLF normalisation regression
// tests). Production code must not call this.
func resetECCMemoForTest() {
	eccAgentsOnce = sync.OnceValue(func() []ClaudeAgent { return parseEmbeddedAgents(eccAgentsFS, "ecc_agents") })
	eccCommandsOnce = sync.OnceValue(func() []ClaudeCommand { return parseEmbeddedCommands(eccCommandsFS, "ecc_commands") })
}

// hasBodyAttribution (renamed from hasFrontmatterAttribution in P11.1.3)
// checks the vendored content carries the MIT attribution. Looks only
// in the first 200 bytes of the body so a "vendor cleanup" PR that
// moves the attribution comment to a sibling file or buries it deep
// in the body fails the test loudly.
//
// Match relaxed to the GitHub org+repo path so the test survives
// upstream URL drift (HTTPS → SSH, raw → blob, branch rename) without
// rewriting every test fixture.
func hasBodyAttribution(body string) bool {
	const window = 200
	leading := body
	if len(leading) > window {
		leading = leading[:window]
	}
	return strings.Contains(leading, "github.com/affaan-m/ECC")
}

// hasFrontmatterAttribution is retained as a deprecated alias for
// backward compat with tests that haven't migrated yet. New callers
// should use hasBodyAttribution directly.
//
// Deprecated: use hasBodyAttribution.
func hasFrontmatterAttribution(body string) bool { return hasBodyAttribution(body) }

// EmbedSummaryForLogging is a startup-time diagnostic showing how
// much ECC content was successfully embedded. P11.3.10 wired into
// server bootstrap so operators see "ecc: 4 agents, 3 commands
// embedded" alongside the codegraph + npx availability log lines.
// Capitalised because it's now called from outside the package.
func EmbedSummaryForLogging() string {
	agents := BuildECCAgents()
	cmds := BuildECCCommands()
	return fmt.Sprintf("ecc: %d agents, %d commands embedded", len(agents), len(cmds))
}
