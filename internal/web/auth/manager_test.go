package auth

import (
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// tlsConnState is a non-nil ConnectionState used to mark a test request as
// having arrived over TLS (isRequestSecure checks r.TLS != nil).
var tlsConnState = tls.ConnectionState{HandshakeComplete: true}

// newTestManager builds a Manager over a fresh temp config dir, failing the
// test on any construction error. It reuses the temp-dir pattern from
// mobile_token_test.go.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	return m
}

// --- Construction & secret loading -----------------------------------------

func TestNewManager_GeneratesAndPersistsSecret(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, true)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	key := m.SigningKey()
	if len(key) != secretKeyBytes {
		t.Fatalf("signing key length = %d, want %d", len(key), secretKeyBytes)
	}

	// The secret must be persisted, hex-encoded, with 0600 perms.
	secretPath := filepath.Join(dir, "auth-secret.key")
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatalf("secret not persisted: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("secret perm = %o, want 0600", perm)
	}
	data, _ := os.ReadFile(secretPath)
	decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("persisted secret not valid hex: %v", err)
	}
	if len(decoded) != secretKeyBytes {
		t.Errorf("persisted secret decodes to %d bytes, want %d", len(decoded), secretKeyBytes)
	}
}

func TestNewManager_LoadsExistingSecret(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	key1 := m1.SigningKey()

	// Second manager over the same dir must reuse the same secret.
	m2, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager (reload): %v", err)
	}
	key2 := m2.SigningKey()

	if string(key1) != string(key2) {
		t.Error("second manager did not load the persisted secret")
	}
}

func TestNewManager_RegeneratesOnCorruptSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// A secret file that is valid hex but the wrong length must be discarded
	// and a fresh full-length secret generated.
	badPath := filepath.Join(dir, "auth-secret.key")
	if err := os.WriteFile(badPath, []byte(hex.EncodeToString([]byte("tooshort"))), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if len(m.SigningKey()) != secretKeyBytes {
		t.Fatalf("expected regenerated %d-byte secret, got %d", secretKeyBytes, len(m.SigningKey()))
	}
}

func TestNewManager_RegeneratesOnNonHexSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth-secret.key"), []byte("not-hex-zzzz"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if len(m.SigningKey()) != secretKeyBytes {
		t.Fatalf("expected regenerated %d-byte secret, got %d", secretKeyBytes, len(m.SigningKey()))
	}
}

func TestNewManager_FailsWhenConfigDirUnwritable(t *testing.T) {
	// Point configDir at a path under a regular file so MkdirAll fails.
	parent := t.TempDir()
	fileBlocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(fileBlocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(fileBlocker, "subdir")
	if _, err := NewManager(badDir, false); err == nil {
		t.Fatal("expected NewManager to fail when config dir cannot be created")
	}
}

// --- HookSigningKey ---------------------------------------------------------

func TestHookSigningKey_DerivedAndDomainSeparated(t *testing.T) {
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", "")
	m := newTestManager(t)

	hookKey := m.HookSigningKey()
	if len(hookKey) == 0 {
		t.Fatal("HookSigningKey returned empty")
	}
	// Derived key must differ from the cookie-signing key (domain separation).
	if string(hookKey) == string(m.SigningKey()) {
		t.Error("hook key equals session key -- domain separation broken")
	}
	// Deterministic: a second call returns the same value.
	if string(hookKey) != string(m.HookSigningKey()) {
		t.Error("HookSigningKey is not deterministic")
	}
}

func TestHookSigningKey_EnvOverride(t *testing.T) {
	raw := strings.Repeat("ab", 16) // 16 bytes hex -> >= 16 byte minimum
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", raw)
	m := newTestManager(t)

	got := m.HookSigningKey()
	want, _ := hex.DecodeString(raw)
	if string(got) != string(want) {
		t.Errorf("env override not honored: got %x want %x", got, want)
	}
}

func TestHookSigningKey_EnvIgnoredWhenTooShort(t *testing.T) {
	// < 16 decoded bytes should be ignored and fall back to the derived key.
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", "abcd") // 2 bytes
	m := newTestManager(t)

	got := m.HookSigningKey()
	if string(got) == "abcd" {
		t.Error("short env key should have been ignored")
	}
	// Should equal the derived key (deterministic from session secret).
	if len(got) == 0 {
		t.Error("expected derived key fallback, got empty")
	}
}

func TestHookSigningKey_EnvIgnoredWhenNonHex(t *testing.T) {
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", "zzzznothex")
	m := newTestManager(t)
	if len(m.HookSigningKey()) == 0 {
		t.Error("non-hex env key should fall back to derived key, not empty")
	}
}

func TestHookSigningKey_NilSecretReturnsNil(t *testing.T) {
	t.Setenv("JARVIS_HOOK_SIGNING_KEY", "")
	m := &Manager{} // no signing secret loaded
	if got := m.HookSigningKey(); got != nil {
		t.Errorf("expected nil hook key with empty secret, got %x", got)
	}
}

// --- Password lifecycle -----------------------------------------------------

func TestValidatePasswordInput(t *testing.T) {
	m := newTestManager(t)
	cases := []struct {
		name     string
		pw       string
		wantErr  bool
		contains string
	}{
		{"too short", "1234567", true, "at least 8"},
		{"exactly 8", "12345678", false, ""},
		{"max length", strings.Repeat("a", maxPasswordLength), false, ""},
		{"over max", strings.Repeat("a", maxPasswordLength+1), true, "at most"},
		{"empty", "", true, "at least 8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := m.ValidatePasswordInput(c.pw)
			if c.wantErr && got == "" {
				t.Fatalf("expected error for %q, got none", c.pw)
			}
			if !c.wantErr && got != "" {
				t.Fatalf("expected no error for %q, got %q", c.pw, got)
			}
			if c.contains != "" && !strings.Contains(got, c.contains) {
				t.Errorf("error %q does not contain %q", got, c.contains)
			}
		})
	}
}

