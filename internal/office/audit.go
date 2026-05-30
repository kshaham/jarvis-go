package office

import (
	"encoding/json"
	"sync"
)

// AuditWriter manages an in-memory ring buffer of audit entries.
type AuditWriter struct {
	mu      sync.Mutex
	entries []AuditEntry
	maxSize int
}

// NewAuditWriter creates a new audit writer.
func NewAuditWriter(maxSize int) *AuditWriter {
	return &AuditWriter{
		entries: make([]AuditEntry, 0),
		maxSize: maxSize,
	}
}

// Log adds an entry to the audit log.
func (aw *AuditWriter) Log(entry AuditEntry) {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	aw.entries = append(aw.entries, entry)
	if len(aw.entries) > aw.maxSize {
		newEntries := make([]AuditEntry, aw.maxSize)
		copy(newEntries, aw.entries[len(aw.entries)-aw.maxSize:])
		aw.entries = newEntries
	}
}

// Entries returns all audit entries.
func (aw *AuditWriter) Entries() []AuditEntry {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	result := make([]AuditEntry, len(aw.entries))
	copy(result, aw.entries)
	return result
}

// Count returns the number of entries.
func (aw *AuditWriter) Count() int {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	return len(aw.entries)
}

// LastN returns at most n entries from the tail of the ring buffer, in
// insertion order. If n >= len(entries) all entries are returned. Copying
// only the required suffix avoids the full-ring allocation that Entries()
// performs when the caller only needs a small window.
func (aw *AuditWriter) LastN(n int) []AuditEntry {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	if n <= 0 {
		return nil
	}
	total := len(aw.entries)
	start := total - n
	if start < 0 {
		start = 0
	}
	result := make([]AuditEntry, total-start)
	copy(result, aw.entries[start:])
	return result
}

// MarshalJSON returns the entries as JSON.
func (aw *AuditWriter) MarshalJSON() ([]byte, error) {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	return json.Marshal(aw.entries)
}
