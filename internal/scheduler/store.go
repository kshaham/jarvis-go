package scheduler

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

// ErrNotFound is returned by Store mutators when the job ID doesn't exist.
var ErrNotFound = errors.New("scheduled job not found")

// ErrDuplicateID is returned by Create when a job ID collision occurs.
var ErrDuplicateID = errors.New("scheduled job with that id already exists")

// Store persists scheduler.Job entries to a single JSON file. Safe for
// concurrent use; reads take an RLock, mutations take a Lock.
type Store struct {
	mu   sync.RWMutex
	path string
	jobs map[string]*Job
}

// NewStore opens/creates the backing file and loads any persisted jobs.
// Falls back to .backup if the primary file is corrupt, matching the
// state.store recovery pattern.
func NewStore(path string) *Store {
	s := &Store{path: path, jobs: make(map[string]*Job)}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		slog.Error("scheduler: mkdir failed", "error", err)
	}
	s.load()
	return s
}

// List returns a snapshot of jobs sorted by Name (stable order for UIs).
func (s *Store) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a copy of the job, or nil if not found.
func (s *Store) Get(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

// Create assigns a UUID + timestamps and persists the job.
func (s *Store) Create(j Job) (*Job, error) {
	now := time.Now()
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	j.CreatedAt = now
	j.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[j.ID]; exists {
		return nil, ErrDuplicateID
	}
	stored := j
	s.jobs[stored.ID] = &stored
	s.save()
	cp := stored
	return &cp, nil
}

// Update replaces mutable fields. Returns ErrNotFound if the ID doesn't
// exist. CreatedAt / ID are preserved.
func (s *Store) Update(id string, updated Job) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	existing.Name = updated.Name
	existing.Cron = updated.Cron
	existing.Action = updated.Action
	existing.Config = updated.Config
	existing.Enabled = updated.Enabled
	existing.UpdatedAt = time.Now()
	s.save()
	cp := *existing
	return &cp, nil
}

// Delete removes the job. Returns ErrNotFound if absent.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return ErrNotFound
	}
	delete(s.jobs, id)
	s.save()
	return nil
}

// UpdateRunStatus records a run's outcome into the job's persisted fields.
// Used by the Runner after each execution.
func (s *Store) UpdateRunStatus(id string, at time.Time, status, errStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return
	}
	ts := at
	j.LastRunAt = &ts
	j.LastStatus = status
	j.LastError = errStr
	j.UpdatedAt = time.Now()
	s.save()
}

// SetNextRun records the upcoming fire time for display purposes.
// Persists to disk so the UI's "next run" timestamp survives a restart
// (the sibling UpdateRunStatus already does this; SetNextRun was missed).
func (s *Store) SetNextRun(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		ts := at
		j.NextRunAt = &ts
		s.save()
	}
}

// ---- persistence ----

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// Try backup.
		if backup, berr := os.ReadFile(s.path + ".backup"); berr == nil {
			slog.Warn("scheduler: primary missing/unreadable, using backup", "path", s.path)
			data = backup
		} else {
			return
		}
	}
	var list []*Job
	if err := json.Unmarshal(data, &list); err != nil {
		// Primary corrupt -- preserve it and try backup.
		corrupt := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405")
		if renameErr := os.Rename(s.path, corrupt); renameErr == nil {
			slog.Error("scheduler: primary corrupt, preserved copy", "corrupt", corrupt)
		}
		if backup, berr := os.ReadFile(s.path + ".backup"); berr == nil {
			if err := json.Unmarshal(backup, &list); err != nil {
				slog.Error("scheduler: backup also unreadable, starting blank", "error", err)
				return
			}
			slog.Warn("scheduler: restored jobs from backup", "count", len(list))
		} else {
			return
		}
	}
	s.mu.Lock()
	for _, j := range list {
		s.jobs[j.ID] = j
	}
	s.mu.Unlock()
}

func (s *Store) save() {
	list := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		list = append(list, j)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		slog.Error("scheduler: marshal failed", "error", err)
		return
	}
	// Stage the new data in tmp FIRST, then rotate the live primary to .backup
	// (last-known-good copy) only once the write succeeded -- rotating before
	// the write destroyed the live file if WriteFile failed.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		slog.Error("scheduler: write failed", "error", err)
		return
	}
	if _, err := os.Stat(s.path); err == nil {
		if rerr := os.Rename(s.path, s.path+".backup"); rerr != nil {
			slog.Warn("scheduler: backup rotation failed", "error", rerr)
		}
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Error("scheduler: rename failed", "error", err)
		os.Remove(tmp)
		// Restore the rotated backup so the primary isn't left missing.
		if _, berr := os.Stat(s.path + ".backup"); berr == nil {
			_ = os.Rename(s.path+".backup", s.path)
		}
	}
}