func TestSetAndVerifyPassword(t *testing.T) {
	m := newTestManager(t)

	if m.IsPasswordConfigured() {
		t.Fatal("password should not be configured initially")
	}

	if err := m.SetPassword("correct horse battery"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	if !m.IsPasswordConfigured() {
		t.Error("IsPasswordConfigured should be true after SetPassword")
	}

	ok, err := m.VerifyPassword("correct horse battery")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword returned false for correct password")
	}

	ok, err = m.VerifyPassword("wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword (wrong): %v", err)
	}
	if ok {
		t.Error("VerifyPassword returned true for wrong password")
	}

	// The stored hash must be a bcrypt hash, not the plaintext.
	data, _ := os.ReadFile(filepath.Join(m.configDir, "auth.json"))
	var cfg authConfig
	json.Unmarshal(data, &cfg)
	if cfg.PasswordHash == "" {
		t.Error("auth.json has empty passwordHash")
	}
	if strings.Contains(cfg.PasswordHash, "correct horse battery") {
		t.Error("plaintext password leaked into auth.json")
	}
}

func TestVerifyPassword_NoConfigFile(t *testing.T) {
	m := newTestManager(t)
	// No auth.json yet: VerifyPassword should return (false, nil) not an error.
	ok, err := m.VerifyPassword("anything")
	if err != nil {
		t.Errorf("expected nil error when no config file, got %v", err)
	}
	if ok {
		t.Error("expected false when no password configured")
	}
}

