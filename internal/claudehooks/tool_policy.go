package claudehooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Tool-policy filter: a PreToolUse-time safety net that denies known-
// dangerous tool calls even when the agent runs in bypass-permissions
// mode. The operator's choice (in this Phase) was to KEEP bypass but
// add a policy hook -- this is the implementation of that hook.
//
// The policy is a JSON document at ~/.jarvis/tool-policy.json. Each
// rule names a tool (Bash, Edit, Write, etc.) and either a regex
// pattern (matched against the tool's "obvious string field": Bash
// command, Edit/Write file_path) or a path-prefix list. Matches deny
// with a reason that's surfaced back to Claude via permissionDecision.
//
// File-not-found -> defaults are used. Malformed file -> defaults
// used, error logged. Defaults are intentionally conservative: block
// outright-destructive shell invocations and writes to system paths.
//
// Mutex-guarded reload so the web Settings page can hot-swap the
// policy without restarting jarvis.

// ToolPolicyRule is one entry in the policy.
type ToolPolicyRule struct {
	Tool        string   `json:"tool"`                  // tool name (Bash, Edit, Write, ...)
	Pattern     string   `json:"pattern,omitempty"`     // regex against the tool's primary string field
	PathPrefix  []string `json:"pathPrefix,omitempty"`  // for Edit/Write: deny if file_path begins with any of these
	Description string   `json:"description,omitempty"` // human-readable rationale, surfaced to the agent
}

// ToolPolicy is the on-disk document.
type ToolPolicy struct {
	DenyRules []ToolPolicyRule `json:"denyRules"`
	// AllowOutsideWorktree, when false, denies any Bash invocation
	// whose first argument is an absolute path outside the agent's
	// worktree. Today this is enforced cooperatively (we don't have
	// the worktree in the hook event); reserved for R3.1 worktree
	// migration.
	AllowOutsideWorktree bool `json:"allowOutsideWorktree"`
}

// DefaultToolPolicy is what new installs start with. Tuned to deny
// the worst foot-guns without being so strict that agents can't do
// their jobs. Operators can override the entire file from the web UI.
var DefaultToolPolicy = ToolPolicy{
	DenyRules: []ToolPolicyRule{
		{
			Tool:        "Bash",
			Pattern:     `\brm\s+-[a-zA-Z]*r[a-zA-Z]*\s+(/(etc|usr|bin|sbin|System|Users|home|opt|Library|Applications|root|boot|lib|lib64|sys|proc)|~|\$HOME)`,
			Description: "Refuses recursive deletes that target user-home or system paths.",
		},
		{
			Tool:        "Bash",
			Pattern:     `\bsudo\b`,
			Description: "Refuses sudo: office agents must not escalate.",
		},
		{
			Tool:        "Bash",
			Pattern:     `\bcurl\s+[^|]*\|\s*(sudo\s+)?(bash|sh|zsh)\b`,
			Description: "Refuses curl | sh shell pipelines.",
		},
		{
			Tool:        "Bash",
			Pattern:     `\bwget\s+[^|]*\|\s*(sudo\s+)?(bash|sh|zsh)\b`,
			Description: "Refuses wget | sh shell pipelines.",
		},
		{
			Tool:        "Bash",
			Pattern:     `\b(eval|exec)\s+\$\(`,
			Description: "Refuses eval/exec of command-substituted strings.",
		},
		{
			Tool: "Bash",
			// World-writable ⟺ the others (last) octal digit grants write: one
			// of 2,3,6,7. The previous pattern matched any mode CONTAINING a 7,
			// which both false-positived on safe modes (chmod 700/755) and
			// missed world-writable modes with no 7 (chmod 666/022).
			Pattern:     `\bchmod\s+(-[a-zA-Z]+\s+)*[0-7]{2,3}[2367]\b`,
			Description: "Refuses world-writable chmods.",
		},
		{
			Tool:       "Write",
			PathPrefix: []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/System/", "~/.ssh/"},
			Description: "Refuses writes to system or SSH paths.",
		},
		{
			Tool:       "Edit",
			PathPrefix: []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/System/", "~/.ssh/"},
			Description: "Refuses edits to system or SSH paths.",
		},
	},
	AllowOutsideWorktree: false,
}

// ToolPolicyStore is a thread-safe, file-backed policy holder.
// One instance per process; the hooks dispatcher consults it on every
// PreToolUse via Evaluate(). Operators mutate it via Replace() from
// the /api/office/tool-policy endpoint.
type ToolPolicyStore struct {
	path   string
	mu     sync.RWMutex
	policy ToolPolicy
	// compiledDenyRules mirrors policy.DenyRules with regexes
	// pre-compiled so the hook path doesn't pay the compile cost on
	// every Bash call.
	compiledDenyRules []compiledRule
}

type compiledRule struct {
	rule    ToolPolicyRule
	pattern *regexp.Regexp // nil if rule has no pattern
}

// NewToolPolicyStore loads the policy from path or seeds with the
// default. Pass "" to start in memory-only mode (tests).
func NewToolPolicyStore(path string) *ToolPolicyStore {
	s := &ToolPolicyStore{path: path}
	s.load()
	return s
}

func (s *ToolPolicyStore) load() {
	if s.path == "" {
		s.replaceLocked(DefaultToolPolicy)
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("toolpolicy: read failed; using defaults", "path", s.path, "err", err)
		}
		s.replaceLocked(DefaultToolPolicy)
		return
	}
	var loaded ToolPolicy
	if err := json.Unmarshal(data, &loaded); err != nil {
		slog.Warn("toolpolicy: malformed file; using defaults", "path", s.path, "err", err)
		s.replaceLocked(DefaultToolPolicy)
		return
	}
	s.replaceLocked(loaded)
}

