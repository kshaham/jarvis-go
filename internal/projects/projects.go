// Package projects owns the operator's saved-project list — the
// durable, server-backed replacement for the spawn page's old
// localStorage "recent workdirs" chips. The list is persisted under
// the "savedProjects" settings key so it follows the operator across
// browsers and devices (the server deployment runs a single shared
// instance). Dedup, cap, sort, and name derivation all live here so
// every writer (today: the spawn handler's auto-save) stays
// consistent.
package projects

import (
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
)

// settingsKey is the key under which the project array is stored in
// the shared settings map.
const settingsKey = "savedProjects"

// maxProjects caps the stored list; the oldest lastUsedAt is evicted
// past this. Twenty is plenty for one operator's working set without
// turning the dropdown into a scroll.
const maxProjects = 20

// Kind enumerates the two project flavours.
const (
	KindLocal  = "local"
	KindGitHub = "github"
)

// Project is one saved entry. For local projects Path is the workdir;
// for github projects RepoURL is the canonical https clone URL and
// Path is filled in after the first successful clone.
type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	RepoURL    string `json:"repoUrl,omitempty"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

// SettingsStore is the minimal surface the service needs from
// state.AppState. Declared here (consumer side) so the projects
// package imports nothing from internal/state — no cycle.
type SettingsStore interface {
	GetSettings() map[string]interface{}
	UpdateSettings(updates map[string]interface{})
}

// Service reads and mutates the saved-project list.
type Service struct {
	store SettingsStore
}

// New constructs a Service over the given settings store.
func New(store SettingsStore) *Service { return &Service{store: store} }

// load reads and decodes the project array, tolerating both the
// in-memory []Project form and the []interface{} form that survives a
// JSON round-trip through the on-disk state file.
func (s *Service) load() []Project {
	raw, ok := s.store.GetSettings()[settingsKey]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []Project
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// sortProjects orders by lastUsedAt desc, then createdAt desc, then id
// — a total order so cap eviction and display are deterministic even
// at 1-second RFC3339 granularity.
func sortProjects(ps []Project) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].LastUsedAt != ps[j].LastUsedAt {
			return ps[i].LastUsedAt > ps[j].LastUsedAt
		}
		if ps[i].CreatedAt != ps[j].CreatedAt {
			return ps[i].CreatedAt > ps[j].CreatedAt
		}
		return ps[i].ID > ps[j].ID
	})
}

// dedupKey is the identity used to collapse re-saves: path for local,
// normalized repoUrl for github.
func dedupKey(p Project) string {
	if p.Kind == KindGitHub {
		return KindGitHub + "|" + p.RepoURL
	}
	return KindLocal + "|" + p.Path
}

// DeriveName produces a friendly display name when one isn't supplied:
// the folder basename for local projects, or the caller-provided
// owner/repo for github. Exported so the spawn handler can reuse it.
func DeriveName(p Project) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Kind == KindGitHub {
		return p.RepoURL
	}
	return filepath.Base(p.Path)
}

// List returns the saved projects, most-recently-used first.
func (s *Service) List() []Project {
	ps := s.load()
	sortProjects(ps)
	return ps
}

// Upsert inserts or updates a project keyed by dedupKey, deriving a
// name when absent, assigning id/createdAt for new entries, advancing
// lastUsedAt, then re-sorting and applying the cap. Returns the stored
// record. The caller supplies LastUsedAt (so tests are deterministic
// and the handler can pass time.Now().UTC().Format(time.RFC3339)).
func (s *Service) Upsert(p Project) Project {
	if p.Kind == "" {
		p.Kind = KindLocal
	}
	p.Name = DeriveName(p)
	list := s.load()
	key := dedupKey(p)
	found := -1
	for i := range list {
		if dedupKey(list[i]) == key {
			found = i
			break
		}
	}
	if found >= 0 {
		existing := list[found]
		p.ID = existing.ID
		p.CreatedAt = existing.CreatedAt
		if p.Path == "" {
			p.Path = existing.Path // never blank out a known clone path
		}
		list[found] = p
	} else {
		p.ID = uuid.NewString()
		if p.CreatedAt == "" {
			p.CreatedAt = p.LastUsedAt
		}
		list = append(list, p)
	}
	sortProjects(list)
	if len(list) > maxProjects {
		list = list[:maxProjects]
	}
	s.persist(list)
	if found >= 0 {
		return list[indexByID(list, p.ID)]
	}
	return p
}

func indexByID(list []Project, id string) int {
	for i := range list {
		if list[i].ID == id {
			return i
		}
	}
	return 0
}

func (s *Service) persist(list []Project) {
	s.store.UpdateSettings(map[string]interface{}{settingsKey: list})
}

// Delete removes the project with the given id. Unknown ids are a
// silent no-op.
func (s *Service) Delete(id string) {
	list := s.load()
	out := list[:0]
	for _, p := range list {
		if p.ID != id {
			out = append(out, p)
		}
	}
	s.persist(out)
}
