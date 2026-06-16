package office

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxInvariants      = 25
	maxInvariantLen    = 300
	invariantsFileName = "project-invariants.json"
)

// projectInvariantsPath returns the invariants file path, or "" when the home
// directory can't be resolved. Callers MUST treat "" as "skip persistence":
// a relative path would otherwise make agents in different working dirs write
// to divergent .jarvis/ locations, silently losing invariants across respawns.
func projectInvariantsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
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
			// Back up to a rune boundary so a multibyte rune isn't split into
			// invalid UTF-8 in the persisted JSON / injected prompt.
			limit := maxInvariantLen
			for limit > 0 && !utf8.RuneStart(t[limit]) {
				limit--
			}
			t = t[:limit]
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
	if path == "" {
		slog.Warn("cannot persist project invariants: home dir unavailable")
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // don't leak the orphan tmp on a full/locked disk
		slog.Warn("failed to persist project invariants (rename)", "err", err)
		return
	}
	// Nil-guard so bare &Manager{} used in unit tests (nil broadcast) does
	// not panic. The mission store broadcasts unconditionally because
	// NewManager always sets a non-nil broadcast; we guard here because
	// these methods are called directly from tests with zero-value managers.
	if m.broadcast != nil {
		m.broadcast("office:projectInvariantsUpdated", map[string]interface{}{"invariants": items})
	}
}

// renderInvariantsBlock formats a slice of invariant strings into a prompt
// section. Returns empty string when items is nil or empty, so callers can
// gate on the return value without an extra length check.
func renderInvariantsBlock(items []string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Project Invariants (Non-Negotiable Rules)\n")
	total := 0
	for _, it := range items {
		line := "- " + it + "\n"
		if total+len(line) > 1500 { // cap rendered block to protect context
			break
		}
		b.WriteString(line)
		total += len(line)
	}
	b.WriteString("\n")
	return b.String()
}

// refreshProjectInvariants re-reads the persisted invariants from disk and, if
// they changed since the in-memory copy, updates the cache and broadcasts.
// Loop-engineering requirement D (standing spec reread each run): a constraint
// added mid-loop -- "never push to main", "do not touch auth" -- must reach the
// running team, not just agents spawned afterward. Called from the idle monitor
// sweep; a small JSON read is negligible. The refreshed copy reaches running
// agents via the [CURRENT CONSTRAINTS] block in the replenishment prompt.
func (m *Manager) refreshProjectInvariants() {
	data, err := os.ReadFile(projectInvariantsPath())
	if err != nil {
		return // no invariants file yet, or unreadable -- nothing to refresh
	}
	var items []string
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	clean := sanitizeInvariants(items)
	m.mu.Lock()
	changed := !equalStringSlices(m.projectInvariants, clean)
	if changed {
		m.projectInvariants = clean
	}
	m.mu.Unlock()
	if changed && m.broadcast != nil {
		m.broadcast("office:projectInvariantsUpdated", map[string]interface{}{
			"invariants": clean,
			"source":     "drift-refresh",
		})
	}
}

// equalStringSlices reports whether two string slices are element-wise equal.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
