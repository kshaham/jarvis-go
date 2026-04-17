package teamtemplates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TeamTemplate represents a reusable team composition template.
type TeamTemplate struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Category    string               `json:"category"`
	IsBuiltIn   bool                `json:"isBuiltIn"`
	Members     []TeamTemplateMember `json:"members"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

// TeamTemplateMember defines a single agent within a team template.
type TeamTemplateMember struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Tier         string   `json:"tier"`
	Color        string   `json:"color"`
	Emoji        string   `json:"emoji"`
	Personality  string   `json:"personality,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	WorkingDir   string   `json:"workingDir,omitempty"`
	// Provider + Model let custom templates pin a specific LLM per
	// member. Empty values mean "inherit from the team-level default
	// at spawn time" -- previously the only possible behavior, so
	// existing persisted templates still load correctly (omitempty).
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// Store manages custom team template persistence on disk.
type Store struct {
	mu        sync.RWMutex
	dir       string
	templates map[string]*TeamTemplate
}

// NewStore creates a new team template store backed by the given directory.
func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	s := &Store{dir: dir, templates: make(map[string]*TeamTemplate)}
	s.load()
	return s
}

// List returns all custom templates sorted by name.
func (s *Store) List() []*TeamTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []*TeamTemplate {
	result := make([]*TeamTemplate, 0, len(s.templates))
	for _, t := range s.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Get returns a custom template by ID, or nil if not found.
func (s *Store) Get(id string) *TeamTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates[id]
}

// Create adds a new custom team template and persists it.
func (s *Store) Create(tpl TeamTemplate) *TeamTemplate {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	t := &TeamTemplate{
		ID:          uuid.New().String(),
		Name:        tpl.Name,
		Description: tpl.Description,
		Category:    tpl.Category,
		IsBuiltIn:   false,
		Members:     tpl.Members,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if t.Category == "" {
		t.Category = "custom"
	}
	s.templates[t.ID] = t
	s.save()
	return t
}

// Update applies changes to a custom template. Returns nil if not found.
func (s *Store) Update(id string, tpl TeamTemplate) *TeamTemplate {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.templates[id]
	if existing == nil {
		return nil
	}
	existing.Name = tpl.Name
	existing.Description = tpl.Description
	existing.Category = tpl.Category
	existing.Members = tpl.Members
	existing.UpdatedAt = time.Now()
	if existing.Category == "" {
		existing.Category = "custom"
	}
	s.save()
	return existing
}

// Delete removes a custom template by ID. Returns false if not found.
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

// Duplicate creates a copy of a template with " (copy)" appended to the name.
// The source can be a custom template (looked up internally) or a built-in
// template passed via the builtIn parameter.
func (s *Store) Duplicate(id string, builtIn *TeamTemplate) *TeamTemplate {
	s.mu.RLock()
	src := s.templates[id]
	s.mu.RUnlock()

	if src == nil && builtIn != nil {
		src = builtIn
	}
	if src == nil {
		return nil
	}

	members := make([]TeamTemplateMember, len(src.Members))
	copy(members, src.Members)

	return s.Create(TeamTemplate{
		Name:        src.Name + " (copy)",
		Description: src.Description,
		Category:    src.Category,
		Members:     members,
	})
}

func (s *Store) load() {
	data, err := os.ReadFile(filepath.Join(s.dir, "team-templates.json"))
	if err != nil {
		return
	}
	var list []*TeamTemplate
	json.Unmarshal(data, &list)
	for _, t := range list {
		s.templates[t.ID] = t
	}
}

func (s *Store) save() {
	data, _ := json.MarshalIndent(s.listLocked(), "", "  ")
	os.WriteFile(filepath.Join(s.dir, "team-templates.json"), data, 0644)
}
