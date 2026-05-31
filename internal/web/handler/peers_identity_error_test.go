package handler

// Error-branch coverage for the Identity handlers in peers.go. A corrupt
// identity.json makes IdentityStore.Get() fail (json parse error), driving
// the 500 paths in IdentityShow / IdentityShowDetailed / IdentityUpdate that
// the happy-path tests don't reach.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/peers"
)

// corruptIdentityPeers builds a *Peers whose IdentityStore points at a file
// containing invalid JSON, so Get() returns a parse error.
func corruptIdentityPeers(t *testing.T) *Peers {
	t.Helper()
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(idPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed corrupt identity: %v", err)
	}
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(idPath)
	store := peers.NewStore(filepath.Join(dir, "relay"))
	svc := &peers.Service{Identity: identity, Registry: reg, Store: store, Client: peers.NewClient()}
	return &Peers{Identity: identity, Registry: reg, Relay: svc}
}

func TestIdentityShow_GetError500(t *testing.T) {
	t.Parallel()
	p := corruptIdentityPeers(t)
	rec := httptest.NewRecorder()
	p.IdentityShow(rec, httptest.NewRequest("GET", "/api/identity", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on corrupt identity, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "identity") {
		t.Errorf("expected identity error message, got %s", rec.Body.String())
	}
}

func TestIdentityShowDetailed_GetError500(t *testing.T) {
	t.Parallel()
	p := corruptIdentityPeers(t)
	rec := httptest.NewRecorder()
	p.IdentityShowDetailed(rec, httptest.NewRequest("GET", "/api/identity/me", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on corrupt identity, got %d", rec.Code)
	}
}

func TestIdentityUpdate_GetError500(t *testing.T) {
	t.Parallel()
	p := corruptIdentityPeers(t)
	// Valid body so we pass validation and reach Identity.Update -> Get() error.
	req := httptest.NewRequest("PUT", "/api/identity/me", strings.NewReader(`{"displayName":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.IdentityUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on corrupt identity during update, got %d: %s", rec.Code, rec.Body.String())
	}
}
