package teamtemplates

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

// TeamTemplate represents a reusable team composition template.
type TeamTemplate struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Category    string               `json:"category"`
	IsBuiltIn   bool                 `json:"isBuiltIn"`
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
	Voice        string   `json:"voice,omitempty"`
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
		result = append(result, copyTemplate(t))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Get returns a custom template by ID, or nil if not found.
func (s *Store) Get(id string) *TeamTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyTemplate(s.templates[id])
}

// copyTemplate returns a deep-enough copy so callers never hold a pointer the
// store later mutates under its write lock in Update (which would be a data
// race: Get/List readers vs Update writers on the same *TeamTemplate). Copies
// the struct and the Members slice (Update replaces .Members wholesale).
func copyTemplate(t *TeamTemplate) *TeamTemplate {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Members != nil {
		cp.Members = make([]TeamTemplateMember, len(t.Members))
		copy(cp.Members, t.Members)
	}
	return &cp
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
	path := filepath.Join(s.dir, "team-templates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var list []*TeamTemplate
	// Surface unmarshal errors so a corrupted file (truncated prior
	// save, hand-edit mistake, struct-change incompatibility) doesn't
	// silently come back as "zero custom templates".
	if err := json.Unmarshal(data, &list); err != nil {
		slog.Warn("team-templates store: failed to unmarshal on load", "path", path, "err", err)
		// R27 · Fall back to .backup on parse failure. The backup is
		// rotated from the last successful save, so at worst we lose
		// the edits from the one save that crashed.
		backup := path + ".backup"
		if bdata, bErr := os.ReadFile(backup); bErr == nil {
			if bUmErr := json.Unmarshal(bdata, &list); bUmErr == nil {
				slog.Warn("team-templates store: loaded from .backup after primary parse failure",
					"backup", backup)
				for _, t := range list {
					s.templates[t.ID] = t
				}
			}
		}
		return
	}
	for _, t := range list {
		s.templates[t.ID] = t
	}
}

// save persists templates to disk with temp-file + rename so a
// crash mid-write can't corrupt the file. Errors are logged rather
// than returned -- see the matching note on templates.Store.save.
//
// R27 state review · Rotate the live file to .backup before writing
// the new version so a crash after rename-into-place but before the
// next successful save always leaves one known-good copy on disk.
// Matches the scheduler/store.go + notifications/store.go pattern.
// Cleanup of the .tmp file on rename failure mirrors R26 phase 4.
func (s *Store) save() {
	path := filepath.Join(s.dir, "team-templates.json")
	backup := path + ".backup"
	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		slog.Error("team-templates store: failed to marshal", "err", err)
		return
	}
	// Rotate current file to backup (best-effort; missing is fine for
	// first-write case).
	if _, statErr := os.Stat(path); statErr == nil {
		if renameErr := os.Rename(path, backup); renameErr != nil {
			slog.Warn("team-templates store: failed to rotate backup",
				"path", path, "err", renameErr)
			// fall through — prefer overwriting a slightly-stale
			// backup over losing new data.
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		slog.Error("team-templates store: failed to write tmp file", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Error("team-templates store: failed to rename tmp into place", "path", path, "err", err)
		_ = os.Remove(tmp)
	}
}
