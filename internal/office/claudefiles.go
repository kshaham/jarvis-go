package office

// Per-session generators for `.claude/commands/*.md` and (R2.2)
// `.claude/agents/*.md`. Each spawned agent gets a curated set of
// office-aware slash commands and Task-tool subagent definitions
// written into its worktree alongside the existing
// `.claude/settings.local.json` (claudehooks.WriteSettings).
//
// Why per-session, not global:
//   - Operators don't have to install anything in ~/.claude/ to get
//     office-specific commands working in spawned agents.
//   - Different teams can ship different command sets (R2.1 keeps
//     it homogeneous; future tier-specific commands hook in here).
//   - The files vanish when the worktree is torn down -- no leftover
//     /standup or /handoff commands lingering in the operator's
//     personal Claude install.
//
// Files are written with a sentinel front-matter line so the cleanup
// pass can distinguish "jarvis-generated, safe to remove" from
// "operator-written, leave alone" -- same pattern as the hooks
// settings file.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// jarvisFileMarker is the leading line every generated .claude/*.md
// file carries. Stays inside an HTML comment so it doesn't render in
// Claude's slash-command picker.
const jarvisFileMarker = "<!-- generated-by: jarvis-office -->"

// ClaudeCommand describes one slash-command to write. Name is the
// file stem (becomes `/name`); Frontmatter is the YAML block Claude
// reads to pick tools, model, description; Body is the prompt the
// command expands into.
type ClaudeCommand struct {
	Name        string
	Description string // operator-facing description; surfaces in `/help`
	AllowedTools []string // empty = inherit; set to restrict the command
	ArgumentHint string   // shown after the slash command in the picker
	Model        string   // empty = inherit
	Body         string   // expanded prompt text
}

// ClaudeAgent (R2.2) describes one Task-tool subagent definition.
type ClaudeAgent struct {
	Name        string
	Description string
	Tools       []string // e.g. ["Bash", "Read", "Grep"]
	Model       string   // empty = inherit (usually sonnet)
	Body        string   // system-prompt-like instructions
}

// WriteCommands installs every command in `cmds` to
// <cwd>/.claude/commands/. Idempotent: files already authored by
// jarvis are overwritten silently; operator-authored files (no
// marker) are left alone with a warning. Returns the count written
// and any first error encountered.
func WriteCommands(cwd string, cmds []ClaudeCommand) (int, error) {
	if len(cmds) == 0 {
		return 0, nil
	}
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("claudefiles: mkdir commands: %w", err)
	}
	wrote := 0
	for _, c := range cmds {
		path := filepath.Join(dir, c.Name+".md")
		if existing, err := os.ReadFile(path); err == nil && !bytes.HasPrefix(existing, []byte(jarvisFileMarker)) {
			// Operator file -- skip with warn (caller decides whether
			// to log). Returning an error would fail the whole spawn
			// over a single benign collision, so we just skip.
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return wrote, fmt.Errorf("claudefiles: read existing %s: %w", path, err)
		}
		body := renderCommand(c)
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
			return wrote, fmt.Errorf("claudefiles: write tmp: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return wrote, fmt.Errorf("claudefiles: rename %s: %w", path, err)
		}
		wrote++
	}
	return wrote, nil
}

// WriteAgents installs every agent in `agents` to <cwd>/.claude/agents/.
// Same semantics as WriteCommands.
func WriteAgents(cwd string, agents []ClaudeAgent) (int, error) {
	if len(agents) == 0 {
		return 0, nil
	}
	dir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("claudefiles: mkdir agents: %w", err)
	}
	wrote := 0
	for _, a := range agents {
		path := filepath.Join(dir, a.Name+".md")
		if existing, err := os.ReadFile(path); err == nil && !bytes.HasPrefix(existing, []byte(jarvisFileMarker)) {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return wrote, fmt.Errorf("claudefiles: read existing %s: %w", path, err)
		}
		body := renderAgent(a)
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
			return wrote, fmt.Errorf("claudefiles: write tmp: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return wrote, fmt.Errorf("claudefiles: rename %s: %w", path, err)
		}
		wrote++
	}
	return wrote, nil
}