func TestVerifyPassword_CorruptConfig(t *testing.T) {
	m := newTestManager(t)
	if err := os.WriteFile(m.authConfigPath, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	ok, err := m.VerifyPassword("anything")
	if err == nil {
		t.Error("expected error parsing corrupt auth config")
	}
	if ok {
		t.Error("expected false on corrupt config")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	m := newTestManager(t)
	// Valid JSON but the hash is not a bcrypt hash -> bcrypt.CompareHashAndPassword
	// returns a non-mismatch error, which must surface as an error.
	cfg := authConfig{PasswordHash: "not-a-bcrypt-hash"}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(m.authConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	ok, err := m.VerifyPassword("anything")
	if err == nil {
		t.Error("expected error for malformed bcrypt hash")
	}
	if ok {
		t.Error("expected false for malformed bcrypt hash")
	}
}

func TestIsPasswordConfigured_CachesNegativeAndPositive(t *testing.T) {
	m := newTestManager(t)
	// First call: no file -> false, cached as -1.
	if m.IsPasswordConfigured() {
		t.Fatal("expected false initially")
	}
	// Set a password; the cache is invalidated inside SetPassword.
	if err := m.SetPassword("password123"); err != nil {
		t.Fatal(err)
	}
	if !m.IsPasswordConfigured() {
		t.Fatal("expected true after SetPassword")
	}
	// Second call hits the positive cache (still true).
	if !m.IsPasswordConfigured() {
		t.Error("cached positive lookup returned false")
	}
}

func TestIsPasswordConfigured_EmptyHashConfig(t *testing.T) {
	m := newTestManager(t)
	// Valid JSON with an empty hash should be treated as not configured.
	data, _ := json.Marshal(authConfig{PasswordHash: ""})
	if err := os.WriteFile(m.authConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if m.IsPasswordConfigured() {
		t.Error("empty passwordHash should count as not configured")
	}
}

func TestIsPasswordConfigured_CorruptConfig(t *testing.T) {
	m := newTestManager(t)
	if err := os.WriteFile(m.authConfigPath, []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if m.IsPasswordConfigured() {
		t.Error("corrupt config should count as not configured")
	}
}

func TestChangePassword(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetPassword("oldpassword"); err != nil {
		t.Fatal(err)
	}
	// Seed a session so we can prove it gets invalidated.
	token := m.CreateSession()
	if !m.ValidateSession(token) {
		t.Fatal("seeded session should validate")
	}

	// Wrong current password -> (false, nil).
	ok, err := m.ChangePassword("incorrect", "newpassword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ChangePassword should fail with wrong current password")
	}

	// Correct current password -> success + sessions cleared.
	ok, err = m.ChangePassword("oldpassword", "newpassword")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if !ok {
		t.Fatal("ChangePassword should succeed with correct current password")
	}
	if m.ValidateSession(token) {
		t.Error("old session should be invalidated after password change")
	}
	// New password now verifies; old one does not.
	if good, _ := m.VerifyPassword("newpassword"); !good {
		t.Error("new password does not verify after change")
	}
	if good, _ := m.VerifyPassword("oldpassword"); good {
		t.Error("old password still verifies after change")
	}
}

func TestChangePassword_VerifyErrorPropagates(t *testing.T) {
	m := newTestManager(t)
	// Corrupt config so VerifyPassword returns an error.
	if err := os.WriteFile(m.authConfigPath, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	ok, err := m.ChangePassword("whatever", "newpassword")
	if err == nil {
		t.Error("expected error to propagate from VerifyPassword")
	}
	if ok {
		t.Error("expected false on verify error")
	}
}

func TestSetPassword_RenameFailure(t *testing.T) {
	m := newTestManager(t)
	// Make authConfigPath a directory so os.Rename(tmp, authConfigPath) fails
	// (cannot rename a file over a non-empty directory). This exercises the
	// rename-error branch and the tmp-file cleanup.
	if err := os.MkdirAll(m.authConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	// Put something inside so the directory is non-empty and rename is rejected.
	if err := os.WriteFile(filepath.Join(m.authConfigPath, "child"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPassword("password123"); err == nil {
		t.Error("expected SetPassword to fail when target is a non-empty dir")
	}
	// The temp file should have been cleaned up on the rename failure.
	if _, err := os.Stat(m.authConfigPath + ".tmp"); err == nil {
		t.Error("temp file should be removed after rename failure")
	}
}

func TestChangePassword_SetPasswordFailurePropagates(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetPassword("oldpassword"); err != nil {
		t.Fatal(err)
	}
	// Replace the auth config path target with a directory AFTER the initial
	// password is set, so VerifyPassword still reads the old hash but the
	// subsequent SetPassword write fails. We do this by reading the current
	// file, turning the path into a directory is not possible while the file
	// exists, so instead make the .tmp path undeletable by making the parent
	// read-only is platform-fragile -- instead point at an unwritable temp path.
	//
	// Simpler: corrupt by making configDir read-only so the tmp write fails.
	if err := os.Chmod(m.configDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(m.configDir, 0700) })

	ok, err := m.ChangePassword("oldpassword", "newpassword")
	if err == nil {
		t.Error("expected ChangePassword to propagate SetPassword write error")
	}
	if ok {
		t.Error("expected false when SetPassword fails")
	}
}

// --- Setup lock -------------------------------------------------------------

func TestSetupLock(t *testing.T) {
	m := newTestManager(t)
	if !m.AcquireSetupLock() {
		t.Fatal("first AcquireSetupLock should succeed")
	}
	if m.AcquireSetupLock() {
		t.Error("second AcquireSetupLock should fail while held")
	}
	m.ReleaseSetupLock()
	if !m.AcquireSetupLock() {
		t.Error("AcquireSetupLock should succeed after release")
	}
}

// --- Sessions ---------------------------------------------------------------

func TestCreateValidateDestroySession(t *testing.T) {
	m := newTestManager(t)

	token := m.CreateSession()
	if token == "" {
		t.Fatal("CreateSession returned empty token")
	}
	if len(token) != sessionTokenBytes*2 {
		t.Errorf("session token length = %d, want %d", len(token), sessionTokenBytes*2)
	}

	if !m.ValidateSession(token) {
		t.Error("freshly created session should validate")
	}
	if m.ValidateSession("nonexistent-token") {
		t.Error("unknown token should not validate")
	}

	m.DestroySession(token)
	if m.ValidateSession(token) {
		t.Error("destroyed session should not validate")
	}
}

func TestValidateSession_ExpiredIsEvicted(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()

	// Force the session's LastActivity into the past so it is expired.
	m.mu.Lock()
	m.sessions[token].SetLastActivity(time.Now().Add(-2 * sessionTimeoutDur))
	m.mu.Unlock()

	if m.ValidateSession(token) {
		t.Error("expired session should not validate")
	}
	// It should have been deleted by ValidateSession.
	m.mu.RLock()
	_, stillThere := m.sessions[token]
	m.mu.RUnlock()
	if stillThere {
		t.Error("expired session should be evicted by ValidateSession")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	m := newTestManager(t)
	live := m.CreateSession()
	dead := m.CreateSession()

	m.mu.Lock()
	m.sessions[dead].SetLastActivity(time.Now().Add(-2 * sessionTimeoutDur))
	m.mu.Unlock()

	m.cleanupExpiredSessions()

	m.mu.RLock()
	_, liveOK := m.sessions[live]
	_, deadOK := m.sessions[dead]
	m.mu.RUnlock()

	if !liveOK {
		t.Error("live session should survive cleanup")
	}
	if deadOK {
		t.Error("dead session should be removed by cleanup")
	}
}

// --- Cookies ----------------------------------------------------------------

func TestBuildAndParseSessionCookie_RoundTrip(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()

	// Insecure (plain HTTP) request: no Secure flag.
	httpReq := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	cookie := m.BuildSessionCookie(token, httpReq)
	if cookie.Name != cookieName {
		t.Errorf("cookie name = %q, want %q", cookie.Name, cookieName)
	}
	if !cookie.HttpOnly {
		t.Error("session cookie should be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Error("session cookie should be SameSite=Strict")
	}
	if cookie.Secure {
		t.Error("Secure should be false on a plain HTTP request")
	}
	if !strings.Contains(cookie.Value, ".") {
		t.Error("cookie value should be token.signature")
	}

	// Parse it back via a request carrying the cookie.
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.AddCookie(cookie)
	parsed := m.ParseSessionCookie(req)
	if parsed != token {
		t.Errorf("ParseSessionCookie = %q, want %q", parsed, token)
	}

	// And the full request-level validation path.
	if !m.ValidateSessionFromRequest(req) {
		t.Error("ValidateSessionFromRequest should accept a valid signed cookie")
	}
}

func TestBuildSessionCookie_SecureOnTLS(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()
	req := httptest.NewRequest(http.MethodGet, "https://example/", nil)
	req.TLS = &tlsConnState
	cookie := m.BuildSessionCookie(token, req)
	if !cookie.Secure {
		t.Error("Secure should be true on a TLS request")
	}
}

func TestBuildSessionCookie_SecureOnForwardedProto(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	cookie := m.BuildSessionCookie(token, req)
	if !cookie.Secure {
		t.Error("Secure should be true when X-Forwarded-Proto=https")
	}
}

func TestBuildClearCookie(t *testing.T) {
	m := newTestManager(t)
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	cookie := m.BuildClearCookie(req)
	if cookie.Value != "" {
		t.Errorf("clear cookie value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Errorf("clear cookie MaxAge = %d, want -1", cookie.MaxAge)
	}
	if !cookie.HttpOnly {
		t.Error("clear cookie should be HttpOnly")
	}

	// On TLS, the clear cookie is also Secure.
	tlsReq := httptest.NewRequest(http.MethodGet, "https://example/", nil)
	tlsReq.TLS = &tlsConnState
	if !m.BuildClearCookie(tlsReq).Secure {
		t.Error("clear cookie should be Secure on TLS")
	}
}

func TestParseSessionCookie_NoCookie(t *testing.T) {
	m := newTestManager(t)
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	if got := m.ParseSessionCookie(req); got != "" {
		t.Errorf("expected empty token when no cookie present, got %q", got)
	}
	if m.ValidateSessionFromRequest(req) {
		t.Error("ValidateSessionFromRequest should be false with no cookie")
	}
}

func TestParseSessionCookie_NoDotSeparator(t *testing.T) {
	m := newTestManager(t)
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "nodotseparator"})
	if got := m.ParseSessionCookie(req); got != "" {
		t.Errorf("expected empty token for value with no dot, got %q", got)
	}
}

func TestParseSessionCookie_MalformedSignatureHex(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	// Signature part is not valid hex.
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token + ".zzzznothex"})
	if got := m.ParseSessionCookie(req); got != "" {
		t.Errorf("expected empty token for non-hex signature, got %q", got)
	}
}

func TestParseSessionCookie_WrongSignature(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()
	// A valid-hex but wrong-length signature triggers the length-mismatch path.
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token + ".abcd"})
	if got := m.ParseSessionCookie(req); got != "" {
		t.Errorf("expected empty token for short signature, got %q", got)
	}

	// A correct-length but wrong-value signature triggers the constant-time
	// comparison failure path.
	wrongSig := strings.Repeat("00", 32) // sha256 is 32 bytes -> 64 hex
	req2 := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: token + "." + wrongSig})
	if got := m.ParseSessionCookie(req2); got != "" {
		t.Errorf("expected empty token for wrong signature, got %q", got)
	}
}

func TestParseSessionCookie_TamperedToken(t *testing.T) {
	m := newTestManager(t)
	token := m.CreateSession()
	httpReq := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	cookie := m.BuildSessionCookie(token, httpReq)

	// Flip the token but keep the original signature -> must be rejected.
	dotIdx := strings.LastIndex(cookie.Value, ".")
	tampered := "deadbeef" + cookie.Value[8:dotIdx] + cookie.Value[dotIdx:]
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tampered})
	if got := m.ParseSessionCookie(req); got != "" {
		t.Error("tampered token with stale signature should be rejected")
	}
}

