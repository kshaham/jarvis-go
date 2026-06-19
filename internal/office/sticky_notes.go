package office

// Agent sticky notes (Feature B).
//
// The legacy whiteboard (whiteboard_store.go) stores ONE freeform note
// per agent, overwrite-on-write. That's a scratchpad, not a feed: a
// team's running thinking -- decisions, blockers, milestones, hand-offs,
// findings -- has nowhere to accumulate where operators can browse it.
//
// StickyNotes is the append-only feed: every `cm whiteboard "<note>"`
// adds a new immutable sticky note authored by the calling agent. They
// render as colored cards on the Whiteboard screen AND get merged into
// the Reports library so a run's narrative is visible after the fact.
//
// Persisted at ~/.jarvis/office-memory/sticky-notes.json, mirroring the
// whiteboard.json read/write pattern (atomic rename, tolerate a missing
// file). Capped at the last stickyNotesMaxKeep entries so a long-lived
// perpetual team can't grow the file without bound.

import (
	"encoding/json"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// StickyNote is one immutable note authored by an agent. Color is a
// CSS-ready hex string; CreatedAt is UTC.
type StickyNote struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Color     string    `json:"color,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// stickyNotesMaxKeep caps the in-memory + persisted list. Oldest notes
// front-drop past this bound. Matches the spirit of WrapUpReportsMaxKeep
// and HandoffAuditCap -- bounded even under heavy perpetual use.
const stickyNotesMaxKeep = 500

// stickyNotePalette is the fixed set of card backgrounds. An author's
// color is chosen by hashing their name so the same agent is always the
// same color across notes (consistent at-a-glance attribution).
var stickyNotePalette = []string{
	"#fef3c7", // amber
	"#dbeafe", // blue
	"#dcfce7", // green
	"#fce7f3", // pink
	"#ede9fe", // violet
	"#ffedd5", // orange
	"#cffafe", // cyan
	"#fee2e2", // red
}

// colorForAuthor returns a stable palette color for an author name.
// Empty author falls back to the first palette entry.
func colorForAuthor(author string) string {
	if author == "" {
		return stickyNotePalette[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(author))
	return stickyNotePalette[int(h.Sum32())%len(stickyNotePalette)]
}

// stickyNotesPath returns the canonical sticky-notes file path. Empty
// string when there's no home directory (mirrors WrapUpReportsDir).
func stickyNotesPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "sticky-notes.json")
}

// loadStickyNotesLocked lazily loads the persisted notes into the
// manager on first access. Caller must hold m.mu. A missing or
// unparseable file yields an empty list (never an error -- the feed
// should degrade quietly, not block agents from posting).
func (m *Manager) loadStickyNotesLocked() {
	if m.stickyNotesLoaded {
		return
	}
	m.stickyNotesLoaded = true
	path := stickyNotesPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var notes []StickyNote
	if err := json.Unmarshal(data, &notes); err != nil {
		return
	}
	m.stickyNotes = notes
}

// saveStickyNotesLocked serializes the current list to disk via atomic
// rename. Caller must hold m.mu. Failures are silent -- the in-memory
// list is still authoritative for the running process.
func (m *Manager) saveStickyNotesLocked() {
	path := stickyNotesPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(m.stickyNotes, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// AddStickyNote appends a new sticky note authored by the given agent,
// persists the list, and returns the created note. Author "" falls back
// to "agent". An empty color is assigned a stable per-author palette
// color. The list is trimmed to the last stickyNotesMaxKeep entries.
func (m *Manager) AddStickyNote(author, content, color string) StickyNote {
	if author == "" {
		author = "agent"
	}
	if color == "" {
		color = colorForAuthor(author)
	}
	note := StickyNote{
		ID:        uuid.New().String(),
		Author:    author,
		Content:   content,
		Color:     color,
		CreatedAt: time.Now().UTC(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadStickyNotesLocked()
	m.stickyNotes = append(m.stickyNotes, note)
	// Front-drop oldest past the cap so the file stays bounded.
	if len(m.stickyNotes) > stickyNotesMaxKeep {
		m.stickyNotes = m.stickyNotes[len(m.stickyNotes)-stickyNotesMaxKeep:]
	}
	m.saveStickyNotesLocked()
	return note
}

// ListStickyNotes returns a copy of the sticky-note feed as stored
// (oldest-first). Callers sort/slice as needed. Safe for read-only use
// outside the lock.
func (m *Manager) ListStickyNotes() []StickyNote {
	m.mu.Lock()
	m.loadStickyNotesLocked()
	out := make([]StickyNote, len(m.stickyNotes))
	copy(out, m.stickyNotes)
	m.mu.Unlock()
	return out
}
