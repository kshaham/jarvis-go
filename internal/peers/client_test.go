package peers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_PostInboxSuccess(t *testing.T) {
	var gotAuth, gotContentType, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	peer := Peer{PublicURL: srv.URL, PairToken: "the-token"}
	msg := RelayMessage{ID: "m1", Content: "hi there"}

	if err := c.PostInbox(peer, msg); err != nil {
		t.Fatalf("PostInbox: %v", err)
	}
	if gotAuth != "Bearer the-token" {
		t.Fatalf("Authorization header wrong: %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type wrong: %q", gotContentType)
	}
	if gotPath != "/api/relay/inbox" {
		t.Fatalf("path wrong: %q", gotPath)
	}
	if !strings.Contains(gotBody, "hi there") {
		t.Fatalf("body missing content: %q", gotBody)
	}
}

func TestClient_PostInboxAccepts2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // 202
	}))
	defer srv.Close()
	c := NewClient()
	if err := c.PostInbox(Peer{PublicURL: srv.URL, PairToken: "t"}, RelayMessage{ID: "x"}); err != nil {
		t.Fatalf("expected 202 to succeed, got %v", err)
	}
}

func TestClient_PostInbox4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad token"))
	}))
	defer srv.Close()
	c := NewClient()
	err := c.PostInbox(Peer{PublicURL: srv.URL, PairToken: "t"}, RelayMessage{ID: "x"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should embed status code: %v", err)
	}
	if !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error should embed response preview: %v", err)
	}
}

func TestClient_PostInbox5xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient()
	err := c.PostInbox(Peer{PublicURL: srv.URL, PairToken: "t"}, RelayMessage{ID: "x"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}

func TestClient_PostInboxNetworkError(t *testing.T) {
	c := NewClientWithTimeout(200 * time.Millisecond)
	// Unroutable / closed port.
	err := c.PostInbox(Peer{PublicURL: "http://127.0.0.1:1", PairToken: "t"}, RelayMessage{ID: "x"})
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "post inbox") {
		t.Fatalf("unexpected error wrapper: %v", err)
	}
}

func TestClient_GetIdentitySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/identity" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"peerId":"remote-id","displayName":"Remote Team"}`))
	}))
	defer srv.Close()
	c := NewClient()
	id, err := c.GetIdentity(srv.URL)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if id.PeerID != "remote-id" || id.DisplayName != "Remote Team" {
		t.Fatalf("identity mismatch: %+v", id)
	}
}

func TestClient_GetIdentityNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not here"))
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.GetIdentity(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestClient_GetIdentityEmptyPeerID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"peerId":"","displayName":"x"}`))
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.GetIdentity(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "empty identity") {
		t.Fatalf("expected empty-identity error, got %v", err)
	}
}

func TestClient_GetIdentityBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.GetIdentity(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "decode identity") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClient_GetIdentityNetworkError(t *testing.T) {
	c := NewClientWithTimeout(200 * time.Millisecond)
	_, err := c.GetIdentity("http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "get identity") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestClient_PingDelegatesToGetIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"peerId":"p","displayName":"d"}`))
	}))
	defer srv.Close()
	c := NewClient()
	if err := c.Ping(Peer{PublicURL: srv.URL}); err != nil {
		t.Fatalf("Ping should succeed: %v", err)
	}

	// Failing peer.
	if err := c.Ping(Peer{PublicURL: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("Ping should fail for unreachable peer")
	}
}

func TestClient_DoesNotFollowRedirects(t *testing.T) {
	// Redirect target should never be hit because CheckRedirect returns
	// ErrUseLastResponse. The 302 itself is a non-2xx → error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	c := NewClient()
	err := c.PostInbox(Peer{PublicURL: srv.URL, PairToken: "t"}, RelayMessage{ID: "x"})
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("expected 302 surfaced as error (no redirect follow), got %v", err)
	}
}

func TestJoinURL(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://x.test", "/api/identity", "https://x.test/api/identity"},
		{"https://x.test/", "/api/identity", "https://x.test/api/identity"},
		{"https://x.test///", "/api/identity", "https://x.test/api/identity"},
		{"https://x.test", "api/identity", "https://x.test/api/identity"},
		{"https://x.test/", "api/identity", "https://x.test/api/identity"},
	}
	for _, tc := range cases {
		if got := joinURL(tc.base, tc.path); got != tc.want {
			t.Errorf("joinURL(%q,%q)=%q want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestReadPreview(t *testing.T) {
	// Trimmed and capped.
	got := readPreview(strings.NewReader("   hello world  "), 1024)
	if got != "hello world" {
		t.Fatalf("readPreview trim failed: %q", got)
	}
	// Cap at maxBytes.
	capped := readPreview(strings.NewReader(strings.Repeat("a", 100)), 10)
	if len(capped) != 10 {
		t.Fatalf("readPreview cap failed: len=%d", len(capped))
	}
	// Empty input.
	if readPreview(strings.NewReader(""), 16) != "" {
		t.Fatal("readPreview of empty should be empty")
	}
}
