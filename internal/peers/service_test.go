package peers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// recordingInjector captures injected messages and returns a configurable
// "live" result.
type recordingInjector struct {
	mu       sync.Mutex
	live     bool
	calls    int
	lastFrom string
	lastName string
	lastBody string
}

func (r *recordingInjector) InjectRelayMessage(fromPeerID, fromDisplayName, content string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastFrom = fromPeerID
	r.lastName = fromDisplayName
	r.lastBody = content
	return r.live
}

// recordingBroadcaster captures emitted SSE events.
type recordingBroadcaster struct {
	mu     sync.Mutex
	events []struct {
		name string
		data interface{}
	}
}

func (b *recordingBroadcaster) fn() EventBroadcaster {
	return func(event string, data interface{}) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.events = append(b.events, struct {
			name string
			data interface{}
		}{event, data})
	}
}

func (b *recordingBroadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func (b *recordingBroadcaster) names() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.events))
	for i, e := range b.events {
		out[i] = e.name
	}
	return out
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{
		Identity: NewIdentityStore(filepath.Join(dir, "identity.json")),
		Registry: reg,
		Store:    NewStore(dir),
	}, dir
}

func TestService_SendOutboundSuccess(t *testing.T) {
	svc, _ := newTestService(t)

	var delivered bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := svc.Registry.Add(Peer{PeerID: "peer1", PublicURL: srv.URL, PairToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	svc.Client = NewClient()
	bc := &recordingBroadcaster{}
	svc.Broadcast = bc.fn()

	msg, err := svc.SendOutbound("peer1", "hello peer", nil)
	if err != nil {
		t.Fatalf("SendOutbound: %v", err)
	}
	if !delivered {
		t.Fatal("peer endpoint was not called")
	}
	if msg.ID == "" {
		t.Fatal("returned message missing ID")
	}
	if msg.DeliveredAt == nil {
		t.Fatal("DeliveredAt should be set on successful delivery")
	}
	if msg.ToPeerID != "peer1" {
		t.Fatalf("ToPeerID wrong: %q", msg.ToPeerID)
	}

	// Registry status should flip to OK.
	p, _ := svc.Registry.Get("peer1")
	if p.Status != PeerStatusOK {
		t.Fatalf("expected peer status OK, got %q", p.Status)
	}

	// Stored message should reflect delivery.
	stored, _ := svc.Store.List("peer1")
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored message, got %d", len(stored))
	}
	if stored[0].DeliveredAt == nil {
		t.Fatal("stored message DeliveredAt not persisted")
	}

	// Broadcast emitted relaySent.
	if bc.count() != 1 {
		t.Fatalf("expected 1 broadcast, got %d", bc.count())
	}
	if bc.names()[0] != "office:relaySent" {
		t.Fatalf("unexpected event name: %q", bc.names()[0])
	}
}

func TestService_SendOutboundEmptyRejected(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.SendOutbound("peer1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "content or attachments") {
		t.Fatalf("expected empty-message error, got %v", err)
	}
}

func TestService_SendOutboundWithAttachmentOnly(t *testing.T) {
	svc, _ := newTestService(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: srv.URL, PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	svc.Client = NewClient()
	atts := []RelayAttachment{{Kind: "experience", Title: "T", Body: "B"}}
	msg, err := svc.SendOutbound("p", "", atts)
	if err != nil {
		t.Fatalf("SendOutbound with attachment only: %v", err)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachment lost: %+v", msg.Attachments)
	}
}

func TestService_SendOutboundUnknownPeer(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.SendOutbound("ghost", "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected not-registered error, got %v", err)
	}
}

func TestService_SendOutboundDeliveryFailureNonFatal(t *testing.T) {
	svc, _ := newTestService(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: srv.URL, PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	svc.Client = NewClient()

	msg, err := svc.SendOutbound("p", "hi", nil)
	// Delivery failed → error returned, but message is still stored.
	if err == nil {
		t.Fatal("expected delivery error")
	}
	if !strings.Contains(err.Error(), "deliver to peer") {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID == "" {
		t.Fatal("message should still be returned on delivery failure")
	}
	if msg.DeliveredAt != nil {
		t.Fatal("DeliveredAt must be nil on failed delivery")
	}
	// Stored with nil DeliveredAt for later retry.
	stored, _ := svc.Store.List("p")
	if len(stored) != 1 || stored[0].DeliveredAt != nil {
		t.Fatalf("expected pending stored message, got %+v", stored)
	}
	// Registry should reflect unreachable.
	p, _ := svc.Registry.Get("p")
	if p.Status != PeerStatusUnreachable {
		t.Fatalf("expected unreachable status, got %q", p.Status)
	}
}

func TestService_SendOutboundUnauthorizedStatus(t *testing.T) {
	svc, _ := newTestService(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden) // 403
	}))
	defer srv.Close()
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: srv.URL, PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	svc.Client = NewClient()
	_, err := svc.SendOutbound("p", "hi", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	p, _ := svc.Registry.Get("p")
	if p.Status != PeerStatusUnauthorized {
		t.Fatalf("expected unauthorized status from 403, got %q", p.Status)
	}
}

func TestService_SendOutboundNilClientPersistsOnly(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: "https://x.test", PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	// No Client set → message persists but no delivery attempted.
	msg, err := svc.SendOutbound("p", "hi", nil)
	if err != nil {
		t.Fatalf("SendOutbound with nil client: %v", err)
	}
	if msg.DeliveredAt != nil {
		t.Fatal("no delivery attempted, DeliveredAt should be nil")
	}
	stored, _ := svc.Store.List("p")
	if len(stored) != 1 {
		t.Fatalf("expected stored message, got %d", len(stored))
	}
}

func TestService_SendOutboundNoBroadcaster(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: "https://x.test", PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	// Broadcast nil must not panic.
	if _, err := svc.SendOutbound("p", "hi", nil); err != nil {
		t.Fatalf("SendOutbound: %v", err)
	}
}

func TestService_ReceiveInboundInjectsAndBroadcasts(t *testing.T) {
	svc, _ := newTestService(t)
	inj := &recordingInjector{live: true}
	bc := &recordingBroadcaster{}
	svc.Injector = inj
	svc.Broadcast = bc.fn()

	msg := RelayMessage{
		FromPeerID:      "remote",
		FromDisplayName: "Remote Team",
		Content:         "incoming",
	}
	if err := svc.ReceiveInbound(msg); err != nil {
		t.Fatalf("ReceiveInbound: %v", err)
	}

	// Injected once with the raw content.
	if inj.calls != 1 {
		t.Fatalf("expected 1 inject call, got %d", inj.calls)
	}
	if inj.lastFrom != "remote" || inj.lastName != "Remote Team" {
		t.Fatalf("inject metadata wrong: from=%q name=%q", inj.lastFrom, inj.lastName)
	}
	if inj.lastBody != "incoming" {
		t.Fatalf("inject body wrong: %q", inj.lastBody)
	}

	// Stored as inbound.
	stored, _ := svc.Store.List("")
	if len(stored) != 1 || stored[0].Direction != "inbound" {
		t.Fatalf("inbound not stored correctly: %+v", stored)
	}

	// Broadcast emitted relayReceived with live=true.
	if bc.count() != 1 || bc.names()[0] != "office:relayReceived" {
		t.Fatalf("unexpected broadcast: %v", bc.names())
	}
	data, ok := bc.events[0].data.(map[string]interface{})
	if !ok {
		t.Fatalf("broadcast data not a map: %T", bc.events[0].data)
	}
	if data["live"] != true {
		t.Fatalf("expected live=true in broadcast, got %v", data["live"])
	}
}

func TestService_ReceiveInboundAppendsAttachments(t *testing.T) {
	svc, _ := newTestService(t)
	inj := &recordingInjector{live: false}
	svc.Injector = inj

	msg := RelayMessage{
		FromPeerID:      "remote",
		FromDisplayName: "Remote",
		Content:         "base content",
		Attachments: []RelayAttachment{
			{Kind: "experience", Title: "Lesson", Body: "the body"},
		},
	}
	if err := svc.ReceiveInbound(msg); err != nil {
		t.Fatalf("ReceiveInbound: %v", err)
	}
	// Injected body should include the appendix and the attachment.
	if !strings.Contains(inj.lastBody, "base content") {
		t.Fatalf("inject body missing base content: %q", inj.lastBody)
	}
	if !strings.Contains(inj.lastBody, "Knowledge from Remote") {
		t.Fatalf("inject body missing knowledge appendix: %q", inj.lastBody)
	}
	if !strings.Contains(inj.lastBody, "experience: Lesson") {
		t.Fatalf("inject body missing attachment heading: %q", inj.lastBody)
	}
	if !strings.Contains(inj.lastBody, "the body") {
		t.Fatalf("inject body missing attachment body: %q", inj.lastBody)
	}
}

func TestService_ReceiveInboundNoInjectorNoBroadcaster(t *testing.T) {
	svc, _ := newTestService(t)
	// Both nil — must persist without panicking.
	if err := svc.ReceiveInbound(RelayMessage{FromPeerID: "r", Content: "x"}); err != nil {
		t.Fatalf("ReceiveInbound: %v", err)
	}
	stored, _ := svc.Store.List("")
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored message, got %d", len(stored))
	}
}

