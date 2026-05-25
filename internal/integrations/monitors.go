package integrations

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// gitMonitorTimeout caps each git invocation in the monitor loop.
// Pre-fix, a hung git (network filesystem stall, busy .git/index)
// wedged the monitor goroutine for hours during 12h unattended runs.
const gitMonitorTimeout = 30 * time.Second

// runMonitorGit runs git with a per-call timeout in the given dir
// and returns the captured stdout. Pure helper so the two RefreshGitStatus
// call sites read uniformly.
func runMonitorGit(workingDir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitMonitorTimeout)
	defer cancel()
	full := append([]string{"-C", workingDir}, args...)
	return exec.CommandContext(ctx, "git", full...).Output()
}

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

	// Get branch (timeout-bounded; pre-fix a hung git wedged the
	// monitor goroutine indefinitely).
	if out, err := runMonitorGit(workingDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		status.Branch = strings.TrimSpace(string(out))
	}

	// Get changed files
	if out, err := runMonitorGit(workingDir, "status", "--porcelain"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if len(line) < 3 {
				continue
			}
			indicator := line[:2]
			file := strings.TrimSpace(line[2:])
			// Order matters: untracked ("??") must be checked before
			// the staged / changed branches, otherwise the second
			// byte's '?' falls through into the "changed" classifier.
			switch {
			case indicator == "??":
				status.Untracked = append(status.Untracked, file)
			case indicator[0] != ' ' && indicator[0] != '?':
				status.Staged = append(status.Staged, file)
			case indicator[1] != ' ':
				status.Changed = append(status.Changed, file)
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
