package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/peers"
	"github.com/kshaham/jarvis/internal/web/request"
)

// RelayInboxIn is the inbound endpoint other jarvis instances POST to
// when they want to deliver a message. Authenticated via the pair
// token the *receiving* operator stored when they registered the
// sender as a peer.
//
// The sender's claimed FromPeerID is cross-checked against the peer
// registry entry the token resolves to — if the token belongs to
// peer X but the body claims to be from peer Y, the message is
// rejected. This stops a compromised token from being used to
// impersonate other peers.
func (p *Peers) RelayInboxIn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// Bearer token from Authorization: Bearer <token>
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		request.WriteError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	token := strings.TrimSpace(auth[len(prefix):])
	peer, ok := p.Registry.FindByToken(token)
	if !ok {
		request.WriteError(w, http.StatusUnauthorized, "unknown peer token")
		return
	}
	var msg peers.RelayMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Cross-check the claimed sender against the token's owning peer.
	if msg.FromPeerID != "" && msg.FromPeerID != peer.PeerID {
		request.WriteError(w, http.StatusForbidden, "token does not match fromPeerId")
		return
	}
	// Fill in defensible fields from the trusted registry side. Sender
	// supplies content; we override identity-shaped fields.
	msg.FromPeerID = peer.PeerID
	if msg.FromDisplayName == "" {
		msg.FromDisplayName = peer.DisplayName
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	// ToPeerID is our local peerID — fill from identity if the sender
	// left it blank.
	if msg.ToPeerID == "" {
		if id, err := p.Identity.Get(); err == nil {
			msg.ToPeerID = id.PeerID
		}
	}
	if err := p.Relay.ReceiveInbound(msg); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "receive: "+err.Error())
		return
	}
	// Update the peer's lastSeen so the operator sees a fresh "ok" dot.
	p.Registry.MarkStatus(peer.PeerID, peers.PeerStatusOK, "")
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"received": true, "id": msg.ID})
}

// RelaySend is the outbound endpoint the SPA hits to send a message
// to a registered peer. The handler resolves attachments to the
// library (so the SPA can pass refs instead of full bodies), runs the
// HTTP delivery, and returns the stored message.
func (p *Peers) RelaySend(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		ToPeerID    string                  `json:"toPeerId"`
		Content     string                  `json:"content"`
		Attachments []peers.RelayAttachment `json:"attachments"`
		// AttachmentRefs is a quick form the SPA can use instead of
		// inlining full bodies — a list of {kind, ref}. The handler
		// resolves each into a real attachment by reading the library.
		AttachmentRefs []struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"attachmentRefs,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ToPeerID == "" {
		request.WriteError(w, http.StatusBadRequest, "toPeerId is required")
		return
	}
	if len(body.Content) > 4000 {
		request.WriteError(w, http.StatusBadRequest, "content max 4000 chars")
		return
	}
	// Resolve any refs into full attachments by reading the library.
	for _, ref := range body.AttachmentRefs {
		att, err := resolveAttachment(ref.Kind, ref.Ref)
		if err != nil {
			request.WriteError(w, http.StatusBadRequest, "attachment "+ref.Ref+": "+err.Error())
			return
		}
		body.Attachments = append(body.Attachments, att)
	}
	msg, err := p.Relay.SendOutbound(body.ToPeerID, body.Content, body.Attachments)
	if err != nil {
		// The message was persisted even on delivery failure (pending),
		// so return the record + the error so the SPA can show it as
		// "queued / retry".
		request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
			"message": msg,
			"error":   err.Error(),
		})
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"message": msg})
}

// RelayList returns the full relay log, optionally filtered by peer
// (?peerId=…). Inbound + outbound mixed, sorted by CreatedAt ascending.
func (p *Peers) RelayList(w http.ResponseWriter, r *http.Request) {
	peerID := r.URL.Query().Get("peerId")
	list, err := p.Relay.Store.List(peerID)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"messages": list})
}

// RelayMarkRead flips the ReadAt timestamp on one message.
func (p *Peers) RelayMarkRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		request.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := p.Relay.Store.MarkRead(id); err != nil {
		request.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"read": true})
}

// resolveAttachment reads a library file based on the (kind, ref)
// shorthand the SPA passes and returns a fully-inlined attachment.
// Kinds:
//
//	experience      — library/experiences/<configID>/<taskID>.md
//	workflow        — library/workflows/<slug>.md
//	missionExcerpt  — the operator-supplied excerpt is in the ref
//	                  (passed directly without library lookup)
//
// task is intentionally not supported yet (the office task store
// doesn't have a per-task markdown projection) — the SPA falls back
// to letting the operator paste a summary directly.
func resolveAttachment(kind, ref string) (peers.RelayAttachment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return peers.RelayAttachment{}, err
	}
	root := filepath.Join(home, ".jarvis", "office-memory", "library")
	switch kind {
	case "experience":
		// ref is "<configID>/<taskID>". Sanitize against traversal.
		clean := filepath.Clean(ref)
		if strings.Contains(clean, "..") || strings.HasPrefix(clean, string(filepath.Separator)) {
			return peers.RelayAttachment{}, errors.New("invalid experience ref")
		}
		full := filepath.Join(root, "experiences", clean+".md")
		body, err := os.ReadFile(full)
		if err != nil {
			return peers.RelayAttachment{}, err
		}
		return peers.RelayAttachment{
			Kind:  "experience",
			Ref:   ref,
			Title: titleFromMarkdown(string(body), ref),
			Body:  string(body),
		}, nil
	case "workflow":
		clean := filepath.Clean(ref)
		if strings.Contains(clean, "..") || strings.HasPrefix(clean, string(filepath.Separator)) {
			return peers.RelayAttachment{}, errors.New("invalid workflow ref")
		}
		full := filepath.Join(root, "workflows", clean+".md")
		body, err := os.ReadFile(full)
		if err != nil {
			return peers.RelayAttachment{}, err
		}
		return peers.RelayAttachment{
			Kind:  "workflow",
			Ref:   ref,
			Title: titleFromMarkdown(string(body), ref),
			Body:  string(body),
		}, nil
	case "missionExcerpt":
		// ref carries the text itself (already-trusted operator input).
		return peers.RelayAttachment{
			Kind:  "missionExcerpt",
			Ref:   "",
			Title: "Mission excerpt",
			Body:  ref,
		}, nil
	default:
		return peers.RelayAttachment{}, errors.New("unknown attachment kind: " + kind)
	}
}

// titleFromMarkdown extracts the first non-empty line (stripping the
// leading `# `) as a human-readable title; falls back to the ref.
func titleFromMarkdown(body, fallback string) string {
	for _, line := range strings.SplitN(body, "\n", 16) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		line = strings.TrimPrefix(line, "# ")
		line = strings.TrimPrefix(line, "## ")
		if line == "" {
			continue
		}
		if len(line) > 120 {
			line = line[:117] + "…"
		}
		return line
	}
	return fallback
}