// --- isRequestSecure --------------------------------------------------------

func TestIsRequestSecure(t *testing.T) {
	if isRequestSecure(nil) {
		t.Error("nil request should not be secure")
	}
	plain := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	if isRequestSecure(plain) {
		t.Error("plain http request should not be secure")
	}
	tls := httptest.NewRequest(http.MethodGet, "https://example/", nil)
	tls.TLS = &tlsConnState
	if !isRequestSecure(tls) {
		t.Error("TLS request should be secure")
	}
	fwd := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	fwd.Header.Set("X-Forwarded-Proto", "https")
	if !isRequestSecure(fwd) {
		t.Error("X-Forwarded-Proto=https should be secure")
	}
	fwdHTTP := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	fwdHTTP.Header.Set("X-Forwarded-Proto", "http")
	if isRequestSecure(fwdHTTP) {
		t.Error("X-Forwarded-Proto=http should not be secure")
	}
}

// --- Rate limiting ----------------------------------------------------------

func TestRateLimit_Tier1Escalation(t *testing.T) {
	m := newTestManager(t)
	const ip = "10.0.0.1"

	if blocked, _ := m.IsRateLimited(ip); blocked {
		t.Fatal("IP should not be limited initially")
	}

	// First (tier1-1) attempts do not block.
	for i := 0; i < rateLimitTier1Attempts-1; i++ {
		if blocked, _ := m.RecordFailedAttempt(ip); blocked {
			t.Fatalf("attempt %d should not block yet", i+1)
		}
	}
	// The attempt that reaches tier1 threshold blocks.
	blocked, retryMs := m.RecordFailedAttempt(ip)
	if !blocked {
		t.Fatal("reaching tier1 threshold should block")
	}
	if retryMs <= 0 {
		t.Errorf("retryAfterMs should be positive, got %d", retryMs)
	}

	// IsRateLimited should now report blocked with a positive retry.
	blocked, retry := m.IsRateLimited(ip)
	if !blocked {
		t.Error("IsRateLimited should report blocked")
	}
	if retry <= 0 {
		t.Errorf("IsRateLimited retry = %d, want > 0", retry)
	}
}

