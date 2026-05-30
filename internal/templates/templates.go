package templates

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Template represents a reusable prompt template.
type Template struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Store manages template persistence on disk.
type Store struct {
	mu        sync.RWMutex
	dir       string
	templates map[string]*Template
}

// NewStore creates a new template store backed by the given directory.
func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	s := &Store{dir: dir, templates: make(map[string]*Template)}
	s.load()
	return s
}

// copyTemplate returns a value copy of t (Template has no reference-type fields
// beyond strings, so a struct copy fully isolates it). Returns nil for nil.
func copyTemplate(t *Template) *Template {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// List returns all templates sorted by name. The returned pointers are
// immutable snapshots -- safe to JSON-encode in a handler after the lock is
// released, even while another request mutates the store.
func (s *Store) List() []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

// listLocked returns COPIES of all templates sorted by name. Caller must hold
// at least an RLock. Copies (not live pointers) so handlers encoding the result
// after releasing the lock can't race an in-place Update.
func (s *Store) listLocked() []*Template {
	result := make([]*Template, 0, len(s.templates))
	for _, t := range s.templates {
		result = append(result, copyTemplate(t))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Get returns a COPY of the template by ID, or nil if not found. A copy so the
// caller can encode it outside the lock without racing a concurrent Update.
func (s *Store) Get(id string) *Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyTemplate(s.templates[id])
}

// Create adds a new template and persists it.
func (s *Store) Create(name, description, content string) *Template {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &Template{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		Content:     content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.templates[t.ID] = t
	s.save()
	return copyTemplate(t)
}

// Update applies the given field updates to a template. Returns nil if not
// found. Replaces the map entry with a FRESH allocation rather than mutating
// the existing struct in place, so any pointer previously handed out by
// Get/List (and being JSON-encoded in another goroutine) is never written to.
func (s *Store) Update(id string, updates map[string]string) *Template {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.templates[id]
	if existing == nil {
		return nil
	}
	t := copyTemplate(existing)
	if v, ok := updates["name"]; ok {
		t.Name = v
	}
	if v, ok := updates["description"]; ok {
		t.Description = v
	}
	if v, ok := updates["content"]; ok {
		t.Content = v
	}
	t.UpdatedAt = time.Now()
	s.templates[id] = t
	s.save()
	return copyTemplate(t)
}

// Delete removes a template by ID. Returns false if not found.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[id]; !ok {
		return false
	}
	delete(s.templates, id)
	s.save()
	return true
}

// Duplicate creates a copy of the template with " (copy)" appended to the name.
func (s *Store) Duplicate(id string) *Template {
	s.mu.RLock()
	orig := s.templates[id]
	if orig == nil {
		s.mu.RUnlock()
		return nil
	}
	name := orig.Name + " (copy)"
	description := orig.Description
	content := orig.Content
	s.mu.RUnlock()
	return s.Create(name, description, content)
}

func (s *Store) load() {
	path := filepath.Join(s.dir, "templates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var list []*Template
	// A corrupted templates file (crash mid-write, hand-edit mistake,
	// upstream struct change) should surface -- otherwise the operator
	// sees an empty template list with no hint why.
	if err := json.Unmarshal(data, &list); err != nil {
		slog.Warn("templates store: failed to unmarshal on load", "path", path, "err", err)
		return
	}
	for _, t := range list {
		s.templates[t.ID] = t
	}
}

// save persists templates to disk. Called from within locked methods; no additional lock needed.
// Uses temp-file + rename so a crash mid-write can't corrupt the file.
// Errors are logged rather than returned -- the public API doesn't
// surface errors and threading them through every caller is out of
// scope; logging is enough for the operator to see a disk-full /
// permission failure instead of silent data loss.
func (s *Store) save() {
	path := filepath.Join(s.dir, "templates.json")
	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		slog.Error("templates store: failed to marshal", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		slog.Error("templates store: failed to write tmp file", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Error("templates store: failed to rename tmp into place", "path", path, "err", err)
	}
}