func TestService_ReceiveInboundLiveFalse(t *testing.T) {
	svc, _ := newTestService(t)
	inj := &recordingInjector{live: false}
	bc := &recordingBroadcaster{}
	svc.Injector = inj
	svc.Broadcast = bc.fn()
	if err := svc.ReceiveInbound(RelayMessage{FromPeerID: "r", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	data := bc.events[0].data.(map[string]interface{})
	if data["live"] != false {
		t.Fatalf("expected live=false, got %v", data["live"])
	}
}

func TestStatusFromErr(t *testing.T) {
	cases := []struct {
		errMsg string
		want   PeerStatus
	}{
		{"peer 401: nope", PeerStatusUnauthorized},
		{"peer 403: nope", PeerStatusUnauthorized},
		{"the user is unauthorized", PeerStatusUnauthorized},
		{"access forbidden", PeerStatusUnauthorized},
		{"connection refused", PeerStatusUnreachable},
		{"peer 500: boom", PeerStatusUnreachable},
		{"timeout", PeerStatusUnreachable},
	}
	for _, tc := range cases {
		got := statusFromErr(errString(tc.errMsg))
		if got != tc.want {
			t.Errorf("statusFromErr(%q)=%q want %q", tc.errMsg, got, tc.want)
		}
	}
	// Nil error → OK.
	if got := statusFromErr(nil); got != PeerStatusOK {
		t.Fatalf("statusFromErr(nil)=%q want ok", got)
	}
}

func TestContains(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello world", "lo wo", true},
		{"hello world", "xyz", false},
		{"short", "longer needle", false},
		{"anything", "", false},
		{"", "x", false},
		{"abc", "abc", true},
	}
	for _, tc := range cases {
		if got := contains(tc.hay, tc.needle); got != tc.want {
			t.Errorf("contains(%q,%q)=%v want %v", tc.hay, tc.needle, got, tc.want)
		}
	}
}

// errString is a tiny error helper so statusFromErr tests don't pull in
// fmt.Errorf noise.
type errString string

func (e errString) Error() string { return string(e) }