func TestRateLimit_Tier2Escalation(t *testing.T) {
	m := newTestManager(t)
	const ip = "10.0.0.2"

	var lastRetry int
	for i := 0; i < rateLimitTier2Attempts; i++ {
		_, lastRetry = m.RecordFailedAttempt(ip)
	}
	// At tier2, the block duration should reflect the longer tier2 window.
	if lastRetry < int(rateLimitTier1Block.Milliseconds()) {
		t.Errorf("tier2 retry %d should exceed tier1 block %d", lastRetry, rateLimitTier1Block.Milliseconds())
	}
	wantMin := int(rateLimitTier2Block.Milliseconds()) - 5000
	if lastRetry < wantMin {
		t.Errorf("tier2 retry %d below expected tier2 window ~%d", lastRetry, rateLimitTier2Block.Milliseconds())
	}
}

func TestResetRateLimit(t *testing.T) {
	m := newTestManager(t)
	const ip = "10.0.0.3"
	for i := 0; i < rateLimitTier1Attempts; i++ {
		m.RecordFailedAttempt(ip)
	}
	if blocked, _ := m.IsRateLimited(ip); !blocked {
		t.Fatal("IP should be blocked before reset")
	}
	m.ResetRateLimit(ip)
	if blocked, _ := m.IsRateLimited(ip); blocked {
		t.Error("IP should not be blocked after reset")
	}
}

