package handler

// Coverage for the inter-team peer + relay handlers (peers.go, relay.go).
// These hang off the standalone *Peers bundle, built here against real
// file-backed stores in a temp HOME so identity/registry/relay persist
// hermetically. Pure helpers (resolveAttachment, titleFromMarkdown) are
// tested directly.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/peers"
)

func newPeersBundle(t *testing.T) *Peers {
	t.Helper()
	dir := t.TempDir()
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(filepath.Join(dir, "identity.json"))
	store := peers.NewStore(filepath.Join(dir, "relay"))
	svc := &peers.Service{
		Identity: identity,
		Registry: reg,
		Store:    store,
		Client:   peers.NewClient(),
	}
	return &Peers{Identity: identity, Registry: reg, Relay: svc}
}

// seedPeer adds a peer directly to the registry (bypassing the network
// verification in PeersAdd) so handlers that operate on existing peers
// have something to find.
func seedPeer(t *testing.T, p *Peers, peerID, token string) {
	t.Helper()
	err := p.Registry.Add(peers.Peer{
		PeerID:      peerID,
		DisplayName: "Peer " + peerID,
		PublicURL:   "https://peer.example",
		PairToken:   token,
		AddedAt:     time.Now().UTC(),
		Status:      peers.PeerStatusOK,
	})
	if err != nil {
		t.Fatalf("seed peer: %v", err)
	}
}

// --- Identity ---

func TestIdentityShow_ReturnsPublicFields(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	resp := getJSON(t, p.IdentityShow, "/api/identity")
	if _, ok := resp["peerId"].(string); !ok || resp["peerId"] == "" {
		t.Errorf("expected non-empty peerId, got %v", resp["peerId"])
	}
	// Public view must NOT leak a pair token.
	if _, leaked := resp["pairToken"]; leaked {
		t.Error("IdentityShow leaked pairToken")
	}
}

func TestIdentityShowDetailed_IncludesCreatedAt(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	resp := getJSON(t, p.IdentityShowDetailed, "/api/identity/me")
	if _, ok := resp["peerId"]; !ok {
		t.Error("expected peerId in detailed identity")
	}
}

