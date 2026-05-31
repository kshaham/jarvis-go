package handler

// Round 5 -- residual relay.go branches that need a live peer / real
// library / fault injection:
//   - RelayInboxIn: ReceiveInbound persistence failure -> 500 (read-only store).
//   - RelaySend: successful delivery to a live peer -> 200 with message,
//     plus the attachment-ref append arm resolving a real library file.
//   - resolveAttachment: os.UserHomeDir failure (HOME unset).
//   - titleFromMarkdown: a "## " heading that collapses to empty is skipped.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/peers"
)

// newDeliverablePeer stands up an httptest server that ACKs inbound relay
// POSTs, registers it as a peer, and returns the bundle. SendOutbound to
// this peer therefore succeeds end-to-end.
func newDeliverablePeer(t *testing.T) (*Peers, *httptest.Server) {
	t.Helper()
	p := newPeersBundle(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Accept any inbox POST.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	t.Cleanup(srv.Close)
	if err := p.Registry.Add(peers.Peer{
		PeerID:      "peer-live",
		DisplayName: "Live",
		PublicURL:   srv.URL,
		PairToken:   "tok-live",
		AddedAt:     time.Now().UTC(),
		Status:      peers.PeerStatusOK,
	}); err != nil {
		t.Fatalf("seed live peer: %v", err)
	}
	return p, srv
}

func TestRelaySend_SuccessfulDelivery200(t *testing.T) {
	t.Parallel()
	p, _ := newDeliverablePeer(t)
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "peer-live",
		"content":  "delivered hello",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful delivery, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	msg, ok := resp["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected message object, got %v", resp["message"])
	}
	if msg["content"] != "delivered hello" {
		t.Errorf("content=%v, want 'delivered hello'", msg["content"])
	}
	// Delivered messages carry a non-nil deliveredAt timestamp.
	if msg["deliveredAt"] == nil {
		t.Error("expected deliveredAt set on successful delivery")
	}
	// No error field on the happy path.
	if _, hasErr := resp["error"]; hasErr {
		t.Error("did not expect an error field on successful send")
	}
}

func TestRelaySend_AttachmentRefAppendedAndDelivered(t *testing.T) {
	t.Parallel()
	p, _ := newDeliverablePeer(t)
	// Populate the library so a missionExcerpt ref resolves to a real
	// attachment and gets appended to body.Attachments (line 115).
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "peer-live",
		"content":  "with attachment",
		"attachmentRefs": []map[string]interface{}{
			{"kind": "missionExcerpt", "ref": "ship the thing"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	msg, _ := resp["message"].(map[string]interface{})
	atts, ok := msg["attachments"].([]interface{})
	if !ok || len(atts) != 1 {
		t.Fatalf("expected 1 resolved attachment, got %v", msg["attachments"])
	}
	att, _ := atts[0].(map[string]interface{})
	if att["body"] != "ship the thing" {
		t.Errorf("attachment body=%v, want 'ship the thing'", att["body"])
	}
}

func TestRelaySend_ExperienceRefFromLibraryDelivered(t *testing.T) {
	p, _ := newDeliverablePeer(t)
	// Build a real experience library file so the experience-kind ref path
	// (file read + titleFromMarkdown) is exercised alongside delivery.
	home := t.TempDir()
	t.Setenv("HOME", home)
	expDir := filepath.Join(home, ".jarvis", "office-memory", "library", "experiences", "cfgX")
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(expDir, "taskX.md"), []byte("# Lesson Title\nbody text"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := postJSON(t, p.RelaySend, "/api/relay/send", map[string]interface{}{
		"toPeerId": "peer-live",
		"content":  "exp",
		"attachmentRefs": []map[string]interface{}{
			{"kind": "experience", "ref": "cfgX/taskX"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	msg, _ := resp["message"].(map[string]interface{})
	atts, _ := msg["attachments"].([]interface{})
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	att, _ := atts[0].(map[string]interface{})
	if att["title"] != "Lesson Title" {
		t.Errorf("title=%v, want 'Lesson Title'", att["title"])
	}
}

func TestRelayInboxIn_ReceivePersistFailure500(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only semantics differ on Windows")
	}
	dir := t.TempDir()
	relayRoot := filepath.Join(dir, "relay")
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Build a bundle whose relay store points at a now read-only root so
	// AppendInbound's MkdirAll/WriteFile fails -> ReceiveInbound errors ->
	// RelayInboxIn returns 500.
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(filepath.Join(dir, "identity.json"))
	store := peers.NewStore(relayRoot)
	svc := &peers.Service{Identity: identity, Registry: reg, Store: store, Client: peers.NewClient()}
	p := &Peers{Identity: identity, Registry: reg, Relay: svc}
	seedPeer(t, p, "peer-ro", "tok-ro")

	// Make the relay root read-only AFTER seeding so inbox/sent subdirs
	// cannot be created/written.
	if err := os.Chmod(relayRoot, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(relayRoot, 0o755) })

	body := `{"fromPeerId":"peer-ro","content":"will fail to persist"}`
	req := httptest.NewRequest("POST", "/api/relay/inbox", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-ro")
	rec := httptest.NewRecorder()
	p.RelayInboxIn(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when persistence fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "receive") {
		t.Errorf("expected 'receive' in error body, got %q", rec.Body.String())
	}
}

func TestResolveAttachment_HomeDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir uses USERPROFILE on Windows")
	}
	// os.UserHomeDir returns an error on unix when $HOME is empty.
	t.Setenv("HOME", "")
	_, err := resolveAttachment("experience", "cfg/task")
	if err == nil {
		t.Fatal("expected error when HOME is unset")
	}
}

func TestTitleFromMarkdown_H2HeadingStripped(t *testing.T) {
	t.Parallel()
	// A "## " heading has its marker stripped and is returned as the title.
	if got := titleFromMarkdown("## Section Two\nbody", "fb"); got != "Section Two" {
		t.Errorf("got %q, want 'Section Two'", got)
	}
	// Front-matter fence lines ("---") are skipped before the heading.
	if got := titleFromMarkdown("---\n## After FM\n", "fb"); got != "After FM" {
		t.Errorf("got %q, want 'After FM' (front matter skipped)", got)
	}
}
