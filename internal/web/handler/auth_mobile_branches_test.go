package handler

// Branch fills for the auth-mobile handlers: nil-manager 503 guards and the
// X-Jarvis-Agent-Token path of bearerToken.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMobileLogout_NilManager503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("POST", "/api/auth/mobile/logout", nil)
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogout(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil manager, got %d", rec.Code)
	}
}

func TestAuthMobileRevokeToken_NilManager503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/auth/mobile/tokens/d1", nil), "deviceId", "d1")
	rec := httptest.NewRecorder()
	ctx.AuthMobileRevokeToken(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil manager, got %d", rec.Code)
	}
}

func TestBearerToken_AgentHeaderAndBearerAndEmpty(t *testing.T) {
	t.Parallel()
	// X-Jarvis-Agent-Token takes precedence.
	reqAgent := httptest.NewRequest("GET", "/", nil)
	reqAgent.Header.Set("X-Jarvis-Agent-Token", "agent-123")
	if got := bearerToken(reqAgent); got != "agent-123" {
		t.Errorf("agent header: got %q, want agent-123", got)
	}

	// Authorization: Bearer fallback.
	reqBearer := httptest.NewRequest("GET", "/", nil)
	reqBearer.Header.Set("Authorization", "Bearer tok-xyz")
	if got := bearerToken(reqBearer); got != "tok-xyz" {
		t.Errorf("bearer header: got %q, want tok-xyz", got)
	}

	// Neither -> empty.
	if got := bearerToken(httptest.NewRequest("GET", "/", nil)); got != "" {
		t.Errorf("no header: got %q, want empty", got)
	}
}
