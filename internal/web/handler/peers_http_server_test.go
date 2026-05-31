package handler

// Round-2 branch fills for PeersAdd / PeersPing happy + unauthorized paths.
// Stand up a real httptest.Server that serves /api/identity so the peers
// Client.GetIdentity round-trip succeeds (or returns 401), exercising the
// branches that need a reachable peer HTTP endpoint.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/peers"
)

// identityServer serves /api/identity with the given peerID/displayName, or a
// 401 when unauthorized is true.
func identityServer(t *testing.T, peerID, displayName string, unauthorized bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/identity" {
			http.NotFound(w, r)
			return
		}
		if unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"401 unauthorized"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"peerId": peerID, "displayName": displayName})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newPeersBundleAt(t *testing.T, dir string) *Peers {
	t.Helper()
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(filepath.Join(dir, "identity.json"))
	svc := &peers.Service{
		Identity: identity,
		Registry: reg,
		Store:    peers.NewStore(filepath.Join(dir, "relay")),
		Client:   peers.NewClient(),
	}
	return &Peers{Identity: identity, Registry: reg, Relay: svc}
}

func TestPeersAdd_HappyPathRegistersPeer(t *testing.T) {
	t.Parallel()
	srv := identityServer(t, "peer-xyz", "Remote Team", false)
	p := newPeersBundleAt(t, t.TempDir())

	req := httptest.NewRequest("POST", "/api/peers", jsonBody(t, map[string]interface{}{
		"publicUrl": srv.URL,
		"pairToken": "tok-abc-123",
		// No displayName -> handler falls back to the peer's reported name.
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.PeersAdd(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got peers.Peer
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PeerID != "peer-xyz" {
		t.Errorf("peerID=%q, want peer-xyz", got.PeerID)
	}
	if got.DisplayName != "Remote Team" {
		t.Errorf("displayName=%q, want the peer-reported 'Remote Team' fallback", got.DisplayName)
	}
	// It should be persisted in the registry.
	if _, ok := p.Registry.Get("peer-xyz"); !ok {
		t.Error("peer not persisted in registry after add")
	}
}

func TestPeersAdd_DisplayNameOverride(t *testing.T) {
	t.Parallel()
	srv := identityServer(t, "peer-ovr", "Server Name", false)
	p := newPeersBundleAt(t, t.TempDir())

	req := httptest.NewRequest("POST", "/api/peers", jsonBody(t, map[string]interface{}{
		"publicUrl":   srv.URL,
		"pairToken":   "tok",
		"displayName": "Operator Override",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.PeersAdd(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got peers.Peer
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.DisplayName != "Operator Override" {
		t.Errorf("displayName=%q, want the operator override", got.DisplayName)
	}
}

func TestPeersPing_SuccessMarksOK(t *testing.T) {
	t.Parallel()
	srv := identityServer(t, "peer-ping", "Pinged", false)
	dir := t.TempDir()
	p := newPeersBundleAt(t, dir)
	if err := p.Registry.Add(peers.Peer{
		PeerID: "peer-ping", DisplayName: "Pinged", PublicURL: srv.URL,
		PairToken: "tok", AddedAt: time.Now().UTC(), Status: peers.PeerStatusUnknown,
	}); err != nil {
		t.Fatalf("seed peer: %v", err)
	}

	req := withURLParam(httptest.NewRequest("POST", "/api/peers/peer-ping/ping", nil), "peerID", "peer-ping")
	rec := httptest.NewRecorder()
	p.PeersPing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, _ := p.Registry.Get("peer-ping")
	if updated.Status != peers.PeerStatusOK {
		t.Errorf("status=%q, want OK after a successful ping", updated.Status)
	}
}

func TestPeersPing_UnauthorizedMarksUnauthorized(t *testing.T) {
	t.Parallel()
	srv := identityServer(t, "peer-401", "Locked", true)
	dir := t.TempDir()
	p := newPeersBundleAt(t, dir)
	if err := p.Registry.Add(peers.Peer{
		PeerID: "peer-401", DisplayName: "Locked", PublicURL: srv.URL,
		PairToken: "tok", AddedAt: time.Now().UTC(), Status: peers.PeerStatusUnknown,
	}); err != nil {
		t.Fatalf("seed peer: %v", err)
	}

	req := withURLParam(httptest.NewRequest("POST", "/api/peers/peer-401/ping", nil), "peerID", "peer-401")
	rec := httptest.NewRecorder()
	p.PeersPing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (ping surfaces status in body), got %d: %s", rec.Code, rec.Body.String())
	}
	updated, _ := p.Registry.Get("peer-401")
	if updated.Status != peers.PeerStatusUnauthorized {
		t.Errorf("status=%q, want Unauthorized after a 401 ping", updated.Status)
	}
}
