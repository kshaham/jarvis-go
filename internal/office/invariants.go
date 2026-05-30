package office

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxInvariants      = 25
	maxInvariantLen    = 300
	invariantsFileName = "project-invariants.json"
)

func projectInvariantsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis", "office-memory", invariantsFileName)
}

// GetProjectInvariants returns a copy of the current invariants.
func (m *Manager) GetProjectInvariants() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.projectInvariants))
	copy(out, m.projectInvariants)
	return out
}

func sanitizeInvariants(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		if len(t) > maxInvariantLen {
			t = t[:maxInvariantLen]
		}
		out = append(out, t)
		if len(out) >= maxInvariants {
			break
		}
	}
	return out
}

// SetProjectInvariants replaces the list (trimmed, de-blanked, capped) and persists.
func (m *Manager) SetProjectInvariants(items []string) {
	clean := sanitizeInvariants(items)
	m.mu.Lock()
	m.projectInvariants = clean
	m.mu.Unlock()
	m.persistProjectInvariants(clean)
}

// AddProjectInvariant appends a single invariant to the list and persists.
func (m *Manager) AddProjectInvariant(text string) {
	m.mu.Lock()
	next := sanitizeInvariants(append(append([]string{}, m.projectInvariants...), text))
	m.projectInvariants = next
	m.mu.Unlock()
	m.persistProjectInvariants(next)
}

// RemoveProjectInvariant removes the invariant at the given index and persists.
func (m *Manager) RemoveProjectInvariant(index int) error {
	m.mu.Lock()
	if index < 0 || index >= len(m.projectInvariants) {
		m.mu.Unlock()
		return errors.New("invariant index out of range")
	}
	next := append(append([]string{}, m.projectInvariants[:index]...), m.projectInvariants[index+1:]...)
	m.projectInvariants = next
	m.mu.Unlock()
	m.persistProjectInvariants(next)
	return nil
}

func (m *Manager) persistProjectInvariants(items []string) {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	path := projectInvariantsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
	// Nil-guard so bare &Manager{} used in unit tests (nil broadcast) does
	// not panic. The mission store broadcasts unconditionally because
	// NewManager always sets a non-nil broadcast; we guard here because
	// these methods are called directly from tests with zero-value managers.
	if m.broadcast != nil {
		m.broadcast("office:projectInvariantsUpdated", map[string]interface{}{"invariants": items})
	}
}

func (m *Manager) loadPersistedInvariants() {
	data, err := os.ReadFile(projectInvariantsPath())
	if err != nil {
		return
	}
	var items []string
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	m.mu.Lock()
	m.projectInvariants = sanitizeInvariants(items)
	m.mu.Unlock()
}
