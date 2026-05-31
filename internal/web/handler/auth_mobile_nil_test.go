package handler

// Coverage for the nil-MobileTokenMgr branch of AuthMobileListTokens.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMobileListTokens_NilManagerReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := &Context{} // MobileTokenMgr is nil
	req := httptest.NewRequest("GET", "/api/auth/mobile/tokens", nil)
	rec := httptest.NewRecorder()
	ctx.AuthMobileListTokens(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	tokens, _ := resp["tokens"].([]interface{})
	if len(tokens) != 0 {
		t.Errorf("expected empty tokens with nil manager, got %d", len(tokens))
	}
}