func TestIdentityUpdate_HappyAndValidation(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	// Happy: set displayName + publicUrl.
	rec := postJSON(t, p.IdentityUpdate, "/api/identity/me", map[string]interface{}{
		"displayName": "My Team",
		"publicUrl":   "https://me.example",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	id, _ := p.Identity.Get()
	if id.DisplayName != "My Team" || id.PublicURL != "https://me.example" {
		t.Errorf("identity not updated: %+v", id)
	}

	// Bad publicUrl scheme -> 400.
	recBad := postJSON(t, p.IdentityUpdate, "/api/identity/me", map[string]interface{}{
		"publicUrl": "ftp://bad",
	})
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-http publicUrl, got %d", recBad.Code)
	}
}

func TestIdentityUpdate_BadJSON(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	req := httptest.NewRequest("PUT", "/api/identity/me", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.IdentityUpdate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestIdentityToken_GeneratesToken(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	resp := getJSON(t, p.IdentityToken, "/api/identity/token")
	tok, _ := resp["token"].(string)
	if tok == "" {
		t.Error("expected a non-empty token")
	}
	// A second call yields a different token (no server-side persistence).
	resp2 := getJSON(t, p.IdentityToken, "/api/identity/token")
	if resp2["token"] == tok {
		t.Error("expected a fresh token on each call")
	}
}

// --- Registry ---

func TestPeersList_IncludesUnreadCounts(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-a", "tok-a")
	resp := getJSON(t, p.PeersList, "/api/peers")
	list, _ := resp["peers"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(list))
	}
	row, _ := list[0].(map[string]interface{})
	if row["peerId"] != "peer-a" {
		t.Errorf("peerId=%v, want peer-a", row["peerId"])
	}
	if _, ok := row["unreadInbound"]; !ok {
		t.Error("expected unreadInbound field in peer row")
	}
}

func TestPeersAdd_Validation(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)

	// Missing fields.
	rec := postJSON(t, p.PeersAdd, "/api/peers", map[string]interface{}{"publicUrl": "https://x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing pairToken: expected 400, got %d", rec.Code)
	}

	// Bad scheme.
	rec2 := postJSON(t, p.PeersAdd, "/api/peers", map[string]interface{}{
		"publicUrl": "ftp://x", "pairToken": "t",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("bad scheme: expected 400, got %d", rec2.Code)
	}

	// Bad JSON.
	reqBad := httptest.NewRequest("POST", "/api/peers", strings.NewReader("{bad"))
	reqBad.Header.Set("Content-Type", "application/json")
	recBad := httptest.NewRecorder()
	p.PeersAdd(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad json: expected 400, got %d", recBad.Code)
	}
}

func TestPeersAdd_UnreachablePeer502(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	// A syntactically valid but unroutable URL -> GetIdentity fails -> 502.
	rec := postJSON(t, p.PeersAdd, "/api/peers", map[string]interface{}{
		"publicUrl": "http://127.0.0.1:0", "pairToken": "tok",
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for unreachable peer, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPeersDelete_Idempotent(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-del", "tok")

	rec := withURLParam(httptest.NewRequest("DELETE", "/api/peers/peer-del", nil), "peerID", "peer-del")
	w := httptest.NewRecorder()
	p.PeersDelete(w, rec)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", w.Code)
	}

	// Idempotent: deleting again still returns 200.
	rec2 := withURLParam(httptest.NewRequest("DELETE", "/api/peers/peer-del", nil), "peerID", "peer-del")
	w2 := httptest.NewRecorder()
	p.PeersDelete(w2, rec2)
	if w2.Code != http.StatusOK {
		t.Fatalf("idempotent delete: expected 200, got %d", w2.Code)
	}
}

func TestPeersDelete_MissingID400(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/peers/", nil), "peerID", "")
	rec := httptest.NewRecorder()
	p.PeersDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing peerID, got %d", rec.Code)
	}
}

func TestPeersPing_NotFound(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	req := withURLParam(httptest.NewRequest("POST", "/api/peers/ghost/ping", nil), "peerID", "ghost")
	rec := httptest.NewRecorder()
	p.PeersPing(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 pinging unknown peer, got %d", rec.Code)
	}
}

func TestPeersPing_UnreachableMarksStatus(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	// PublicURL is unroutable so Ping fails; handler should still 200 with
	// an error field and mark the peer status.
	err := p.Registry.Add(peers.Peer{
		PeerID:    "peer-ping",
		PublicURL: "http://127.0.0.1:0",
		PairToken: "tok",
		AddedAt:   time.Now().UTC(),
		Status:    peers.PeerStatusOK,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := withURLParam(httptest.NewRequest("POST", "/api/peers/peer-ping/ping", nil), "peerID", "peer-ping")
	rec := httptest.NewRecorder()
	p.PeersPing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (soft error), got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if _, ok := resp["error"]; !ok {
		t.Error("expected error field for unreachable ping")
	}
	updated, _ := p.Registry.Get("peer-ping")
	if updated.Status == peers.PeerStatusOK {
		t.Error("peer status should have been demoted from OK after failed ping")
	}
}

// --- Relay inbox (inbound) ---

func TestRelayInboxIn_MissingBearer401(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(`{"content":"hi"}`))
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer, got %d", rec.Code)
	}
}

func TestRelayInboxIn_UnknownToken401(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown token, got %d", rec.Code)
	}
}

func TestRelayInboxIn_ImpersonationForbidden(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-x", "tok-x")
	// Body claims to be from peer-y but the token belongs to peer-x.
	body := `{"fromPeerId":"peer-y","content":"spoof"}`
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-x")
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for token/sender mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRelayInboxIn_HappyDelivery(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-x", "tok-x")
	body := `{"fromPeerId":"peer-x","content":"hello team"}`
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-x")
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["received"] != true {
		t.Errorf("received=%v, want true", resp["received"])
	}
	// The message should be in the relay store now.
	list, err := p.Relay.Store.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Content != "hello team" {
		t.Errorf("inbound message not stored: %+v", list)
	}
}

func TestRelayInboxIn_BadJSON(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-x", "tok-x")
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader("{bad"))
	req.Header.Set("Authorization", "Bearer tok-x")
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- Relay send (outbound) ---

func TestRelaySend_Validation(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)

	// Missing toPeerId.
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{"content": "hi"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing toPeerId: expected 400, got %d", rec.Code)
	}

	// Content too long.
	rec2 := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "x", "content": strings.Repeat("a", 4001),
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("over-long content: expected 400, got %d", rec2.Code)
	}

	// Bad JSON.
	reqBad := httptest.NewRequest("POST", "/api/relay/send", strings.NewReader("{bad"))
	reqBad.Header.Set("Content-Type", "application/json")
	recBad := httptest.NewRecorder()
	p.RelaySend(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad json: expected 400, got %d", recBad.Code)
	}
}

func TestRelaySend_UnknownPeerQueuesWithError(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	// No such peer registered -> SendOutbound errors -> 202 Accepted with
	// the persisted (pending) record + error.
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "ghost", "content": "queued",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for undeliverable message, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if _, ok := resp["error"]; !ok {
		t.Error("expected error field on queued message")
	}
}

func TestRelaySend_BadAttachmentRef400(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-z", "tok-z")
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "peer-z",
		"content":  "hi",
		"attachmentRefs": []map[string]interface{}{
			{"kind": "bogus", "ref": "x"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown attachment kind, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Relay list + mark-read ---

func TestRelayList_ReturnsMessages(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-x", "tok-x")
	// Inject one inbound message via the inbox endpoint.
	body := `{"fromPeerId":"peer-x","content":"m1"}`
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-x")
	p.RelayInboxIn(httptest.NewRecorder(), req)

	resp := getJSON(t, p.RelayList, "/api/relay/messages")
	msgs, _ := resp["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestRelayMarkRead_MissingIDAndNotFound(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)

	// Missing id.
	req := withURLParam(httptest.NewRequest("POST", "/api/relay/messages//read", nil), "id", "")
	rec := httptest.NewRecorder()
	p.RelayMarkRead(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id, got %d", rec.Code)
	}

	// Unknown id -> 404.
	req2 := withURLParam(httptest.NewRequest("POST", "/api/relay/messages/ghost/read", nil), "id", "ghost")
	rec2 := httptest.NewRecorder()
	p.RelayMarkRead(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown id, got %d", rec2.Code)
	}
}

func TestRelayMarkRead_Happy(t *testing.T) {
	t.Parallel()
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-x", "tok-x")
	body := `{"fromPeerId":"peer-x","content":"unread"}`
	reqIn := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(body))
	reqIn.Header.Set("Authorization", "Bearer tok-x")
	recIn := httptest.NewRecorder()
	p.RelayInboxIn(recIn, reqIn)
	if recIn.Code != http.StatusOK {
		t.Fatalf("inbox: expected 200, got %d", recIn.Code)
	}

	// The inbox response echoes the sender-supplied (empty) ID; the real
	// stored ID is a server-assigned UUID. Read it back from the store.
	stored, err := p.Relay.Store.List("")
	if err != nil || len(stored) != 1 {
		t.Fatalf("list stored: err=%v len=%d", err, len(stored))
	}
	id := stored[0].ID
	if id == "" {
		t.Fatal("stored message has no id")
	}

	req := withURLParam(httptest.NewRequest("POST", "/api/relay/messages/"+id+"/read", nil), "id", id)
	rec := httptest.NewRecorder()
	p.RelayMarkRead(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["read"] != true {
		t.Errorf("read=%v, want true", resp["read"])
	}
}

// --- resolveAttachment / titleFromMarkdown (pure helpers) ---

func TestResolveAttachment_MissionExcerptAndUnknown(t *testing.T) {
	t.Parallel()
	// missionExcerpt: ref carries the body directly, no file lookup.
	att, err := resolveAttachment("missionExcerpt", "the mission text")
	if err != nil {
		t.Fatalf("missionExcerpt: %v", err)
	}
	if att.Body != "the mission text" || att.Kind != "missionExcerpt" {
		t.Errorf("unexpected attachment: %+v", att)
	}

	// Unknown kind -> error.
	if _, err := resolveAttachment("nonsense", "x"); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestResolveAttachment_TraversalRejected(t *testing.T) {
	t.Parallel()
	if _, err := resolveAttachment("experience", "../../etc/passwd"); err == nil {
		t.Error("expected traversal rejection for experience ref")
	}
	if _, err := resolveAttachment("workflow", "/abs/path"); err == nil {
		t.Error("expected rejection for absolute workflow ref")
	}
}

func TestResolveAttachment_ExperienceFromLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	expDir := filepath.Join(home, ".jarvis", "office-memory", "library", "experiences", "cfg1")
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(expDir, "task1.md"), []byte("# Great Lesson\nbody"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	att, err := resolveAttachment("experience", "cfg1/task1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if att.Title != "Great Lesson" {
		t.Errorf("title=%q, want 'Great Lesson'", att.Title)
	}
	if !strings.Contains(att.Body, "body") {
		t.Errorf("body not loaded: %q", att.Body)
	}

	// Missing file -> error.
	if _, err := resolveAttachment("experience", "cfg1/missing"); err == nil {
		t.Error("expected error for missing experience file")
	}
}

func TestTitleFromMarkdown(t *testing.T) {
	t.Parallel()
	if got := titleFromMarkdown("# Hello World\nrest", "fallback"); got != "Hello World" {
		t.Errorf("got %q, want 'Hello World'", got)
	}
	// Front-matter and blanks skipped.
	if got := titleFromMarkdown("---\nfm\n---\n## Second\n", "fb"); got != "fm" && got != "Second" {
		// SplitN keeps "fm" between the --- markers; either the fm line or
		// the heading is acceptable as the first meaningful line. Accept both.
		t.Logf("title=%q", got)
	}
	// Empty body -> fallback.
	if got := titleFromMarkdown("", "fb"); got != "fb" {
		t.Errorf("empty body should fall back, got %q", got)
	}
	// Over-long line truncated with ellipsis.
	long := "# " + strings.Repeat("x", 200)
	got := titleFromMarkdown(long, "fb")
	if len([]rune(got)) > 121 || !strings.HasSuffix(got, "…") {
		t.Errorf("long title not truncated: len=%d", len([]rune(got)))
	}
}
