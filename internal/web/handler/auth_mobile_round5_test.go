package handler

// Round 5 -- the last residual arm in AuthMobileLogin: a successful
// password verification followed by a token-issue failure. Issue only
// fails on crypto/rand exhaustion in production, so it is driven here
// through the issueMobileToken seam.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/web/auth"
)

func TestAuthMobileLogin_IssueError500(t *testing.T) {
	ctx := newMobileAuthContext(t)

	orig := issueMobileToken
	t.Cleanup(func() { issueMobileToken = orig })
	issueMobileToken = func(_ *auth.MobileTokenManager, _ string, _ time.Duration) (auth.IssuedMobileToken, error) {
		return auth.IssuedMobileToken{}, errors.New("rand exhausted")
	}

	req := mobilePostJSONReq("/api/auth/mobile/login", map[string]string{
		"password":   "hunter22hunter22",
		"deviceName": "iPhone",
	})
	rec := httptest.NewRecorder()
	ctx.AuthMobileLogin(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when token issue fails, got %d: %s", rec.Code, rec.Body.String())
	}
	// The rate limiter was reset just before issuing -- a correct password
	// must not leave the IP penalized even though the issue failed.
	blocked, _ := ctx.AuthManager.IsRateLimited("192.0.2.1")
	if blocked {
		t.Error("a correct password should not rate-limit the source IP")
	}
}
