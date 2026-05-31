package handler

// Round 5 -- residual peers.go arms:
//   - IdentityToken: the GeneratePairToken failure -> 500 (via the
//     generatePairToken seam).
//   - PeersAdd: the Registry.Add failure -> 400, driven by a live identity
//     server that returns an empty peerId so Registry.Add rejects it.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityToken_GenerateError500(t *testing.T) {
	p := newPeersBundle(t)

	orig := generatePairToken
	t.Cleanup(func() { generatePairToken = orig })
	generatePairToken = func() (string, error) { return "", errors.New("rand exhausted") }

	req := httptest.NewRequest("GET", "/api/identity/token", nil)
	rec := httptest.NewRecorder()
	p.IdentityToken(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when token generation fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPeersAdd_RegistryRejectsDuplicate400(t *testing.T) {
	p := newPeersBundle(t)
	// A live identity server that returns peerId "dup". GetIdentity succeeds,
	// but the peer is already registered, so Registry.Add returns
	// "already registered" -> 400.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"peerId":"dup","displayName":"Dup Peer"}`))
	}))
	t.Cleanup(srv.Close)
	seedPeer(t, p, "dup", "tok-existing")

	rec := postJSON(t, p.PeersAdd, "/api/peers", map[string]interface{}{
		"publicUrl": srv.URL,
		"pairToken": "tok-x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when registry rejects the duplicate, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Error("expected an error message body")
	}
}
