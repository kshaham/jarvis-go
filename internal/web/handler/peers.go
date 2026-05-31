package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/peers"
	"github.com/kshaham/jarvis/internal/web/request"
)

// generatePairToken is a seam over peers.GeneratePairToken so the rare
// crypto/rand exhaustion error can be exercised deterministically in tests.
// Production behaviour is identical.
var generatePairToken = peers.GeneratePairToken

// Peers is the dedicated handler bundle for the inter-team feature.
// Kept off the main Context to avoid further inflating its field list —
// the server constructor builds one Peers and passes it to the router
// alongside the existing Context.
type Peers struct {
	Identity *peers.IdentityStore
	Registry *peers.Registry
	Relay    *peers.Service
}

// ---------------------------------------------------------------------
// Local identity
// ---------------------------------------------------------------------

// IdentityShow is the public, unauthenticated GET /api/identity. Peer
// jarvis instances hit this during paste-token registration to verify
// the URL the operator pasted is a real jarvis. Returns only what's
// safe to expose — peerID + displayName, never the pair token.
func (p *Peers) IdentityShow(w http.ResponseWriter, _ *http.Request) {
	id, err := p.Identity.Get()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "identity: "+err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"peerId":      id.PeerID,
		"displayName": id.DisplayName,
	})
}

// IdentityShowDetailed is the authenticated GET /api/identity/me —
// includes the publicURL + createdAt the operator may want to display
// or edit. Mounted on the same auth as the rest of /api/peers.
func (p *Peers) IdentityShowDetailed(w http.ResponseWriter, _ *http.Request) {
	id, err := p.Identity.Get()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "identity: "+err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, id)
}

// IdentityUpdate sets displayName / publicURL on the local identity.
// PeerID is immutable. Empty fields are left unchanged.
func (p *Peers) IdentityUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		DisplayName string `json:"displayName"`
		PublicURL   string `json:"publicUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	body.PublicURL = strings.TrimSpace(body.PublicURL)
	if body.PublicURL != "" && !strings.HasPrefix(body.PublicURL, "http://") && !strings.HasPrefix(body.PublicURL, "https://") {
		request.WriteError(w, http.StatusBadRequest, "publicUrl must start with http(s)://")
		return
	}
	updated, err := p.Identity.Update(body.DisplayName, body.PublicURL)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "save identity: "+err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, updated)
}

// IdentityToken returns a freshly generated pair token the operator
// can paste into a peer's Add-Peer wizard. Each call returns a new
// token — there's no server-side persistence; the secret only exists
// once the peer adds us and stores it in their registry.
func (p *Peers) IdentityToken(w http.ResponseWriter, _ *http.Request) {
	token, err := generatePairToken()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "generate token: "+err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"token": token})
}

// ---------------------------------------------------------------------
// Peer registry
// ---------------------------------------------------------------------

// PeersList returns every registered peer plus the unread inbound
// count per peer so the SPA can render badges without a second roundtrip.
func (p *Peers) PeersList(w http.ResponseWriter, _ *http.Request) {
	list := p.Registry.List()
	counts, err := p.Relay.Store.UnreadInboundByPeer()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "unread counts: "+err.Error())
		return
	}
	type row struct {
		peers.Peer
		UnreadInbound int `json:"unreadInbound"`
	}
	out := make([]row, 0, len(list))
	for _, peer := range list {
		out = append(out, row{Peer: peer, UnreadInbound: counts[peer.PeerID]})
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"peers": out})
}

// PeersAdd registers a new peer. The operator pastes the peer's URL +
// the pair token the other side gave them; we GET /api/identity on
// the peer to verify the URL points to a real jarvis and to capture
// the peerID + displayName. The displayName from the registration
// form takes precedence if provided.
func (p *Peers) PeersAdd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		PublicURL   string `json:"publicUrl"`
		PairToken   string `json:"pairToken"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.PublicURL = strings.TrimSpace(body.PublicURL)
	body.PairToken = strings.TrimSpace(body.PairToken)
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	if body.PublicURL == "" || body.PairToken == "" {
		request.WriteError(w, http.StatusBadRequest, "publicUrl and pairToken are required")
		return
	}
	if !strings.HasPrefix(body.PublicURL, "http://") && !strings.HasPrefix(body.PublicURL, "https://") {
		request.WriteError(w, http.StatusBadRequest, "publicUrl must start with http(s)://")
		return
	}
	// Verify the peer is real before persisting.
	peerIdentity, err := p.Relay.Client.GetIdentity(body.PublicURL)
	if err != nil {
		request.WriteError(w, http.StatusBadGateway, "peer unreachable: "+err.Error())
		return
	}
	displayName := body.DisplayName
	if displayName == "" {
		displayName = peerIdentity.DisplayName
	}
	peer := peers.Peer{
		PeerID:      peerIdentity.PeerID,
		DisplayName: displayName,
		PublicURL:   body.PublicURL,
		PairToken:   body.PairToken,
		AddedAt:     time.Now().UTC(),
		Status:      peers.PeerStatusOK,
	}
	if err := p.Registry.Add(peer); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, peer)
}

// PeersDelete is idempotent — the SPA can always send DELETE and
// expect 200, even if the peer was already gone.
func (p *Peers) PeersDelete(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	if peerID == "" {
		request.WriteError(w, http.StatusBadRequest, "peerID is required")
		return
	}
	if err := p.Registry.Remove(peerID); err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// PeersPing health-checks one peer by hitting their /api/identity and
// updates the registry's Status field. The SPA polls this from the
// "Ping" button on a peer card.
func (p *Peers) PeersPing(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	peer, ok := p.Registry.Get(peerID)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "peer not found")
		return
	}
	err := p.Relay.Client.Ping(peer)
	if err != nil {
		// Don't 500 — surface as a successful response with the new
		// status. The SPA reads the status from the response.
		status := peers.PeerStatusUnreachable
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			status = peers.PeerStatusUnauthorized
		}
		p.Registry.MarkStatus(peer.PeerID, status, err.Error())
		updated, _ := p.Registry.Get(peer.PeerID)
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"peer":  updated,
			"error": err.Error(),
		})
		return
	}
	p.Registry.MarkStatus(peer.PeerID, peers.PeerStatusOK, "")
	updated, _ := p.Registry.Get(peer.PeerID)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"peer": updated})
}

// ---------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------

// ErrNoRelayService is returned (logged really) when a handler path
// is hit but the bundle wasn't initialised — only possible if a
// future caller forgets to wire up the registry; keep the type around
// so the wiring tests can assert on it.
var ErrNoRelayService = errors.New("relay service not configured")