func TestIsRateLimited_BlockExpired(t *testing.T) {
	m := newTestManager(t)
	const ip = "10.0.0.4"
	for i := 0; i < rateLimitTier1Attempts; i++ {
		m.RecordFailedAttempt(ip)
	}
	// Force the block into the past.
	m.mu.Lock()
	m.rateLimits[ip].BlockedUntil = time.Now().Add(-time.Minute)
	m.mu.Unlock()

	blocked, retry := m.IsRateLimited(ip)
	if blocked {
		t.Error("expired block should not report limited")
	}
	if retry != 0 {
		t.Errorf("retry should be 0 when block expired, got %d", retry)
	}
}

func TestCleanupExpiredRateLimits(t *testing.T) {
	m := newTestManager(t)

	// Entry A: quiet long enough + block expired -> should be deleted.
	// Entry B: recent attempt -> should be kept (anti-cycling protection).
	m.mu.Lock()
	m.rateLimits["quiet"] = &RateLimitEntry{
		FailedAttempts: 3,
		BlockedUntil:   time.Now().Add(-time.Hour),
		LastAttempt:    time.Now().Add(-2 * rateLimitQuietWindow),
	}
	m.rateLimits["recent"] = &RateLimitEntry{
		FailedAttempts: 3,
		BlockedUntil:   time.Time{},
		LastAttempt:    time.Now(),
	}
	m.rateLimits["blocked"] = &RateLimitEntry{
		FailedAttempts: 6,
		BlockedUntil:   time.Now().Add(time.Hour), // still blocked
		LastAttempt:    time.Now().Add(-2 * rateLimitQuietWindow),
	}
	m.mu.Unlock()

	m.CleanupExpiredRateLimits()

	m.mu.RLock()
	_, quietThere := m.rateLimits["quiet"]
	_, recentThere := m.rateLimits["recent"]
	_, blockedThere := m.rateLimits["blocked"]
	m.mu.RUnlock()

	if quietThere {
		t.Error("quiet, block-expired entry should be cleaned up")
	}
	if !recentThere {
		t.Error("recently-active entry must be retained to prevent tier-cycling")
	}
	if !blockedThere {
		t.Error("still-blocked entry must be retained even if quiet")
	}
}

func TestRateLimit_Concurrent(t *testing.T) {
	m := newTestManager(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := "192.168.0." + string(rune('0'+n%10))
			m.RecordFailedAttempt(ip)
			m.IsRateLimited(ip)
			m.ResetRateLimit(ip)
		}(i)
	}
	wg.Wait()
}

// --- Trusted proxy / ClientIP ----------------------------------------------

