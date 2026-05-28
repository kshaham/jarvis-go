package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/middleware"
)

// newMobileAuthContext spins up an isolated Context with a password
// already configured ("hunter22hunter22") so login tests have something
// to verify against. AuthManager + MobileTokenManager use the same
// tempdir for both their on-disk state.
func newMobileAuthContext(t *testing.T) *Context {
	t.Helper()
	dir := t.TempDir()

	mgr, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.SetPassword("hunter22hunter22"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	return &Context{
		AuthManager:    mgr,
		MobileTokenMgr: auth.NewMobileTokenManager(dir),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, rec.Body.String())
	}
	return resp
}

func mobilePostJSONReq(path string, body interface{}) *http.Request {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAuthMobileLogin_HappyPath(t *testing.T) {
	ctx := newMobileAuthContext(t)

	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password":   "hunter22hunter22",
		"deviceName": "My iPhone",
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeJSON(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatal("token missing from response")
	}
	if deviceID, _ := resp["deviceId"].(string); deviceID == "" {
		t.Error("deviceId missing from response")
	}
	if _, ok := resp["expiresAt"].(float64); !ok {
		t.Error("expiresAt missing or wrong type")
	}

	// And the token actually works.
	if _, ok := ctx.MobileTokenMgr.Validate(token); !ok {
		t.Error("issued token does not Validate")
	}
}

func TestAuthMobileLogin_WrongPassword(t *testing.T) {
	ctx := newMobileAuthContext(t)
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password":   "wrong-password",
		"deviceName": "iPhone",
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeJSON(t, rec)
	if resp["success"] != false {
		t.Errorf("success=%v, want false", resp["success"])
	}
	if _, has := resp["token"]; has {
		t.Error("response leaked a token field on failed login")
	}
}

func TestAuthMobileLogin_BlankPassword(t *testing.T) {
	ctx := newMobileAuthContext(t)
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{"deviceName": "x"})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank password, got %d", rec.Code)
	}
}

func TestAuthMobileLogin_TooLongPassword(t *testing.T) {
	ctx := newMobileAuthContext(t)
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{"password": string(long)})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >128 char password, got %d", rec.Code)
	}
}

func TestAuthMobileLogout_RevokesPresentedToken(t *testing.T) {
	ctx := newMobileAuthContext(t)
	issued, err := ctx.MobileTokenMgr.Issue("Device", 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/mobile/logout", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeJSON(t, rec)
	if resp["revoked"] != true {
		t.Errorf("revoked=%v, want true", resp["revoked"])
	}
	if _, ok := ctx.MobileTokenMgr.Validate(issued.Token); ok {
		t.Error("token still validates after logout")
	}
}

func TestAuthMobileLogout_MissingBearer(t *testing.T) {
	ctx := newMobileAuthContext(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mobile/logout", nil)
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogout(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAuthMobileListTokens_OmitsSecrets(t *testing.T) {
	ctx := newMobileAuthContext(t)
	ctx.MobileTokenMgr.Issue("Alpha", 0)
	ctx.MobileTokenMgr.Issue("Beta", 0)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/mobile/tokens", nil)
	rec := httptest.NewRecorder()
	ctx.AuthMobileListTokens(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeJSON(t, rec)
	tokens, ok := resp["tokens"].([]interface{})
	if !ok {
		t.Fatalf("tokens missing or wrong type: %T", resp["tokens"])
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
	for i, raw := range tokens {
		row, _ := raw.(map[string]interface{})
		if _, leaked := row["token"]; leaked {
			t.Errorf("entry %d leaked token field", i)
		}
		if _, leaked := row["tokenHash"]; leaked {
			t.Errorf("entry %d leaked tokenHash field", i)
		}
		if name, _ := row["deviceName"].(string); name == "" {
			t.Errorf("entry %d missing deviceName", i)
		}
	}
}

func TestAuthMobileRevokeToken_ByDeviceID(t *testing.T) {
	ctx := newMobileAuthContext(t)
	issued, _ := ctx.MobileTokenMgr.Issue("To Revoke", 0)

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/mobile/tokens/"+issued.DeviceID, nil)
	// chi normally injects URL params via context -- mirror that.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("deviceId", issued.DeviceID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	ctx.AuthMobileRevokeToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeJSON(t, rec)
	if resp["revoked"] != true {
		t.Errorf("revoked=%v, want true", resp["revoked"])
	}
	if _, ok := ctx.MobileTokenMgr.Validate(issued.Token); ok {
		t.Error("token still validates after explicit revoke")
	}
}

func TestAuthMobileRevokeToken_UnknownDeviceID(t *testing.T) {
	ctx := newMobileAuthContext(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/mobile/tokens/does-not-exist", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("deviceId", "does-not-exist")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	ctx.AuthMobileRevokeToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (idempotent), got %d", rec.Code)
	}
	resp := decodeJSON(t, rec)
	if resp["revoked"] != false {
		t.Errorf("revoked=%v, want false for unknown deviceId", resp["revoked"])
	}
}

func TestAuthMobileRevokeToken_BlankDeviceID(t *testing.T) {
	ctx := newMobileAuthContext(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/mobile/tokens/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("deviceId", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	ctx.AuthMobileRevokeToken(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank deviceId, got %d", rec.Code)
	}
}

func TestAuthMobileLogin_RateLimited(t *testing.T) {
	ctx := newMobileAuthContext(t)
	// Six failed attempts trigger tier-1 (>= 5).
	for i := 0; i < 6; i++ {
		req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{"password": "wrong-password"})
		req.RemoteAddr = "127.0.0.1:55555"
		rec := httptest.NewRecorder()
		ctx.AuthMobileLogin(rec, req)
	}

	// Next attempt should be blocked even with the correct password.
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{"password": "hunter22hunter22"})
	req.RemoteAddr = "127.0.0.1:55555"
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit, got %d body=%s", rec.Code, rec.Body.String())
	}
}
