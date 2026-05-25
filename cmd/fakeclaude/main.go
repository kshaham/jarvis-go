// fakeclaude is a tiny test binary that impersonates the real
// `claude` CLI for the office hooks-mode integration test. It does
// NOT do any LLM work -- it just reads the .claude/settings.local.json
// that Jarvis wrote into its cwd at spawn time, walks the URL list,
// and POSTs a scripted sequence of hook events at each one.
//
// Usage (the office spawn handler points pty.SessionConfig.ClaudeBinaryPath
// at this binary):
//
//   FAKECLAUDE_SCRIPT=/path/to/script.json fakeclaude
//
// If FAKECLAUDE_SCRIPT is unset, runs the default minimal sequence:
//   SessionStart -> UserPromptSubmit -> PreToolUse(Bash) ->
//   PostToolUse(Bash) -> Stop -> SessionEnd
//
// On exit, the office's normal session-death detection fires.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// scriptedEvent is one entry in a fakeclaude script. Type names match
// the Claude Code hook event names exactly. DelayMs is sleep time
// AFTER the event fires (lets tests probe state mid-sequence).
type scriptedEvent struct {
	Type     string                 `json:"type"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
	DelayMs  int                    `json:"delayMs,omitempty"`
}

// defaultScript is what fakeclaude runs when FAKECLAUDE_SCRIPT is
// unset. Mirrors the minimum a real claude session would emit for one
// turn: start, get prompt, run a Bash command, finish.
var defaultScript = []scriptedEvent{
	{Type: "SessionStart", Payload: map[string]interface{}{
		"source": "startup",
		"model":  "claude-sonnet-4-6",
	}, DelayMs: 50},
	{Type: "UserPromptSubmit", Payload: map[string]interface{}{
		"prompt": "do a small thing",
	}, DelayMs: 50},
	{Type: "PreToolUse", Payload: map[string]interface{}{
		"tool_name":   "Bash",
		"tool_input":  map[string]interface{}{"command": "echo hi"},
		"tool_use_id": "tu-1",
	}, DelayMs: 50},
	{Type: "PostToolUse", Payload: map[string]interface{}{
		"tool_name":   "Bash",
		"tool_input":  map[string]interface{}{"command": "echo hi"},
		"tool_use_id": "tu-1",
		"tool_result": map[string]interface{}{"text": "hi"},
	}, DelayMs: 50},
	{Type: "Stop", Payload: map[string]interface{}{}, DelayMs: 50},
	{Type: "SessionEnd", Payload: map[string]interface{}{
		"reason": "fakeclaude-finished-script",
	}, DelayMs: 0},
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		die("getwd: %v", err)
	}

	settings, err := loadSettings(cwd)
	if err != nil {
		die("load settings: %v", err)
	}
	urls := extractHookURLs(settings)
	if len(urls) == 0 {
		die("no hook URLs found in %s/.claude/settings.local.json", cwd)
	}
	authToken := os.Getenv("JARVIS_AGENT_TOKEN")

	transcriptPath := filepath.Join(cwd, "fakeclaude-transcript.jsonl")
	if err := writeStubTranscript(transcriptPath); err != nil {
		die("seed transcript: %v", err)
	}

	script := loadScript()
	for _, ev := range script {
		url, ok := urls[ev.Type]
		if !ok {
			fmt.Fprintf(os.Stderr, "fakeclaude: no URL for event %q (skipping)\n", ev.Type)
		} else {
			body := buildEnvelope(ev.Type, transcriptPath, cwd, ev.Payload)
			if err := postEvent(url, authToken, body); err != nil {
				fmt.Fprintf(os.Stderr, "fakeclaude: POST %s failed: %v\n", ev.Type, err)
			}
		}
		if ev.DelayMs > 0 {
			time.Sleep(time.Duration(ev.DelayMs) * time.Millisecond)
		}
	}
	// Stay alive for a moment so the parent's idle detector observes
	// our process is still running (mimics a real claude session that
	// would keep the PTY open until killed).
	time.Sleep(200 * time.Millisecond)
}

// loadSettings reads {cwd}/.claude/settings.local.json and returns
// the parsed document.
func loadSettings(cwd string) (map[string]interface{}, error) {
	path := filepath.Join(cwd, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// extractHookURLs returns event-name -> URL for every hook block in
// the settings. Mirrors the JSON shape produced by
// internal/claudehooks/settings.go BuildSettings.
func extractHookURLs(doc map[string]interface{}) map[string]string {
	out := map[string]string{}
	hooks, _ := doc["hooks"].(map[string]interface{})
	for event, raw := range hooks {
		bindings, ok := raw.([]interface{})
		if !ok || len(bindings) == 0 {
			continue
		}
		first, ok := bindings[0].(map[string]interface{})
		if !ok {
			continue
		}
		entries, ok := first["hooks"].([]interface{})
		if !ok || len(entries) == 0 {
			continue
		}
		entry, ok := entries[0].(map[string]interface{})
		if !ok {
			continue
		}
		if u, ok := entry["url"].(string); ok && u != "" {
			out[event] = u
		}
	}
	return out
}

// buildEnvelope constructs the JSON body claude would POST. We slot
// in BaseEvent fields plus whatever event-specific fields the script
// supplies in payload.
func buildEnvelope(event, transcriptPath, cwd string, payload map[string]interface{}) []byte {
	body := map[string]interface{}{
		"session_id":      "fake-session",
		"transcript_path": transcriptPath,
		"cwd":             cwd,
		"hook_event_name": event,
	}
	for k, v := range payload {
		body[k] = v
	}
	out, _ := json.Marshal(body)
	return out
}

// postEvent sends one event POST. Empty/204 responses are normal.
func postEvent(url, token string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Jarvis-Agent-Token", token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// writeStubTranscript seeds a tiny transcript JSONL so the dispatcher's
// Stop handler (which tail-reads transcript_path for usage) finds a
// plausible assistant message with a usage block.
func writeStubTranscript(path string) error {
	const stub = `{"type":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":50}}` + "\n"
	return os.WriteFile(path, []byte(stub), 0o644)
}

// loadScript returns the user-provided script if FAKECLAUDE_SCRIPT
// is set, else the default sequence.
func loadScript() []scriptedEvent {
	path := os.Getenv("FAKECLAUDE_SCRIPT")
	if path == "" {
		return defaultScript
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeclaude: cannot read script %q: %v (using default)\n", path, err)
		return defaultScript
	}
	var script []scriptedEvent
	if err := json.Unmarshal(data, &script); err != nil {
		fmt.Fprintf(os.Stderr, "fakeclaude: bad script JSON: %v (using default)\n", err)
		return defaultScript
	}
	return script
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fakeclaude: "+format+"\n", args...)
	os.Exit(1)
}