func TestClientIP_NoTrustedProxy(t *testing.T) {
	// Reset to a known empty state so spoofed headers are ignored.
	SetTrustedProxyCIDRs(nil)
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")

	if got := ClientIP(req); got != "203.0.113.5" {
		t.Errorf("ClientIP = %q, want 203.0.113.5 (headers must be ignored without trusted proxy)", got)
	}
}

func TestClientIP_TrustedProxyHonorsXFF(t *testing.T) {
	SetTrustedProxyCIDRs([]string{"203.0.113.0/24"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9")

	if got := ClientIP(req); got != "1.2.3.4" {
		t.Errorf("ClientIP = %q, want first XFF entry 1.2.3.4", got)
	}
}

func TestClientIP_TrustedProxySingleXFF(t *testing.T) {
	SetTrustedProxyCIDRs([]string{"203.0.113.0/24"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(req); got != "1.2.3.4" {
		t.Errorf("ClientIP = %q, want 1.2.3.4", got)
	}
}

func TestClientIP_TrustedProxyHonorsXRealIP(t *testing.T) {
	SetTrustedProxyCIDRs([]string{"203.0.113.0/24"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	// No XFF, only X-Real-IP.
	req.Header.Set("X-Real-IP", "5.6.7.8")
	if got := ClientIP(req); got != "5.6.7.8" {
		t.Errorf("ClientIP = %q, want X-Real-IP 5.6.7.8", got)
	}
}

func TestClientIP_TrustedProxyNoHeaders(t *testing.T) {
	SetTrustedProxyCIDRs([]string{"203.0.113.0/24"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	// Trusted proxy but no forwarding headers -> fall back to remote addr.
	if got := ClientIP(req); got != "203.0.113.5" {
		t.Errorf("ClientIP = %q, want 203.0.113.5", got)
	}
}

func TestClientIP_IPv6RemoteAddr(t *testing.T) {
	SetTrustedProxyCIDRs(nil)
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "[::1]:5555"
	if got := ClientIP(req); got != "::1" {
		t.Errorf("ClientIP = %q, want ::1 (brackets/port stripped)", got)
	}
}

func TestSetTrustedProxyCIDRs_IgnoresInvalid(t *testing.T) {
	// A mix of valid and invalid CIDRs: only the valid one should take effect.
	SetTrustedProxyCIDRs([]string{"not-a-cidr", "10.0.0.0/8"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })

	inRange := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	inRange.RemoteAddr = "10.1.2.3:5555"
	inRange.Header.Set("X-Forwarded-For", "8.8.8.8")
	if got := ClientIP(inRange); got != "8.8.8.8" {
		t.Errorf("valid CIDR should trust XFF: got %q want 8.8.8.8", got)
	}

	outOfRange := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	outOfRange.RemoteAddr = "172.16.0.1:5555"
	outOfRange.Header.Set("X-Forwarded-For", "8.8.8.8")
	if got := ClientIP(outOfRange); got != "172.16.0.1" {
		t.Errorf("out-of-range source should ignore XFF: got %q want 172.16.0.1", got)
	}
}

func TestRemoteAddrIP_NoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "198.51.100.7" // no port -> SplitHostPort fails, returns as-is
	if got := remoteAddrIP(req); got != "198.51.100.7" {
		t.Errorf("remoteAddrIP = %q, want 198.51.100.7", got)
	}
}

func TestRequestFromTrustedProxy_UnparseableRemoteAddr(t *testing.T) {
	SetTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	t.Cleanup(func() { SetTrustedProxyCIDRs(nil) })
	req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	req.RemoteAddr = "garbage-not-an-ip"
	if requestFromTrustedProxy(req) {
		t.Error("unparseable remote addr must not be treated as trusted")
	}
}

// --- signToken determinism --------------------------------------------------

func TestSignToken_Deterministic(t *testing.T) {
	m := newTestManager(t)
	a := m.signToken("hello")
	b := m.signToken("hello")
	if a != b {
		t.Error("signToken should be deterministic for the same input")
	}
	if a == m.signToken("world") {
		t.Error("signToken should differ for different inputs")
	}
	// Output is hex of a SHA-256 MAC -> 64 chars.
	if len(a) != 64 {
		t.Errorf("signToken length = %d, want 64", len(a))
	}
}
