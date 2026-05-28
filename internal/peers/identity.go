// Package peers owns the "this jarvis instance is one team identity"
// model. A LocalIdentity is the operator's handle for talking to other
// jarvis processes (peer teams). It's stable across team spawns and
// shutdowns — operator's frontend team keeps being "frontend" even
// when the team isn't currently spawned.
//
// On-disk layout:
//
//	~/.jarvis/identity.json        — LocalIdentity
//	~/.jarvis/peers/peers.json     — registry of known peer teams
//	~/.jarvis/relay/inbox/<id>.json — inbound messages from peers
//	~/.jarvis/relay/sent/<id>.json — outbound message audit log
//
// The package is import-cycle-safe: it depends on nothing in
// internal/office; the office side wires up message injection through
// the MessageInjector interface declared here.
package peers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LocalIdentity is "who am I when another team talks to me."
// PeerID is generated once and never changes. DisplayName and PublicURL
// are operator-set and used by peers to render the message in their UI.
type LocalIdentity struct {
	PeerID      string    `json:"peerId"`
	DisplayName string    `json:"displayName"`
	PublicURL   string    `json:"publicUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

// IdentityStore loads, saves, and (when missing) initializes the
// LocalIdentity for this jarvis process. Safe for concurrent access.
type IdentityStore struct {
	mu   sync.RWMutex
	path string
	id   *LocalIdentity
}

// NewIdentityStore returns a store backed by ~/.jarvis/identity.json
// (or the override path for tests). It loads the existing identity
// from disk if present; otherwise generates a new one on first
// Get/Set call.
func NewIdentityStore(path string) *IdentityStore {
	return &IdentityStore{path: path}
}

// DefaultIdentityPath returns ~/.jarvis/identity.json (or "" if the
// home directory can't be resolved — caller treats "" as "skip" the
// same way the library package handles its degraded mode).
func DefaultIdentityPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "identity.json")
}

// Get returns the current LocalIdentity, generating one (and writing
// it to disk) on first access.
func (s *IdentityStore) Get() (LocalIdentity, error) {
	s.mu.RLock()
	if s.id != nil {
		cp := *s.id
		s.mu.RUnlock()
		return cp, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Double-check after write lock — another goroutine may have loaded.
	if s.id != nil {
		return *s.id, nil
	}
	loaded, err := s.loadLocked()
	if err != nil {
		return LocalIdentity{}, err
	}
	if loaded == nil {
		// First run — synthesize and persist.
		newID := defaultIdentity()
		if err := s.saveLocked(newID); err != nil {
			return LocalIdentity{}, fmt.Errorf("save new identity: %w", err)
		}
		s.id = &newID
		return newID, nil
	}
	s.id = loaded
	return *loaded, nil
}

// Update applies a partial change (empty fields leave the existing
// value alone) and persists. PeerID is never mutated by Update — it's
// generated once and forever.
func (s *IdentityStore) Update(displayName, publicURL string) (LocalIdentity, error) {
	// Make sure the identity exists before mutating.
	if _, err := s.Get(); err != nil {
		return LocalIdentity{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := *s.id
	if displayName != "" {
		cur.DisplayName = displayName
	}
	if publicURL != "" {
		cur.PublicURL = publicURL
	}
	if err := s.saveLocked(cur); err != nil {
		return LocalIdentity{}, err
	}
	s.id = &cur
	return cur, nil
}

func (s *IdentityStore) loadLocked() (*LocalIdentity, error) {
	if s.path == "" {
		// Degraded mode — fall back to a non-persistent in-memory identity.
		ephemeral := defaultIdentity()
		return &ephemeral, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read identity: %w", err)
	}
	var out LocalIdentity
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return &out, nil
}

func (s *IdentityStore) saveLocked(id LocalIdentity) error {
	if s.path == "" {
		// Degraded mode — accept the write but don't persist.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir for identity: %w", err)
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal identity: %w", err)
	}
	// Atomic write: tmp file + rename. Mirrors the library package's
	// persistence pattern so an interrupted write can't leave us with
	// a half-written identity.json on disk.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write identity tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename identity: %w", err)
	}
	return nil
}

func defaultIdentity() LocalIdentity {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "Local team"
	}
	return LocalIdentity{
		PeerID:      uuid.New().String(),
		DisplayName: host,
		PublicURL:   "",
		CreatedAt:   time.Now().UTC(),
	}
}