// Replace swaps the in-memory policy + writes it to disk atomically.
// Returns an error if any rule has an invalid regex (caller can show
// the operator before discarding their edit).
func (s *ToolPolicyStore) Replace(next ToolPolicy) error {
	if err := validatePolicy(next); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Persist to disk FIRST. If the write fails we return the error with the
	// in-memory policy UNTOUCHED, so live policy always matches what's persisted
	// (previously replaceLocked ran first, so a write failure left the live
	// gating diverged from disk until the next restart).
	if s.path != "" {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
			return fmt.Errorf("toolpolicy: mkdir: %w", err)
		}
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return fmt.Errorf("toolpolicy: marshal: %w", err)
		}
		tmp := s.path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return fmt.Errorf("toolpolicy: write tmp: %w", err)
		}
		if err := os.Rename(tmp, s.path); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("toolpolicy: rename: %w", err)
		}
	}
	s.replaceLocked(next)
	return nil
}

// Snapshot returns a copy of the current policy. Safe to surface to
// operators without leaking the internal compiled forms.
func (s *ToolPolicyStore) Snapshot() ToolPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Defensive copy so callers can't mutate the live policy.
	cp := ToolPolicy{
		DenyRules:            make([]ToolPolicyRule, len(s.policy.DenyRules)),
		AllowOutsideWorktree: s.policy.AllowOutsideWorktree,
	}
	copy(cp.DenyRules, s.policy.DenyRules)
	return cp
}

// Evaluate checks a PreToolUse event against the active policy.
// Returns (denied, reason): denied=true means deny the call; the
// reason is human-readable and gets surfaced to the agent via
// permissionDecisionReason so the next turn knows why.
func (s *ToolPolicyStore) Evaluate(toolName string, toolInput []byte) (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.compiledDenyRules) == 0 {
		return false, ""
	}
	primary := primaryField(toolName, toolInput)
	pathField := pathFieldFor(toolName, toolInput)
	for _, cr := range s.compiledDenyRules {
		if !equalFold(cr.rule.Tool, toolName) {
			continue
		}
		if cr.pattern != nil && primary != "" && cr.pattern.MatchString(primary) {
			return true, fmt.Sprintf("tool-policy: %s (matched pattern)", cr.rule.Description)
		}
		if len(cr.rule.PathPrefix) > 0 && pathField != "" {
			for _, prefix := range cr.rule.PathPrefix {
				if hasPrefixExpandingHome(pathField, prefix) {
					return true, fmt.Sprintf("tool-policy: %s (path prefix %s)", cr.rule.Description, prefix)
				}
			}
		}
	}
	return false, ""
}

// replaceLocked must be called with s.mu held in the case of Replace,
// or fresh on construction. Recompiles regexes.
func (s *ToolPolicyStore) replaceLocked(next ToolPolicy) {
	compiled := make([]compiledRule, 0, len(next.DenyRules))
	for _, rule := range next.DenyRules {
		entry := compiledRule{rule: rule}
		if rule.Pattern != "" {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				// Validated upstream; this is defensive only.
				slog.Warn("toolpolicy: skipping bad rule", "tool", rule.Tool, "err", err)
				continue
			}
			entry.pattern = re
		}
		compiled = append(compiled, entry)
	}
	s.policy = next
	s.compiledDenyRules = compiled
}

func validatePolicy(p ToolPolicy) error {
	for i, rule := range p.DenyRules {
		if rule.Tool == "" {
			return fmt.Errorf("rule %d: tool is required", i)
		}
		if rule.Pattern == "" && len(rule.PathPrefix) == 0 {
			return fmt.Errorf("rule %d (%s): must set pattern or pathPrefix", i, rule.Tool)
		}
		if rule.Pattern != "" {
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				return fmt.Errorf("rule %d (%s): invalid regex %q: %w", i, rule.Tool, rule.Pattern, err)
			}
		}
	}
	return nil
}

// primaryField returns the string field a pattern is matched against
// for a given tool. Bash -> command, Read -> file_path, etc. Unknown
// tools return "" (no pattern can match).
func primaryField(toolName string, toolInput []byte) string {
	if len(toolInput) == 0 {
		return ""
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(toolInput, &probe); err != nil {
		return ""
	}
	switch toolName {
	case "Bash":
		return decodeString(probe["command"])
	case "Read", "Glob", "Grep", "Write", "Edit":
		return decodeString(probe["file_path"])
	case "WebFetch", "WebSearch":
		return decodeString(probe["url"]) + " " + decodeString(probe["prompt"])
	default:
		return ""
	}
}

// pathFieldFor extracts just the path field for prefix matching. Same
// idea as primaryField but only returns paths (so the Bash command
// string doesn't accidentally trip a /etc/ prefix rule).
func pathFieldFor(toolName string, toolInput []byte) string {
	if len(toolInput) == 0 {
		return ""
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(toolInput, &probe); err != nil {
		return ""
	}
	switch toolName {
	case "Read", "Write", "Edit", "Glob", "Grep":
		return decodeString(probe["file_path"])
	default:
		return ""
	}
}

func decodeString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func hasPrefixExpandingHome(path, prefix string) bool {
	if strings.HasPrefix(prefix, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			prefix = filepath.Join(home, prefix[2:]) + string(filepath.Separator)
		}
	}
	return strings.HasPrefix(path, prefix)
}

func equalFold(a, b string) bool {
	return strings.EqualFold(a, b)
}
