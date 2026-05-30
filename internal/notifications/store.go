package notifications

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by store mutators when the route ID doesn't exist.
var ErrNotFound = errors.New("notification route not found")

// Store persists Route entries to a single JSON file with backup rotation
// mirroring internal/scheduler/store.go. Safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	path   string
	routes map[string]*Route
}

// NewStore opens or creates the backing file and loads any persisted routes.
func NewStore(path string) *Store {
	s := &Store{path: path, routes: make(map[string]*Route)}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		slog.Error("notifications: mkdir failed", "error", err)
	}
	s.load()
	return s
}

// List returns a stable snapshot sorted by Name.
func (s *Store) List() []*Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Route, 0, len(s.routes))
	for _, r := range s.routes {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a copy of the route, or nil if not found.
func (s *Store) Get(id string) *Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// Create assigns an ID and timestamps, persists, and returns the new route.
func (s *Store) Create(r Route) *Route {
	now := time.Now()
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	r.CreatedAt = now
	r.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	stored := r
	s.routes[stored.ID] = &stored
	s.save()
	cp := stored
	return &cp
}

// Update replaces mutable fields. Returns ErrNotFound if the ID doesn't exist.
func (s *Store) Update(id string, updated Route) (*Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.routes[id]
	if !ok {
		return nil, ErrNotFound
	}
	existing.Name = updated.Name
	existing.Enabled = updated.Enabled
	existing.Match = updated.Match
	existing.Channels = updated.Channels
	existing.UpdatedAt = time.Now()
	s.save()
	cp := *existing
	return &cp, nil
}

// Delete removes a route.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.routes[id]; !ok {
		return ErrNotFound
	}
	delete(s.routes, id)
	s.save()
	return nil
}

// ---- persistence (mirrors scheduler.Store pattern) ----

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if backup, berr := os.ReadFile(s.path + ".backup"); berr == nil {
			slog.Warn("notifications: primary missing/unreadable, using backup", "path", s.path)
			data = backup
		} else {
			return
		}
	}
	var list []*Route
	if err := json.Unmarshal(data, &list); err != nil {
		corrupt := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405")
		if renameErr := os.Rename(s.path, corrupt); renameErr == nil {
			slog.Error("notifications: primary corrupt, preserved copy", "corrupt", corrupt)
		}
		if backup, berr := os.ReadFile(s.path + ".backup"); berr == nil {
			if err := json.Unmarshal(backup, &list); err != nil {
				slog.Error("notifications: backup also unreadable, starting blank", "error", err)
				return
			}
		} else {
			return
		}
	}
	s.mu.Lock()
	for _, r := range list {
		s.routes[r.ID] = r
	}
	s.mu.Unlock()
}

func (s *Store) save() {
	list := make([]*Route, 0, len(s.routes))
	for _, r := range s.routes {
		list = append(list, r)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		slog.Error("notifications: marshal failed", "error", err)
		return
	}
	// Stage the new data in tmp FIRST. Only after it is safely written do we
	// rotate the live primary to .backup -- rotating before the write (the old
	// order) destroyed the live file if WriteFile failed, leaving only a stale
	// backup (or, on the first save, nothing).
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		slog.Error("notifications: write failed", "error", err)
		return
	}
	if _, err := os.Stat(s.path); err == nil {
		if rerr := os.Rename(s.path, s.path+".backup"); rerr != nil {
			slog.Warn("notifications: backup rotation failed", "error", rerr)
		}
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Error("notifications: rename failed", "error", err)
		os.Remove(tmp)
		// Restore the primary by COPYING the rotated backup (not renaming it):
		// load() falls back to .backup when the primary is unreadable or corrupt,
		// so that file must survive. Renaming it back would leave no recovery
		// copy until the next successful save.
		if b, rerr := os.ReadFile(s.path + ".backup"); rerr == nil {
			if werr := os.WriteFile(s.path, b, 0600); werr != nil {
				slog.Error("notifications: restore from backup failed", "error", werr)
			}
		}
	}
}
