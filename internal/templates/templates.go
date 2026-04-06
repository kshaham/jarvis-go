package templates

import (
	"encoding/json"
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

// List returns all templates sorted by name.
func (s *Store) List() []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

// listLocked returns all templates sorted by name. Caller must hold at least an RLock.
func (s *Store) listLocked() []*Template {
	result := make([]*Template, 0, len(s.templates))
	for _, t := range s.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Get returns a template by ID, or nil if not found.
func (s *Store) Get(id string) *Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates[id]
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
	return t
}

// Update applies the given field updates to a template. Returns nil if not found.
func (s *Store) Update(id string, updates map[string]string) *Template {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.templates[id]
	if t == nil {
		return nil
	}
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
	s.save()
	return t
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
	data, err := os.ReadFile(filepath.Join(s.dir, "templates.json"))
	if err != nil {
		return
	}
	var list []*Template
	json.Unmarshal(data, &list)
	for _, t := range list {
		s.templates[t.ID] = t
	}
}

// save persists templates to disk. Called from within locked methods; no additional lock needed.
func (s *Store) save() {
	data, _ := json.MarshalIndent(s.listLocked(), "", "  ")
	os.WriteFile(filepath.Join(s.dir, "templates.json"), data, 0644)
}
