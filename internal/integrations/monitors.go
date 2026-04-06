package integrations

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// GitStatus holds parsed git status information for a session's working directory.
type GitStatus struct {
	Branch    string    `json:"branch"`
	Changed   []string  `json:"changed"`
	Staged    []string  `json:"staged"`
	Untracked []string  `json:"untracked"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IntegrationStatus holds all integration statuses for a session.
type IntegrationStatus struct {
	Git *GitStatus `json:"git,omitempty"`
}

// Monitor tracks integration statuses per session.
type Monitor struct {
	mu       sync.RWMutex
	statuses map[string]*IntegrationStatus // sessionID -> status
}

// NewMonitor creates a new integration monitor.
func NewMonitor() *Monitor {
	return &Monitor{
		statuses: make(map[string]*IntegrationStatus),
	}
}

// RefreshGitStatus runs git status in the given directory and caches the result.
func (m *Monitor) RefreshGitStatus(sessionID, workingDir string) *GitStatus {
	status := &GitStatus{UpdatedAt: time.Now()}

	// Get branch
	if out, err := exec.Command("git", "-C", workingDir, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		status.Branch = strings.TrimSpace(string(out))
	}

	// Get changed files
	if out, err := exec.Command("git", "-C", workingDir, "status", "--porcelain").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if len(line) < 3 {
				continue
			}
			indicator := line[:2]
			file := strings.TrimSpace(line[2:])
			switch {
			case indicator[0] != ' ' && indicator[0] != '?':
				status.Staged = append(status.Staged, file)
			case indicator[1] != ' ':
				status.Changed = append(status.Changed, file)
			case indicator == "??":
				status.Untracked = append(status.Untracked, file)
			}
		}
	}

	m.mu.Lock()
	if m.statuses[sessionID] == nil {
		m.statuses[sessionID] = &IntegrationStatus{}
	}
	m.statuses[sessionID].Git = status
	m.mu.Unlock()

	return status
}

// GetStatus returns the cached integration status for a session.
func (m *Monitor) GetStatus(sessionID string) *IntegrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.statuses[sessionID]; ok {
		return s
	}
	return &IntegrationStatus{}
}
