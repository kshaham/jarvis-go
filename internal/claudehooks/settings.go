package claudehooks

// Generates the per-agent .claude/settings.local.json that points
// every relevant Claude Code hook event at this daemon. Pure JSON
// builder -- no dependency on the office package, so it can be
// imported by both the dispatcher AND internal/office/worktree.go
// without creating a cycle.
//
// We write settings.local.json (not settings.json) to:
//   1. avoid colliding with whatever the operator has committed at
//      the project level in .claude/settings.json, and
//   2. take advantage of Claude Code's documented precedence:
//      .local.json wins over .json for the same project.
//
// The generated file is stamped with a sentinel field so the cleanup
// path can distinguish "Jarvis-written, safe to delete" from "operator
// edited this themselves, leave alone."

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// JarvisMarkerKey is the top-level key Jarvis stamps into every
// settings.local.json it writes. Cleanup checks for it before
// deleting / merging so we never clobber operator-authored content.
const JarvisMarkerKey = "_jarvisGenerated"

// HookedEvents is the canonical list of events Jarvis subscribes to.
// Order is stable so the generated settings JSON is deterministic
// across runs (golden-file friendly). Adding an event here without a
// handler in handlers.go is an error caught at dispatch time.
//
// Covers the full hooks surface relevant to office agents:
//   - Lifecycle: SessionStart/End, Stop, StopFailure
//   - Per-turn: UserPromptSubmit, PreToolUse, PostToolUse[Failure], PostToolBatch
//   - Permissions: PermissionRequest, PermissionDenied
//   - Agent/task: TaskCreated, TaskCompleted, SubagentStart, SubagentStop
//   - Context: PreCompact, PostCompact, CwdChanged, InstructionsLoaded, ConfigChange
//   - MCP: Elicitation, ElicitationResult
//   - Worktree: WorktreeCreate, WorktreeRemove (wired now for R3.1 migration)
//   - Notification: operator-visible banners
var HookedEvents = []string{
	"SessionStart",
	"SessionEnd",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"PostToolBatch",
	"Stop",
	"StopFailure",
	"PermissionRequest",
	"PermissionDenied",
	"TaskCreated",
	"TaskCompleted",
	"SubagentStart",
	"SubagentStop",
	"CwdChanged",
	"InstructionsLoaded",
	"ConfigChange",
	"Elicitation",
	"ElicitationResult",
	"Notification",
	"PreCompact",
	"PostCompact",
	"WorktreeCreate",
	"WorktreeRemove",
}

// SettingsParams are the inputs to BuildSettings. DaemonURL must be
// reachable from inside the spawned `claude` process -- typically
// http://127.0.0.1:<port> when claude runs on the same host as the
// daemon, or the container-network address when claude runs in
// Docker. SessionID identifies the office agent and becomes part of
// every hook URL so the dispatcher can route events to the right
// AgentState without parsing the JSON body.
type SettingsParams struct {
	SessionID string
	DaemonURL string
	// AuthEnvVar is the env-var name Claude Code will interpolate into
	// the X-Jarvis-Agent-Token header. The same name must be present
	// in the spawned process's env -- Jarvis already sets
	// JARVIS_AGENT_TOKEN in getProviderEnvVars, so this defaults to
	// that. Tests override it.
	AuthEnvVar string
}

