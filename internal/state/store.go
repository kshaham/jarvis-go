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

	filePath       string
	debounceTimer  *time.Timer
	debounceDirty  bool
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

// RecordDailyStats adds token usage to today's daily stats entry.
func (s *AppState) RecordDailyStats(input, output int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	for i, ds := range s.DailyStats {
		if ds.Date == today {
			s.DailyStats[i].Input += input
			s.DailyStats[i].Output += output
			s.DailyStats[i].Total = s.DailyStats[i].Input + s.DailyStats[i].Output
			s.save()
			return
		}
	}
	s.DailyStats = append(s.DailyStats, DailyTokenStats{
		Date:   today,
		Input:  input,
		Output: output,
		Total:  input + output,
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

func (s *AppState) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return // File doesn't exist yet
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.Unmarshal(data, s); err != nil {
		slog.Warn("Failed to unmarshal state", "error", err)
	}
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
func (s *AppState) flushLocked() {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal state", "error", err)
		return
	}
	dir := filepath.Dir(s.filePath)
	os.MkdirAll(dir, 0755)
	// Atomic write via temp file
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		slog.Error("Failed to write state", "error", err)
		return
	}
	os.Rename(tmp, s.filePath)
}
