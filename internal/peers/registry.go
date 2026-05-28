package peers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PeerStatus is the latest observed reachability of a peer team. Reset
// to "unknown" until the first ping completes; flips between "ok",
// "unreachable" (network/timeout), and "unauthorized" (401/403).
type PeerStatus string

const (
	PeerStatusUnknown      PeerStatus = "unknown"
	PeerStatusOK           PeerStatus = "ok"
	PeerStatusUnreachable  PeerStatus = "unreachable"
	PeerStatusUnauthorized PeerStatus = "unauthorized"
)

// Peer is a registered remote jarvis instance — another team this
// operator wants to exchange messages with. PairToken is a shared
// secret bearer the two operators exchanged out of band; it's used to
// authenticate every inter-process call.
type Peer struct {
	PeerID      string     `json:"peerId"`
	DisplayName string     `json:"displayName"`
	PublicURL   string     `json:"publicUrl"`
	PairToken   string     `json:"pairToken"`
	AddedAt     time.Time  `json:"addedAt"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
	Status      PeerStatus `json:"status"`
	// LastError is the most recent ping/send error message (capped to
	// 200 chars) so the dashboard can surface "why is this red?" without
	// the operator hunting through logs.
	LastError string `json:"lastError,omitempty"`
}

// Registry tracks the peers this jarvis instance can talk to. JSON-
// backed (~/.jarvis/peers/peers.json), atomic writes, safe for
// concurrent access from the HTTP layer + the relay sender.
type Registry struct {
	mu    sync.RWMutex
	path  string
	peers map[string]*Peer // keyed by PeerID
}

// NewRegistry returns a registry backed by the given path. Loads
// existing peers from disk; missing file is fine (treated as empty).
func NewRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, peers: map[string]*Peer{}}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// DefaultRegistryPath returns ~/.jarvis/peers/peers.json. Returns "" if
// the home directory can't be resolved (degraded in-memory mode).
func DefaultRegistryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "peers", "peers.json")
}

// List returns peers in insertion order (sorted by AddedAt).
func (r *Registry) List() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, *p)
	}
	// Stable-ish order: by AddedAt ascending. Two peers added in the
	// same millisecond will tie-break by PeerID, which is fine.
	sortPeersByAddedAt(out)
	return out
}

// Get returns a single peer (deep copy) by ID.
func (r *Registry) Get(peerID string) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[peerID]
	if !ok {
		return Peer{}, false
	}
	return *p, true
}

// FindByToken returns the peer registered with the given pair token,
// if any. Used by the inbound /api/relay/inbox handler to authenticate
// the calling peer without needing the FromPeerID field to be trusted.
func (r *Registry) FindByToken(token string) (Peer, bool) {
	if token == "" {
		return Peer{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.peers {
		if p.PairToken == token {
			return *p, true
		}
	}
	return Peer{}, false
}

// Add inserts a new peer. Returns an error if a peer with the same
// peerID already exists.
func (r *Registry) Add(p Peer) error {
	if p.PeerID == "" {
		return errors.New("peerID is required")
	}
	if p.PublicURL == "" {
		return errors.New("publicURL is required")
	}
	if p.PairToken == "" {
		return errors.New("pairToken is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.peers[p.PeerID]; exists {
		return fmt.Errorf("peer %s is already registered", p.PeerID)
	}
	if p.AddedAt.IsZero() {
		p.AddedAt = time.Now().UTC()
	}
	if p.Status == "" {
		p.Status = PeerStatusUnknown
	}
	cp := p
	r.peers[p.PeerID] = &cp
	return r.saveLocked()
}

// Update merges non-zero fields of the patch into the existing peer
// and persists.
func (r *Registry) Update(peerID string, patch func(*Peer)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers[peerID]
	if !ok {
		return fmt.Errorf("peer %s not found", peerID)
	}
	patch(p)
	return r.saveLocked()
}

// MarkStatus updates the peer's status + lastSeenAt and persists. Used
// by the ping endpoint and the send-message path.
func (r *Registry) MarkStatus(peerID string, status PeerStatus, errMsg string) {
	_ = r.Update(peerID, func(p *Peer) {
		p.Status = status
		now := time.Now().UTC()
		if status == PeerStatusOK {
			p.LastSeenAt = &now
			p.LastError = ""
		} else {
			if len(errMsg) > 200 {
				errMsg = errMsg[:200]
			}
			p.LastError = errMsg
		}
	})
}

// Remove deletes a peer by ID. Returns nil even if the peer didn't
// exist (idempotent delete is what the HTTP DELETE expects).
func (r *Registry) Remove(peerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, peerID)
	return r.saveLocked()
}

func (r *Registry) load() error {
	if r.path == "" {
		return nil
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read peers: %w", err)
	}
	var list []*Peer
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse peers: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range list {
		if p == nil || p.PeerID == "" {
			continue
		}
		if p.Status == "" {
			p.Status = PeerStatusUnknown
		}
		r.peers[p.PeerID] = p
	}
	return nil
}

func (r *Registry) saveLocked() error {
	if r.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("mkdir peers: %w", err)
	}
	list := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		list = append(list, *p)
	}
	sortPeersByAddedAt(list)
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal peers: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write peers tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename peers: %w", err)
	}
	return nil
}

// GeneratePairToken returns a fresh 32-byte hex bearer suitable for
// the operator to paste into a peer's "Add peer" form. The token is
// only meaningful as the *shared secret* — each side stores it on
// their own peers.json entry for the other side.
func GeneratePairToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random read: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// sortPeersByAddedAt sorts in-place by AddedAt ascending, tie-breaking
// by PeerID so the order is deterministic.
func sortPeersByAddedAt(peers []Peer) {
	for i := 1; i < len(peers); i++ {
		j := i
		for j > 0 && peerLess(peers[j], peers[j-1]) {
			peers[j], peers[j-1] = peers[j-1], peers[j]
			j--
		}
	}
}

func peerLess(a, b Peer) bool {
	if a.AddedAt.Equal(b.AddedAt) {
		return a.PeerID < b.PeerID
	}
	return a.AddedAt.Before(b.AddedAt)
}
