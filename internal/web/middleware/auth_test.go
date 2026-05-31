package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/web/auth"
)

// newConfiguredManager returns a Manager whose password is set (so auth is
// enforced) backed by a fresh temp config dir.
func newConfiguredManager(t *testing.T) *auth.Manager {
	t.Helper()
	dir := t.TempDir()
	am, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := am.SetPassword("super-secret-password"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	return am
}

// newOpenManager returns a Manager with no password configured (auth disabled).
func newOpenManager(t *testing.T) *auth.Manager {
	t.Helper()
	dir := t.TempDir()
	am, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return am
}

// validSessionCookie creates a session and returns a signed cookie for it.
func validSessionCookie(t *testing.T, am *auth.Manager) *http.Cookie {
	t.Helper()
	token := am.CreateSession()
	if token == "" {
		t.Fatal("CreateSession returned empty token")
	}
	req := httptest.NewRequest("GET", "/", nil)
	return am.BuildSessionCookie(token, req)
}

// okNext is a trivial next handler that records whether it ran and writes 200.
func okNext(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestAuth_PassThroughWhenNoPassword(t *testing.T) {
	am := newOpenManager(t)
	called := false
	h := Auth(am)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected pass-through when no password configured")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAuth_ExemptPathsBypass(t *testing.T) {
	am := newConfiguredManager(t)
	exempt := []string{
		"/api/health",
		"/api/ready",
		"/api/auth/login",
		"/api/auth/status",
		"/api/auth",
		"/api/hook-event",
	}
	for _, path := range exempt {
		called := false
		h := Auth(am)(okNext(&called))
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !called {
			t.Errorf("path %s: expected exempt pass-through, handler not called", path)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("path %s: got %d, want 200", path, rec.Code)
		}
	}
}

func TestAuth_RejectsUnauthenticated(t *testing.T) {
	am := newConfiguredManager(t)
	called := false
	h := Auth(am)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run for unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got content-type %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["success"] != false {
		t.Errorf("expected success=false, got %v", body["success"])
	}
	if body["error"] != "authentication required" {
		t.Errorf("got error %q", body["error"])
	}
}

func TestAuth_AcceptsValidSessionCookie(t *testing.T) {
	am := newConfiguredManager(t)
	called := false
	h := Auth(am)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.AddCookie(validSessionCookie(t, am))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid session cookie")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAuth_RejectsTamperedCookieButAcceptsNothingElse(t *testing.T) {
	am := newConfiguredManager(t)
	called := false
	h := Auth(am)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	// A cookie with a bad signature must be rejected.
	req.AddCookie(&http.Cookie{Name: "jarvis_session", Value: "sometoken.deadbeef"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run for tampered cookie")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestAuthWithTokens_AcceptsAgentTokenViaXHeader(t *testing.T) {
	am := newConfiguredManager(t)
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AuthWithTokens(am, atm, nil)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Jarvis-Agent-Token", atm.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid agent token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAuthWithTokens_AcceptsAgentTokenViaBearer(t *testing.T) {
	am := newConfiguredManager(t)
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AuthWithTokens(am, atm, nil)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+atm.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid bearer agent token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAuthWithTokens_RejectsWrongAgentToken(t *testing.T) {
	am := newConfiguredManager(t)
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AuthWithTokens(am, atm, nil)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Jarvis-Agent-Token", "not-the-real-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run for wrong agent token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestAuthWithTokens_AcceptsMobileToken(t *testing.T) {
	am := newConfiguredManager(t)
	mtm := auth.NewMobileTokenManager(t.TempDir())
	issued, err := mtm.Issue("Test Phone", 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	called := false
	h := AuthWithTokens(am, nil, mtm)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid mobile token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAuthWithTokens_RejectsWhenBothManagersReject(t *testing.T) {
	am := newConfiguredManager(t)
	atm := auth.NewAgentTokenManager(t.TempDir())
	mtm := auth.NewMobileTokenManager(t.TempDir())
	called := false
	h := AuthWithTokens(am, atm, mtm)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer bogus-token-value")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run when neither token validates")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestAuthWithAgentToken_NilAgentManagerStillRejects(t *testing.T) {
	// AuthWithAgentToken with nil atm is cookie-only; a bearer token must
	// not slip through when there is no token manager to validate it.
	am := newConfiguredManager(t)
	called := false
	h := AuthWithAgentToken(am, nil)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Jarvis-Agent-Token", "anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run: nil atm cannot validate any token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestAgentTokenAuth_AcceptsValidViaXHeader(t *testing.T) {
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AgentTokenAuth(atm)(okNext(&called))

	req := httptest.NewRequest("POST", "/api/office/agent/claim", nil)
	req.Header.Set("X-Jarvis-Agent-Token", atm.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid agent token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAgentTokenAuth_AcceptsValidViaBearer(t *testing.T) {
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AgentTokenAuth(atm)(okNext(&called))

	req := httptest.NewRequest("POST", "/api/office/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+atm.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to run with valid bearer agent token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestAgentTokenAuth_RejectsMissingToken(t *testing.T) {
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AgentTokenAuth(atm)(okNext(&called))

	req := httptest.NewRequest("POST", "/api/office/agent/claim", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run without a token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got content-type %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "agent token required" {
		t.Errorf("got error %q", body["error"])
	}
}

func TestAgentTokenAuth_RejectsWrongToken(t *testing.T) {
	atm := auth.NewAgentTokenManager(t.TempDir())
	called := false
	h := AgentTokenAuth(atm)(okNext(&called))

	req := httptest.NewRequest("POST", "/api/office/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run for wrong token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestBearerOrAgentHeader_Extraction(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"x-header wins", map[string]string{"X-Jarvis-Agent-Token": "xtok", "Authorization": "Bearer btok"}, "xtok"},
		{"bearer only", map[string]string{"Authorization": "Bearer btok"}, "btok"},
		{"x only", map[string]string{"X-Jarvis-Agent-Token": "xtok"}, "xtok"},
		{"none", map[string]string{}, ""},
		{"non-bearer authorization", map[string]string{"Authorization": "Basic abc"}, ""},
		{"empty x falls back to bearer", map[string]string{"Authorization": "Bearer btok"}, "btok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			got := bearerOrAgentHeader(req)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsAuthExempt(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/health", true},
		{"/api/ready", true},
		{"/api/auth/login", true},
		{"/api/auth/logout", true},
		{"/api/auth", true},
		{"/api/hook-event", true},
		{"/api/status", false},
		{"/api/config", false},
		{"/api/office/agent/claim", false},
		{"/api/authentication", false}, // not under /api/auth/ and not exactly /api/auth
		{"/", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isAuthExempt(tc.path); got != tc.want {
			t.Errorf("isAuthExempt(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
