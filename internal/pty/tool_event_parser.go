package pty

// R21 · Extract structured tool_use and tool_result content blocks
// from the SDK event stream.
//
// The Go SDK v0.3.1's ContentBlock struct only captures Type, Text,
// Thinking — it silently drops the id/name/input fields on tool_use
// blocks and the tool_use_id/content fields on tool_result blocks.
// But every Event carries the full JSON line in Raw, so we can parse
// our own content block view off Raw regardless of what the SDK
// exposes as typed fields.
//
// Pairing logic: we store pending tool_use entries in a per-session
// map keyed by ToolUseID. When the matching tool_result arrives
// (later in the same turn — assistant messages carry tool_use, user
// messages carry tool_result), we compute result/input hashes and
// emit a single EventToolInvocation.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// rawContentBlock mirrors the on-the-wire content block shape with
// all tool fields captured. Fields are omitempty so one struct covers
// text, thinking, tool_use, and tool_result blocks.
type rawContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// rawAssistantOrUserEnvelope matches both assistant and user messages.
// The CLI emits both shapes with { type, message: { role, content[] } }.
type rawAssistantOrUserEnvelope struct {
	Type    string `json:"type"`
	Message struct {
		Role    string            `json:"role"`
		Content []rawContentBlock `json:"content"`
	} `json:"message"`
}

// toolPendingMap tracks tool_use blocks seen before their matching
// tool_result arrives. Keyed by ToolUseID.
type toolPending struct {
	name      string
	inputHash string
}

// toolEventTracker is held per SDKSession; thread-safe.
type toolEventTracker struct {
	mu      sync.Mutex
	pending map[string]*toolPending
}

func newToolEventTracker() *toolEventTracker {
	return &toolEventTracker{pending: map[string]*toolPending{}}
}

// observeAssistant walks assistant content blocks and stashes any
// tool_use entries. Called whenever event.Type == TypeAssistant.
func (t *toolEventTracker) observeAssistant(raw []byte) {
	blocks := parseContentBlocks(raw)
	if len(blocks) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range blocks {
		if b.Type != "tool_use" || b.ID == "" {
			continue
		}
		t.pending[b.ID] = &toolPending{
			name:      b.Name,
			inputHash: hashCanonicalJSON(b.Input),
		}
	}
}

// observeUser walks user content blocks for tool_result entries and
// returns any that matched a pending tool_use. Caller should emit an
// EventToolInvocation for each returned value. Called whenever
// event.Type is a user-shaped message with tool_result blocks.
func (t *toolEventTracker) observeUser(raw []byte) []toolInvocationEmit {
	blocks := parseContentBlocks(raw)
	if len(blocks) == 0 {
		return nil
	}
	var out []toolInvocationEmit
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range blocks {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		pend, ok := t.pending[b.ToolUseID]
		if !ok {
			continue
		}
		delete(t.pending, b.ToolUseID)
		resultHash := hashToolResultContent(b.Content)
		out = append(out, toolInvocationEmit{
			Name:       pend.name,
			ToolUseID:  b.ToolUseID,
			InputHash:  pend.inputHash,
			ResultHash: resultHash,
			IsError:    b.IsError,
		})
	}
	return out
}

// reset clears any pending tool_use entries — called at turn
// boundaries so stale pairings don't leak across turns.
// Uses clear() to reuse the existing map allocation rather than
// reallocating; safe because no caller holds a reference to the
// map itself (observeAssistant/observeUser work with values, not
// the map pointer).
func (t *toolEventTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.pending)
}

type toolInvocationEmit struct {
	Name       string
	ToolUseID  string
	InputHash  string
	ResultHash string
	IsError    bool
}

// parseContentBlocks extracts the content[] array from an assistant
// or user message envelope. Returns nil if the raw bytes aren't a
// recognized envelope shape — lets the caller ignore events that
// don't carry content blocks without erroring.
//
// R21 code review (third pass): emit a Debug log on parse failure
// so format regressions in the upstream CLI become diagnosable
// without flooding production logs. Raw length only, not content,
// to keep noise down.
func parseContentBlocks(raw []byte) []rawContentBlock {
	var env rawAssistantOrUserEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		slog.Debug("tool_event_parser: failed to parse content blocks",
			"err", err, "rawLen", len(raw))
		return nil
	}
	return env.Message.Content
}

// hashCanonicalJSON returns a hex SHA-256 over a stable key-sorted
// canonical form of a JSON value. Uses a shallow canonicalization
// (sort top-level object keys); good enough for tool inputs which
// are mostly flat {command: "...", path: "..."} shapes. Returns ""
// for empty input.
func hashCanonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	canon := canonicalize(raw)
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])
}

// hashToolResultContent handles both string and array shapes for
// tool_result.content. The CLI sometimes emits a string, sometimes
// an array of {type:"text", text:"..."} blocks. Flatten to a single
// string then hash.
func hashToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	// Try array of text blocks.
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var b strings.Builder
		for _, item := range arr {
			if item.Type == "text" {
				b.WriteString(item.Text)
			}
		}
		sum := sha256.Sum256([]byte(b.String()))
		return hex.EncodeToString(sum[:])
	}
	// Fallback: hash the raw bytes directly.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// canonicalize produces a stable string form of a JSON value. Top
// level must be an object, array, or scalar. Object keys are sorted
// alphabetically so {"a":1,"b":2} and {"b":2,"a":1} canonicalize to
// the same string. Not truly recursive on nested objects in this
// implementation — tool inputs in practice are one level deep; if
// deeper canonicalization is ever needed, swap in a recursive walker.
func canonicalize(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] != '{' {
		// Not an object — scalars and arrays are already canonical
		// enough for hashing purposes.
		return trimmed
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return trimmed
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(k))
		b.WriteByte(':')
		b.Write(m[k])
	}
	b.WriteByte('}')
	return b.String()
}
