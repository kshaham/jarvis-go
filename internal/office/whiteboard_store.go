package office

// Persisted team whiteboard (R17).
//
// The legacy whiteboard lived only on AgentState.WhiteboardNote — that's
// per-session and in-memory, so it died whenever a session restarted.
// Teams using the whiteboard as a shared scratchpad lost everything on
// wrap-up.
//
// TeamWhiteboard is the disk-backed replacement: keyed by AGENT NAME
// (not session ID) so a respawn of the same role inherits the old
// notes, with a sections map so agents can track separate buckets
// (`tracking`, `blockers`, `decisions`) rather than overwriting a single
// content blob. Persisted at ~/.jarvis/office-memory/whiteboard.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TeamWhiteboardSection is a named bucket on an agent's whiteboard
// entry. Content is plain text (markdown allowed, not enforced).
type TeamWhiteboardSection struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TeamWhiteboardEntry is one agent's whiteboard: a map of named
// sections plus a "default" section for back-compat with the legacy
// single-content model.
type TeamWhiteboardEntry struct {
	Sections map[string]TeamWhiteboardSection `json:"sections"`
}

// TeamWhiteboard is the full persisted whiteboard keyed by agent name.
type TeamWhiteboard struct {
	Entries map[string]TeamWhiteboardEntry `json:"entries"`
}

// WhiteboardStore wraps TeamWhiteboard with a mutex + disk persistence.
// One instance lives on the Manager; all mutations go through here so
// concurrent writes from the HTTP handlers don't corrupt the file.
type WhiteboardStore struct {
	mu   sync.RWMutex
	data TeamWhiteboard
	path string
}

// defaultWhiteboardSection is the fallback section name for legacy
// callers that POST a plain content blob without specifying a section.
const defaultWhiteboardSection = "notes"

// NewWhiteboardStore loads the whiteboard from disk (or initializes an
// empty one on miss) and returns a ready-to-use store.
func NewWhiteboardStore() *WhiteboardStore {
	homeDir, _ := os.UserHomeDir()
	path := filepath.Join(homeDir, ".jarvis", "office-memory", "whiteboard.json")
	store := &WhiteboardStore{path: path}
	store.load()
	return store
}

// load reads the JSON file and populates the in-memory copy. Missing
// file is fine — we just start empty.
func (s *WhiteboardStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.data = TeamWhiteboard{Entries: map[string]TeamWhiteboardEntry{}}
		return
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		s.data = TeamWhiteboard{Entries: map[string]TeamWhiteboardEntry{}}
		return
	}
	if s.data.Entries == nil {
		s.data.Entries = map[string]TeamWhiteboardEntry{}
	}
}

// saveLocked serializes the current in-memory state to disk. Caller
// must hold the write lock. Uses atomic rename so concurrent readers
// never see a half-written file.
func (s *WhiteboardStore) saveLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// Set writes or replaces a section's content for an agent. Blank
// section defaults to "notes".
func (s *WhiteboardStore) Set(agentName, section, content string) {
	if agentName == "" {
		return
	}
	if section == "" {
		section = defaultWhiteboardSection
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.data.Entries[agentName]
	if entry.Sections == nil {
		entry.Sections = map[string]TeamWhiteboardSection{}
	}
	entry.Sections[section] = TeamWhiteboardSection{
		Content:   content,
		UpdatedAt: time.Now().UTC(),
	}
	s.data.Entries[agentName] = entry
	s.saveLocked()
}

// Append concatenates new content onto an existing section, separated
// by a newline. Creates the section if it doesn't exist yet.
func (s *WhiteboardStore) Append(agentName, section, line string) {
	if agentName == "" {
		return
	}
	if section == "" {
		section = defaultWhiteboardSection
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.data.Entries[agentName]
	if entry.Sections == nil {
		entry.Sections = map[string]TeamWhiteboardSection{}
	}
	prev := entry.Sections[section]
	newContent := prev.Content
	if newContent != "" {
		newContent += "\n"
	}
	newContent += line
	entry.Sections[section] = TeamWhiteboardSection{
		Content:   newContent,
		UpdatedAt: time.Now().UTC(),
	}
	s.data.Entries[agentName] = entry
	s.saveLocked()
}

// Snapshot returns a deep copy of the whiteboard safe for read-only use
// by HTTP handlers without holding the lock.
func (s *WhiteboardStore) Snapshot() TeamWhiteboard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := TeamWhiteboard{Entries: make(map[string]TeamWhiteboardEntry, len(s.data.Entries))}
	for k, v := range s.data.Entries {
		sectionsCopy := make(map[string]TeamWhiteboardSection, len(v.Sections))
		for sk, sv := range v.Sections {
			sectionsCopy[sk] = sv
		}
		out.Entries[k] = TeamWhiteboardEntry{Sections: sectionsCopy}
	}
	return out
}
