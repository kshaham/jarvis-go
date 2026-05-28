package peers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RelayMessage is one cross-team message — either outbound (we sent
// it) or inbound (a peer sent it to us). Direction is set from the
// local perspective so the UI can render with/without bubble side.
//
// Attachments are embedded — when the sender attaches an experience
// from their library, the rendered markdown is copied into Body at
// send time so the recipient doesn't need access to the sender's
// library.
type RelayMessage struct {
	ID              string            `json:"id"`
	FromPeerID      string            `json:"fromPeerId"`
	FromDisplayName string            `json:"fromDisplayName"`
	ToPeerID        string            `json:"toPeerId"`
	Content         string            `json:"content"`
	Attachments     []RelayAttachment `json:"attachments,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	DeliveredAt     *time.Time        `json:"deliveredAt,omitempty"`
	ReadAt          *time.Time        `json:"readAt,omitempty"`
	Direction       string            `json:"direction"` // "outbound" | "inbound"
}

// RelayAttachment is a snippet of knowledge the sender wants the
// recipient to learn from — a library experience, a workflow, a task
// summary, or a mission excerpt. Body holds the rendered markdown
// inline so attachments are self-contained.
type RelayAttachment struct {
	Kind  string `json:"kind"` // "experience" | "workflow" | "task" | "missionExcerpt"
	Ref   string `json:"ref"`  // sender-local ID — opaque to the recipient
	Title string `json:"title"`
	Body  string `json:"body"`
}

// MessageInjector is implemented by the office.Manager. It lets the
// relay service deliver an inbound message into the live team without
// a hard dependency on internal/office (which would create an import
// cycle). The office side wires this up in main / server bootstrap.
//
// The "live" bool tells the relay layer whether the inject actually
// reached running agents so the SPA can render a "delivered to agents"
// badge.
type MessageInjector interface {
	InjectRelayMessage(fromPeerID, fromDisplayName, content string) (live bool)
}

// EventBroadcaster is implemented by the office.Manager's broadcast
// channel (or any test double). Kept tiny so the relay package can
// emit SSE events without depending on internal/web/sse.
type EventBroadcaster func(event string, data interface{})

// Store persists inbox + sent messages as one JSON file per message
// under ~/.jarvis/relay/. Matches the per-record convention used
// elsewhere in the codebase (library, snapshots).
type Store struct {
	mu      sync.RWMutex
	inbox   string // dir for inbound messages
	sent    string // dir for outbound messages
	loaded  bool   // have we walked the directories yet?
	indexIn map[string]RelayMessage
	indexOut map[string]RelayMessage
}

// DefaultRelayDir returns ~/.jarvis/relay. Returns "" if home can't
// be resolved (degraded in-memory mode).
func DefaultRelayDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "relay")
}

// NewStore creates a relay store rooted at the given directory. The
// inbox/ and sent/ subdirs are created on first write.
func NewStore(root string) *Store {
	if root == "" {
		return &Store{
			indexIn:  map[string]RelayMessage{},
			indexOut: map[string]RelayMessage{},
			loaded:   true,
		}
	}
	return &Store{
		inbox:    filepath.Join(root, "inbox"),
		sent:     filepath.Join(root, "sent"),
		indexIn:  map[string]RelayMessage{},
		indexOut: map[string]RelayMessage{},
	}
}

// ensureLoaded walks the inbox/sent directories on first call so
// subsequent List() calls are cheap. Idempotent.
func (s *Store) ensureLoaded() error {
	s.mu.RLock()
	if s.loaded {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	if err := loadDir(s.inbox, "inbound", s.indexIn); err != nil {
		return err
	}
	if err := loadDir(s.sent, "outbound", s.indexOut); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

func loadDir(dir, expectedDirection string, into map[string]RelayMessage) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read relay dir %q: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			// Skip unreadable record but keep going so one corrupt
			// file can't lock out the whole inbox.
			continue
		}
		var msg RelayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID == "" {
			continue
		}
		// Trust the directory's intended direction over the field on
		// disk, so a mislabelled record doesn't end up in the wrong bucket.
		msg.Direction = expectedDirection
		into[msg.ID] = msg
	}
	return nil
}

// AppendOutbound stores an outbound message. DeliveredAt may be nil
// (peer was unreachable) — caller updates the record later via
// MarkDelivered when the retry succeeds.
func (s *Store) AppendOutbound(msg RelayMessage) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	msg.Direction = "outbound"
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexOut[msg.ID] = msg
	return s.persistLocked(s.sent, msg)
}

// AppendInbound stores an inbound message and returns it so the
// caller can re-broadcast as an SSE event.
func (s *Store) AppendInbound(msg RelayMessage) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	msg.Direction = "inbound"
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexIn[msg.ID] = msg
	return s.persistLocked(s.inbox, msg)
}

// MarkRead flips the ReadAt field for an inbound (or outbound — both
// are settable) record and persists. Returns "not found" if the
// message ID doesn't exist in either bucket.
func (s *Store) MarkRead(id string) error {
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if m, ok := s.indexIn[id]; ok {
		m.ReadAt = &now
		s.indexIn[id] = m
		return s.persistLocked(s.inbox, m)
	}
	if m, ok := s.indexOut[id]; ok {
		m.ReadAt = &now
		s.indexOut[id] = m
		return s.persistLocked(s.sent, m)
	}
	return fmt.Errorf("relay message %s not found", id)
}

// MarkDelivered updates an outbound message's DeliveredAt to the
// supplied time and persists. Idempotent.
func (s *Store) MarkDelivered(id string, when time.Time) error {
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.indexOut[id]
	if !ok {
		return fmt.Errorf("outbound message %s not found", id)
	}
	m.DeliveredAt = &when
	s.indexOut[id] = m
	return s.persistLocked(s.sent, m)
}

// List returns every message we know about (inbox + sent), sorted by
// CreatedAt ascending. Filter by peer (sender or recipient = peerID).
// An empty peerID returns everything.
func (s *Store) List(peerID string) ([]RelayMessage, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]RelayMessage, 0, len(s.indexIn)+len(s.indexOut))
	for _, m := range s.indexIn {
		if peerID == "" || m.FromPeerID == peerID {
			all = append(all, m)
		}
	}
	for _, m := range s.indexOut {
		if peerID == "" || m.ToPeerID == peerID {
			all = append(all, m)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	return all, nil
}

// UnreadInboundByPeer returns peerID → count of unread inbound
// messages. Powers the badge counts in the RelayScreen's peer list.
func (s *Store) UnreadInboundByPeer() (map[string]int, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int{}
	for _, m := range s.indexIn {
		if m.ReadAt == nil {
			counts[m.FromPeerID]++
		}
	}
	return counts, nil
}

func (s *Store) persistLocked(dir string, msg RelayMessage) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir relay: %w", err)
	}
	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal relay message: %w", err)
	}
	path := filepath.Join(dir, msg.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write relay tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename relay: %w", err)
	}
	return nil
}

// Service ties together the registry, the local identity, the store,
// the HTTP client, and the message injector. It's the single object
// that the HTTP handler layer talks to.
type Service struct {
	Identity  *IdentityStore
	Registry  *Registry
	Store     *Store
	Client    *Client
	Injector  MessageInjector
	Broadcast EventBroadcaster
}

// SendOutbound is called by the operator's "Send" click. Resolves the
// target peer, persists the outbound record, attempts delivery via
// HTTP, and updates DeliveredAt / status accordingly. Returns the
// stored message (with a fresh ID and any delivery metadata) so the
// HTTP handler can return it to the SPA.
func (s *Service) SendOutbound(toPeerID, content string, attachments []RelayAttachment) (RelayMessage, error) {
	if content == "" && len(attachments) == 0 {
		return RelayMessage{}, errors.New("message must have content or attachments")
	}
	peer, ok := s.Registry.Get(toPeerID)
	if !ok {
		return RelayMessage{}, fmt.Errorf("peer %s not registered", toPeerID)
	}
	identity, err := s.Identity.Get()
	if err != nil {
		return RelayMessage{}, fmt.Errorf("load identity: %w", err)
	}
	msg := RelayMessage{
		ID:              uuid.New().String(),
		FromPeerID:      identity.PeerID,
		FromDisplayName: identity.DisplayName,
		ToPeerID:        peer.PeerID,
		Content:         content,
		Attachments:     attachments,
		CreatedAt:       time.Now().UTC(),
		Direction:       "outbound",
	}
	if err := s.Store.AppendOutbound(msg); err != nil {
		return RelayMessage{}, fmt.Errorf("persist outbound: %w", err)
	}
	if s.Broadcast != nil {
		s.Broadcast("office:relaySent", msg)
	}

	// Attempt delivery. Failure is non-fatal — the message stays in the
	// sent log with DeliveredAt == nil so the UI can show "pending /
	// retry."
	if s.Client != nil {
		if err := s.Client.PostInbox(peer, msg); err != nil {
			s.Registry.MarkStatus(peer.PeerID, statusFromErr(err), err.Error())
			return msg, fmt.Errorf("deliver to peer: %w", err)
		}
		now := time.Now().UTC()
		msg.DeliveredAt = &now
		_ = s.Store.MarkDelivered(msg.ID, now)
		s.Registry.MarkStatus(peer.PeerID, PeerStatusOK, "")
	}
	return msg, nil
}

// ReceiveInbound is called by the HTTP handler for /api/relay/inbox
// after it has authenticated the calling peer. Persists the message,
// optionally injects it into the live team, and emits the SSE event.
func (s *Service) ReceiveInbound(msg RelayMessage) error {
	msg.Direction = "inbound"
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := s.Store.AppendInbound(msg); err != nil {
		return fmt.Errorf("persist inbound: %w", err)
	}
	live := false
	if s.Injector != nil {
		body := msg.Content
		// Append attachments as a compact appendix the agents can read.
		if len(msg.Attachments) > 0 {
			body += "\n\n— Knowledge from " + msg.FromDisplayName + " —"
			for _, att := range msg.Attachments {
				body += "\n\n## " + att.Kind + ": " + att.Title + "\n" + att.Body
			}
		}
		live = s.Injector.InjectRelayMessage(msg.FromPeerID, msg.FromDisplayName, body)
	}
	if s.Broadcast != nil {
		s.Broadcast("office:relayReceived", map[string]interface{}{
			"message": msg,
			"live":    live,
		})
	}
	return nil
}

func statusFromErr(err error) PeerStatus {
	if err == nil {
		return PeerStatusOK
	}
	msg := err.Error()
	// Crude but effective — the Client returns specific error strings
	// for 401/403 so we can map back to "unauthorized."
	for _, needle := range []string{"401", "403", "unauthorized", "forbidden"} {
		if contains(msg, needle) {
			return PeerStatusUnauthorized
		}
	}
	return PeerStatusUnreachable
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
