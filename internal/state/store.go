package state

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DailyTokenStats represents token usage for a single day.
type DailyTokenStats struct {
	Date   string  `json:"date"`
	Input  int64   `json:"input"`
	Output int64   `json:"output"`
	Total  int64   `json:"total"`
	Cost   float64 `json:"cost"`
}

// SessionState represents a persisted session record.
type SessionState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	WorkingDir string `json:"workingDir"`
	Mode       string `json:"mode"`
	CreatedAt  string `json:"createdAt"`
	Status     string `json:"status"`
}

// debouncePersistDelay is the window for batching write operations.
const debouncePersistDelay = 500 * time.Millisecond

// AppState represents the persisted application state.
type AppState struct {
	mu sync.RWMutex

	Settings   map[string]interface{}   `json:"settings"`
	Config     map[string]interface{}   `json:"config"`
	Sessions   map[string]*SessionState `json:"sessions"`
	DailyStats []DailyTokenStats        `json:"dailyStats"`
	Version    string                   `json:"version"`

	filePath      string
	debounceTimer *time.Timer
	debounceDirty bool
}

// NewStore creates or loads state from the given file path.
func NewStore(filePath string) *AppState {
	s := &AppState{
		Settings: make(map[string]interface{}),
		Config:   make(map[string]interface{}),
		Sessions: make(map[string]*SessionState),
		Version:  "0.2.0-go",
		filePath: filePath,
	}
	s.load()
	return s
}

// GetSettings returns a copy of settings.
func (s *AppState) GetSettings() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range s.Settings {
		result[k] = v
	}
	return result
}

// UpdateSettings merges new settings.
func (s *AppState) UpdateSettings(updates map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range updates {
		s.Settings[k] = v
	}
	s.save()
}

// GetConfig returns a copy of config.
func (s *AppState) GetConfig() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range s.Config {
		result[k] = v
	}
	return result
}

// UpdateConfig merges new config.
func (s *AppState) UpdateConfig(updates map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range updates {
		s.Config[k] = v
	}
	s.save()
}

// SaveSession persists a session record to state.
func (s *AppState) SaveSession(sess *SessionState) {
	s.mu.Lock()
	if s.Sessions == nil {
		s.Sessions = make(map[string]*SessionState)
	}
	s.Sessions[sess.ID] = sess
	s.save()
	s.mu.Unlock()
}

// RemoveSession deletes a session record from state.
func (s *AppState) RemoveSession(id string) {
	s.mu.Lock()
	delete(s.Sessions, id)
	s.save()
	s.mu.Unlock()
}

// GetSessions returns a copy of all persisted sessions.
func (s *AppState) GetSessions() map[string]*SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*SessionState)
	for k, v := range s.Sessions {
		result[k] = v
	}
	return result
}

// RecordDailyStats adds token usage to today's daily stats entry and updates
// the running dollar cost using the model-specific pricing table. Pass an
// empty model string to fall back to Sonnet-equivalent pricing.
func (s *AppState) RecordDailyStats(input, output int64, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	cost := ComputeCost(model, input, output)
	for i, ds := range s.DailyStats {
		if ds.Date == today {
			s.DailyStats[i].Input += input
			s.DailyStats[i].Output += output
			s.DailyStats[i].Total = s.DailyStats[i].Input + s.DailyStats[i].Output
			s.DailyStats[i].Cost += cost
			s.save()
			return
		}
	}
	s.DailyStats = append(s.DailyStats, DailyTokenStats{
		Date:   today,
		Input:  input,
		Output: output,
		Total:  input + output,
		Cost:   cost,
	})
	// Keep last 30 days
	if len(s.DailyStats) > 30 {
		s.DailyStats = s.DailyStats[len(s.DailyStats)-30:]
	}
	s.save()
}

// GetDailyStats returns a copy of the daily token stats.
func (s *AppState) GetDailyStats() []DailyTokenStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DailyTokenStats, len(s.DailyStats))
	copy(result, s.DailyStats)
	return result
}

// load attempts to restore state from disk. If the primary file is missing or
// corrupt, falls back to the .backup copy. A corrupted primary is preserved
// as state.json.corrupt-<unix> for forensic inspection and the empty defaults
// are retained rather than silently dropping all prior data.
func (s *AppState) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loadFromFile(s.filePath) {
		return
	}

	// Primary failed or missing. Move the corrupt primary aside (if present)
	// so we don't overwrite it on the next flush.
	if _, err := os.Stat(s.filePath); err == nil {
		corruptPath := s.filePath + ".corrupt-" + time.Now().UTC().Format("20060102T150405")
		if renameErr := os.Rename(s.filePath, corruptPath); renameErr == nil {
			slog.Error("State file was corrupt, preserved copy", "corrupt", corruptPath)
		} else {
			slog.Error("Failed to preserve corrupt state file", "error", renameErr)
		}
	}

	backupPath := s.filePath + ".backup"
	if s.loadFromFile(backupPath) {
		slog.Warn("Restored state from backup after primary failure", "backup", backupPath)
		return
	}
	// Both failed or don't exist -- start blank (maps were initialized in NewStore).
}

// loadFromFile reads + unmarshals the given path into s. Returns true on full
// success. Safe to call with s.mu already held.
func (s *AppState) loadFromFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, s); err != nil {
		slog.Error("Failed to unmarshal state", "path", path, "error", err)
		// Reset maps that may have been partially populated during unmarshal.
		s.Settings = make(map[string]interface{})
		s.Config = make(map[string]interface{})
		s.Sessions = make(map[string]*SessionState)
		s.DailyStats = nil
		return false
	}
	if s.Settings == nil {
		s.Settings = make(map[string]interface{})
	}
	if s.Config == nil {
		s.Config = make(map[string]interface{})
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]*SessionState)
	}
	return true
}

func (s *AppState) save() {
	// Called from within locked methods; no additional lock needed.
	// Use debounced persistence: instead of writing to disk on every change,
	// batch writes with a 500ms delay using a timer.
	s.debounceDirty = true
	if s.debounceTimer == nil {
		s.debounceTimer = time.AfterFunc(debouncePersistDelay, func() {
			s.mu.Lock()
			s.debounceTimer = nil
			if !s.debounceDirty {
				s.mu.Unlock()
				return
			}
			s.debounceDirty = false
			s.flushLocked()
			s.mu.Unlock()
		})
	}
}

// Flush forces an immediate write of any pending state to disk.
func (s *AppState) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.debounceTimer = nil
	}
	if s.debounceDirty {
		s.debounceDirty = false
		s.flushLocked()
	}
}

// flushLocked performs the actual disk write.
// Must be called with s.mu held.
//
// Rotation: before writing the new file, we rename the current state.json to
// state.json.backup. Combined with the loader's backup-fallback path, this
// guarantees at least one readable copy exists even if a flush is interrupted.
func (s *AppState) flushLocked() {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal state", "error", err)
		return
	}
	dir := filepath.Dir(s.filePath)
	os.MkdirAll(dir, 0700)

	// Rotate the existing file into the backup slot before overwriting. If the
	// main file doesn't exist yet (first flush ever), skip the rotation.
	if _, err := os.Stat(s.filePath); err == nil {
		backupPath := s.filePath + ".backup"
		if err := os.Rename(s.filePath, backupPath); err != nil {
			slog.Warn("Failed to rotate state backup (continuing)", "error", err)
		}
	}

	// Atomic write via temp file (0600: contains API keys)
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		slog.Error("Failed to write state", "error", err)
		return
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		slog.Error("Failed to rename state file", "error", err)
		os.Remove(tmp) // clean up temp file
	}
}
