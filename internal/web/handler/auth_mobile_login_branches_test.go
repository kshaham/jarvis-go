package handler

// Round-2 branch fills for AuthMobileLogin: nil-manager 503, malformed-JSON
// 400, device-name truncation, and the VerifyPassword IO/parse-error 500 arm.
// The latter is reached HONESTLY by corrupting the on-disk auth.json so
// json.Unmarshal inside VerifyPassword fails.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/middleware"
)

func TestAuthMobileLogin_NilManager503(t *testing.T) {
	t.Parallel()
	ctx := &Context{} // MobileTokenMgr is nil
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password": "whatever", "deviceName": "x",
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil MobileTokenMgr, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMobileLogin_MalformedJSON400(t *testing.T) {
	t.Parallel()
	ctx := newMobileAuthContext(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mobile/login", strings.NewReader("{not valid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON body, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthMobileLogin_LongDeviceNameTruncated drives the deviceName-too-long
// truncation arm. We give a deviceName longer than maxMobileDeviceNameLen and
// then assert the persisted device row carries the truncated name.
func TestAuthMobileLogin_LongDeviceNameTruncated(t *testing.T) {
	t.Parallel()
	ctx := newMobileAuthContext(t)
	longName := strings.Repeat("z", maxMobileDeviceNameLen+40)
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password":   "hunter22hunter22",
		"deviceName": longName,
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeJSON(t, rec)
	deviceID, _ := resp["deviceId"].(string)
	if deviceID == "" {
		t.Fatal("missing deviceId")
	}
	// The persisted entry should carry the truncated (<= max) device name.
	for _, row := range ctx.MobileTokenMgr.List() {
		if row.DeviceID == deviceID {
			if len(row.DeviceName) != maxMobileDeviceNameLen {
				t.Errorf("device name len=%d, want %d (truncated)", len(row.DeviceName), maxMobileDeviceNameLen)
			}
			return
		}
	}
	t.Fatalf("issued device %q not found in token list", deviceID)
}

// TestAuthMobileLogin_VerifyPasswordError500 corrupts the auth.json on disk so
// VerifyPassword's json.Unmarshal fails, returning a non-nil error -> 500.
func TestAuthMobileLogin_VerifyPasswordError500(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.SetPassword("hunter22hunter22"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	// Corrupt the stored auth.json so VerifyPassword's json.Unmarshal errors.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("corrupt auth.json: %v", err)
	}
	ctx := &Context{
		AuthManager:    mgr,
		MobileTokenMgr: auth.NewMobileTokenManager(dir),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
	}
	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password":   "hunter22hunter22",
		"deviceName": "iPhone",
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when auth config is unparseable, got %d: %s", rec.Code, rec.Body.String())
	}
}
