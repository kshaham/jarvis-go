package library

// Promotion ledger — small JSON store at
// <library_root>/.promotion-ledger.json. Tracks per-document
// re-use statistics that Tier 3's auto-promotion job consumes:
//
//   - reuse_count: how many tasks have cited this doc as a hint
//   - avg_outcome: rolling-mean outcome score of tasks that
//                  cited this doc (proxy for "did the hint actually
//                  help?")
//   - first_seen_at: when the doc first showed up in a hint
//   - last_used_at:  most recent citation
//   - promoted_at:   timestamp when the doc graduated to a shared
//                    skill / workflow (memify will set this)
//   - revoked_at:    operator-override; suppresses future promotion
//                    until revoked_at + 24h cooldown
//
// Why JSON not SQLite: the ledger is small (<100 KB even after
// months of operation), single-writer (memify + handler updates
// are serialised through the ledger's own mutex), and we already
// rely on atomic tmp+rename for every other library write.
// Avoiding SQLite keeps the dependency story trivial and the
// on-disk file directly inspectable by operators.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// LedgerEntry is one row in the promotion ledger.
type LedgerEntry struct {
	Path         string    `json:"path"`           // relative-to-library path of the cited doc
	ReuseCount   int       `json:"reuse_count"`
	AvgOutcome   float64   `json:"avg_outcome"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	PromotedAt   time.Time `json:"promoted_at,omitempty"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
	RevokedReason string   `json:"revoked_reason,omitempty"`
}

// Ledger is the in-memory + on-disk ledger. Methods are safe for
// concurrent use; the mutex protects both the map and disk writes.
// Re-reading from disk is a no-op once Load has succeeded — the
// ledger is the source of truth at runtime.
type Ledger struct {
	mu      sync.Mutex
	path    string
	entries map[string]*LedgerEntry
	loaded  bool
}

// LedgerPath returns the absolute path of the ledger file. Lives
// under the library root so backups + snapshots pick it up
// automatically.
func LedgerPath() string {
	root := Dir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".promotion-ledger.json")
}

// OpenLedger returns a Ledger ready to use. Load is lazy — the
// first method call hits disk; subsequent calls use the in-memory
// map. Returns nil + nil error when the library root is
// unavailable (graceful degradation matching the rest of library).
func OpenLedger() (*Ledger, error) {
	path := LedgerPath()
	if path == "" {
		return nil, nil
	}
	return &Ledger{path: path, entries: map[string]*LedgerEntry{}}, nil
}

// load reads the ledger from disk (idempotent — subsequent calls
// are no-ops). Caller MUST hold l.mu.
func (l *Ledger) load() error {
	if l.loaded {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			l.loaded = true
			return nil
		}
		return fmt.Errorf("read ledger: %w", err)
	}
	var rows []*LedgerEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return fmt.Errorf("parse ledger: %w", err)
	}
	for _, r := range rows {
		l.entries[r.Path] = r
	}
	l.loaded = true
	return nil
}

// flushLocked writes the current in-memory ledger to disk
// atomically (tmp + rename). Caller MUST hold l.mu.
func (l *Ledger) flushLocked() error {
	rows := make([]*LedgerEntry, 0, len(l.entries))
	for _, r := range l.entries {
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Path < rows[j].Path
	})
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	return writeAtomic(l.path, data)
}

// Get returns the entry for path, or nil if not yet recorded.
func (l *Ledger) Get(path string) (*LedgerEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return nil, err
	}
	if e, ok := l.entries[path]; ok {
		// Return a copy so callers can't mutate ledger state via
		// the returned pointer.
		copied := *e
		return &copied, nil
	}
	return nil, nil
}

// Increment bumps the reuse count for path and updates the rolling
// average outcome. The first time path is seen, FirstSeenAt is set
// to now. LastUsedAt is set to now every time.
//
// outcomeScore is the just-completed task's outcome (0..1) for the
// citing agent — captures whether the hint actually helped.
//
// Atomic at the file level: each call rewrites the whole ledger
// JSON. Fine for the expected size (<1000 entries) since the file
// stays well under 1 MB.
func (l *Ledger) Increment(path string, outcomeScore float64) error {
	if path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return err
	}
	now := time.Now().UTC()
	e, ok := l.entries[path]
	if !ok {
		e = &LedgerEntry{
			Path:        path,
			FirstSeenAt: now,
		}
		l.entries[path] = e
	}
	// Rolling mean: new = (old*n + x) / (n+1)
	n := float64(e.ReuseCount)
	e.AvgOutcome = (e.AvgOutcome*n + outcomeScore) / (n + 1)
	e.ReuseCount++
	e.LastUsedAt = now
	return l.flushLocked()
}

// MarkPromoted stamps PromotedAt = now for the given path so the
// memify job's threshold check can skip already-promoted entries.
// No-op when the path has no ledger entry yet (caller should ensure
// the entry exists via Increment before calling).
func (l *Ledger) MarkPromoted(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return err
	}
	e, ok := l.entries[path]
	if !ok {
		return nil
	}
	e.PromotedAt = time.Now().UTC()
	return l.flushLocked()
}

// MarkRevoked stamps RevokedAt = now + the operator-supplied reason.
// Promotion threshold respects a 24h cooldown after revoke before
// re-promoting the same path.
func (l *Ledger) MarkRevoked(path, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return err
	}
	e, ok := l.entries[path]
	if !ok {
		// Create an entry so the revocation persists even if the
		// path has never been cited. Lets operators preemptively
		// block a hit they don't want to see promoted.
		e = &LedgerEntry{Path: path, FirstSeenAt: time.Now().UTC()}
		l.entries[path] = e
	}
	e.RevokedAt = time.Now().UTC()
	e.RevokedReason = reason
	// Clear PromotedAt so memify's "not promoted yet" branch fires
	// next time (after cooldown).
	e.PromotedAt = time.Time{}
	return l.flushLocked()
}

// List returns a snapshot of all entries sorted by ReuseCount desc.
// Used by the operator dashboard's promotion view.
func (l *Ledger) List() ([]LedgerEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return nil, err
	}
	rows := make([]LedgerEntry, 0, len(l.entries))
	for _, e := range l.entries {
		rows = append(rows, *e)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ReuseCount != rows[j].ReuseCount {
			return rows[i].ReuseCount > rows[j].ReuseCount
		}
		return rows[i].Path < rows[j].Path
	})
	return rows, nil
}
