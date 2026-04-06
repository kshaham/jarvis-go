package office

import (
	"fmt"
	"strings"
	"time"
)

// AcquireLock attempts to acquire a file lock.
func (m *Manager) AcquireLock(path, sessionID, ownerName string) (*FileLock, error) {
	normalized := normalizePath(path)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.fileLocks[normalized]; ok {
		if time.Now().Before(existing.ExpiresAt) && existing.Owner != sessionID {
			return nil, fmt.Errorf("file locked by %s until %s", existing.OwnerName, existing.ExpiresAt.Format(time.RFC3339))
		}
	}

	lock := &FileLock{
		Path:       normalized,
		Owner:      sessionID,
		OwnerName:  ownerName,
		AcquiredAt: time.Now(),
		ExpiresAt:  time.Now().Add(m.config.FileLockTTL),
	}
	m.fileLocks[normalized] = lock
	return lock, nil
}

// ReleaseLock releases a file lock.
func (m *Manager) ReleaseLock(path, sessionID string) bool {
	normalized := normalizePath(path)

	m.mu.Lock()
	defer m.mu.Unlock()

	lock, ok := m.fileLocks[normalized]
	if !ok {
		return false
	}
	if lock.Owner != sessionID {
		return false
	}
	delete(m.fileLocks, normalized)
	return true
}

// GetLocks returns all active file locks, lazily cleaning up expired ones.
func (m *Manager) GetLocks() []*FileLock {
	m.mu.Lock() // Write lock needed for cleanup
	defer m.mu.Unlock()

	now := time.Now()
	for path, lock := range m.fileLocks {
		if now.After(lock.ExpiresAt) {
			delete(m.fileLocks, path)
		}
	}
	result := make([]*FileLock, 0, len(m.fileLocks))
	for _, lock := range m.fileLocks {
		result = append(result, lock)
	}
	return result
}

// CleanupExpiredLocks removes expired locks.
func (m *Manager) CleanupExpiredLocks() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0
	for path, lock := range m.fileLocks {
		if now.After(lock.ExpiresAt) {
			delete(m.fileLocks, path)
			count++
		}
	}
	return count
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/")
	return path
}
