package handler

// Branch fills for AuthLogin: the >128-char password rejection, the
// RecordFailedAttempt-triggers-rate-limit path (after enough wrong guesses),
// and the already-blocked IsRateLimited short-circuit on a subsequent request.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuthLogin_PasswordTooLong(t *testing.T) {
	ctx := newAuthCtx(t)
	if err := ctx.AuthManager.SetPassword("correcthorse"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	long := strings.Repeat("a", 129)
	rec := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": long})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >128-char password, got %d", rec.Code)
	}
}

func TestAuthLogin_RateLimitedAfterRepeatedFailures(t *testing.T) {
	ctx := newAuthCtx(t)
	if err := ctx.AuthManager.SetPassword("correcthorse"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	// Tier-1 rate limit trips at 5 failed attempts. Submit wrong passwords
	// until we observe a 429 -- this drives RecordFailedAttempt's
	// rate-limited return branch.
	saw429 := false
	for i := 0; i < 8; i++ {
		rec := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": "wrongguess"})
		if rec.Code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401 or 429, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	if !saw429 {
		t.Fatal("expected to hit a 429 rate-limit after repeated failures")
	}

	// A subsequent request while still blocked hits the IsRateLimited
	// short-circuit at the top of AuthLogin (also 429), even with the correct
	// password.
	recBlocked := postJSON(t, ctx.AuthLogin, "/api/auth/login", map[string]interface{}{"password": "correcthorse"})
	if recBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 while still rate-limited, got %d", recBlocked.Code)
	}
}