// CleanupClaudeFiles removes every jarvis-authored .md under
// <cwd>/.claude/{commands,agents}/. Operator-authored files (no
// marker) are left alone. Used by team teardown so worktrees don't
// accumulate stale command definitions across spawns.
func CleanupClaudeFiles(cwd string) error {
	var firstErr error
	for _, sub := range []string{"commands", "agents"} {
		dir := filepath.Join(cwd, ".claude", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if !bytes.HasPrefix(data, []byte(jarvisFileMarker)) {
				continue
			}
			if err := os.Remove(path); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ErrNoCwd is returned when the caller passes an empty cwd.
var ErrNoCwd = errors.New("claudefiles: cwd is required")

// appendSystemPromptFileName is the per-session file that holds the
// stable cross-agent kickstart prefix. Lives in .claude/ so Claude
// finds it via CLAUDE_PROJECT_DIR and the SDK can pass the path to
// --append-system-prompt-file. Identical content across teammates
// means Anthropic's ephemeral cache (5-min TTL, 90% discount on
// hits) catches the prefix without us paying per-call tokenisation.
const appendSystemPromptFileName = "jarvis-append-system-prompt.md"

// AppendSystemPromptPath returns the absolute path the prefix file will be
// written to for a given cwd and variant. The variant (e.g. the agent tier)
// is folded into the filename so members that share a working dir but have
// DIFFERENT prompt content (BuildStableSystemPrompt varies by tier) don't
// clobber each other's file — while same-variant members still share one file
// for cache reuse. An empty variant uses the base filename.
func AppendSystemPromptPath(cwd, variant string) string {
	name := appendSystemPromptFileName
	if variant != "" {
		base := strings.TrimSuffix(name, ".md")
		// sanitize: keep alnum/dash so the variant can't escape the filename
		safe := make([]rune, 0, len(variant))
		for _, r := range variant {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				safe = append(safe, r)
			}
		}
		name = base + "-" + string(safe) + ".md"
	}
	return filepath.Join(cwd, ".claude", name)
}

// WriteAppendSystemPromptFile installs the stable system-prompt
// prefix at <cwd>/.claude/jarvis-append-system-prompt.md so the
// spawned claude can read it via --append-system-prompt-file. The
// path is what the caller forwards to SessionConfig.AppendSystemPromptFile.
//
// Operator-authored files (no marker) are NOT overwritten -- same
// safety pattern as the other claudefiles helpers. Returns the
// absolute path written (or pre-existing operator path) so the
// spawn handler can forward it unconditionally.
func WriteAppendSystemPromptFile(cwd, variant, content string) (string, error) {
	if cwd == "" {
		return "", ErrNoCwd
	}
	if content == "" {
		return "", fmt.Errorf("claudefiles: AppendSystemPrompt content is empty")
	}
	dir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("claudefiles: mkdir .claude: %w", err)
	}
	path := AppendSystemPromptPath(cwd, variant)
	// If a file exists and wasn't authored by jarvis, leave it alone
	// and return its path -- the operator wants their content used.
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.HasPrefix(existing, []byte(jarvisFileMarker)) {
			return path, nil
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("claudefiles: read existing append-system-prompt: %w", err)
	}
	body := jarvisFileMarker + "\n" + content
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("claudefiles: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("claudefiles: rename: %w", err)
	}
	return path, nil
}

// renderCommand serialises a ClaudeCommand to the Markdown format
// Claude Code expects:
//
//   <marker>
//   ---
//   description: ...
//   argument-hint: ...
//   allowed-tools: Bash, Read
//   model: sonnet
//   ---
//   <prompt body>
func renderCommand(c ClaudeCommand) string {
	var sb strings.Builder
	sb.WriteString(jarvisFileMarker)
	sb.WriteByte('\n')
	sb.WriteString("---\n")
	if c.Description != "" {
		sb.WriteString("description: ")
		sb.WriteString(yamlScalar(c.Description))
		sb.WriteByte('\n')
	}
	if c.ArgumentHint != "" {
		sb.WriteString("argument-hint: ")
		sb.WriteString(yamlScalar(c.ArgumentHint))
		sb.WriteByte('\n')
	}
	if len(c.AllowedTools) > 0 {
		sb.WriteString("allowed-tools: ")
		sb.WriteString(strings.Join(c.AllowedTools, ", "))
		sb.WriteByte('\n')
	}
	if c.Model != "" {
		sb.WriteString("model: ")
		sb.WriteString(c.Model)
		sb.WriteByte('\n')
	}
	sb.WriteString("---\n")
	sb.WriteString(c.Body)
	if !strings.HasSuffix(c.Body, "\n") {
		sb.WriteByte('\n')
	}
	return sb.String()
}

// renderAgent serialises a ClaudeAgent for .claude/agents/.
func renderAgent(a ClaudeAgent) string {
	var sb strings.Builder
	sb.WriteString(jarvisFileMarker)
	sb.WriteByte('\n')
	sb.WriteString("---\n")
	sb.WriteString("name: ")
	sb.WriteString(a.Name)
	sb.WriteByte('\n')
	if a.Description != "" {
		sb.WriteString("description: ")
		sb.WriteString(yamlScalar(a.Description))
		sb.WriteByte('\n')
	}
	if len(a.Tools) > 0 {
		sb.WriteString("tools: ")
		sb.WriteString(strings.Join(a.Tools, ", "))
		sb.WriteByte('\n')
	}
	if a.Model != "" {
		sb.WriteString("model: ")
		sb.WriteString(a.Model)
		sb.WriteByte('\n')
	}
	sb.WriteString("---\n")
	sb.WriteString(a.Body)
	if !strings.HasSuffix(a.Body, "\n") {
		sb.WriteByte('\n')
	}
	return sb.String()
}

// yamlScalar quotes a string for safe insertion into a YAML scalar
// value. Wraps in double quotes only when the content has YAML-special
// characters; bare strings save bytes when possible.
func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, `:#{}[]&*!|>'"%@\` + "\n\t") {
		// Escape backslash and double quote, then wrap.
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}