// BuildSettings returns the JSON bytes for settings.local.json.
// Deterministic for a given input -- safe to snapshot in tests.
func BuildSettings(p SettingsParams) ([]byte, error) {
	if p.SessionID == "" {
		return nil, errors.New("claudehooks: SessionID is required")
	}
	if p.DaemonURL == "" {
		return nil, errors.New("claudehooks: DaemonURL is required")
	}
	authVar := p.AuthEnvVar
	if authVar == "" {
		authVar = "JARVIS_AGENT_TOKEN"
	}

	hooksByEvent := make(map[string][]eventBinding, len(HookedEvents))
	for _, event := range HookedEvents {
		hooksByEvent[event] = []eventBinding{{
			// Most events have no matcher; SessionStart limits to "startup"
			// so resume/clear/compact don't double-fire (we'd handle them
			// later if needed).
			Matcher: matcherFor(event),
			Hooks: []hookEntry{{
				Type:    "http",
				URL:     fmt.Sprintf("%s/api/claude/hooks/%s/%s", p.DaemonURL, p.SessionID, event),
				Timeout: timeoutFor(event),
				Headers: map[string]string{
					"X-Jarvis-Agent-Token": "${" + authVar + "}",
				},
				AllowedEnvVars: []string{authVar},
			}},
		}}
	}

	doc := settingsDoc{
		JarvisGenerated: true,
		Hooks:           hooksByEvent,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("claudehooks: encode settings: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteSettings writes BuildSettings output to the agent's
// .claude/settings.local.json under cwd. Atomic via tmp+rename so a
// concurrent claude reading the file never sees a half-written
// document. If the target file already exists and was NOT written by
// Jarvis, returns ErrOperatorOwned so the caller can decide whether
// to skip or merge.
func WriteSettings(cwd string, data []byte) error {
	dir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("claudehooks: mkdir .claude: %w", err)
	}
	target := filepath.Join(dir, "settings.local.json")

	if existing, err := os.ReadFile(target); err == nil {
		if !isJarvisAuthored(existing) {
			return ErrOperatorOwned
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("claudehooks: read existing settings: %w", err)
	}

	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("claudehooks: write tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("claudehooks: rename: %w", err)
	}
	return nil
}

// RemoveSettings removes our settings.local.json from cwd's .claude
// directory iff the file carries the Jarvis marker. Operator-owned
// files are left alone. Used by the team teardown path.
func RemoveSettings(cwd string) error {
	target := filepath.Join(cwd, ".claude", "settings.local.json")
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !isJarvisAuthored(data) {
		return ErrOperatorOwned
	}
	return os.Remove(target)
}

// ErrOperatorOwned is returned by WriteSettings/RemoveSettings when
// the target file exists but was not generated by Jarvis. Callers
// should typically log a warning and continue (skip injection) rather
// than overwrite the operator's content.
var ErrOperatorOwned = errors.New("claudehooks: settings.local.json exists and was not generated by Jarvis")

// settingsDoc is the on-disk shape of .claude/settings.local.json.
// Intentionally minimal -- Claude Code merges hook lists across all
// settings sources, so a sparse file is fine and won't disable other
// settings the operator has elsewhere.
//
// TODO(self-improvement): hooks-mode agents currently miss the
// PageIndex MCP because settingsDoc has no mcpServers field. SDK-mode
// agents pick it up via pty.SessionConfig.McpServers. To wire
// hooks-mode, either (a) add an MCP map to settingsDoc and let
// BuildSettings accept caller-supplied entries, or (b) write a
// separate .mcp.json at project root from the office package. (a) is
// cleaner because it keeps the Jarvis-authored marker; (b) avoids
// touching the settings.local.json schema. Defer until hooks mode
// becomes a primary deployment shape.
type settingsDoc struct {
	JarvisGenerated bool                       `json:"_jarvisGenerated"`
	Hooks           map[string][]eventBinding `json:"hooks"`
}

type eventBinding struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type           string            `json:"type"`
	URL            string            `json:"url"`
	Timeout        int               `json:"timeout,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`
}

// matcherFor returns the documented matcher for an event, or "" when
// none applies. SessionStart's "startup" matcher avoids firing on
// resume/clear/compact -- we treat those as no-ops in v1.
func matcherFor(event string) string {
	switch event {
	case "SessionStart":
		return "startup"
	}
	return ""
}

// timeoutFor caps how long Claude waits for a hook response (seconds).
// Most events get 5s -- handlers do trivial AgentState mutations and
// return 204 fast. Stop gets 30s because it tail-reads the transcript
// for token usage; 30s is well under the documented 600s default and
// keeps a slow filesystem from stalling Claude indefinitely.
func timeoutFor(event string) int {
	switch event {
	case "Stop":
		return 30
	}
	return 5
}

// isJarvisAuthored returns true if the JSON document at the path
// carries the Jarvis sentinel marker. Lenient: any unparseable JSON
// returns false (treat as "not ours, don't touch").
func isJarvisAuthored(data []byte) bool {
	var probe struct {
		JarvisGenerated bool `json:"_jarvisGenerated"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.JarvisGenerated
}
