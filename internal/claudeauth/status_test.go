package claudeauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withFakeHome redirects HOME so credential reads land in a temp
// dir. Returns the simulated ~/.claude path.
func withFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".claude")
}

func writeCreds(t *testing.T, dir string, body map[string]interface{}) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".credentials.json")
	data, _ := json.Marshal(body)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckStatus_NoCredentialsFile(t *testing.T) {
	_ = withFakeHome(t)
	got := CheckStatus()
	if got.Authenticated || got.Reason != ReasonNoCredentials {
		t.Errorf("got %+v want Authenticated=false Reason=%q", got, ReasonNoCredentials)
	}
}

func TestCheckStatus_EmptyCredentialsFile(t *testing.T) {
	claudeDir := withFakeHome(t)
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(path, []byte("   \n  "), 0o600); err != nil {
		t.Fatal(err)
	}
	got := CheckStatus()
	if got.Authenticated || got.Reason != ReasonEmptyCredentials {
		t.Errorf("got %+v want Authenticated=false Reason=%q", got, ReasonEmptyCredentials)
	}
}

func TestCheckStatus_InvalidJSON(t *testing.T) {
	claudeDir := withFakeHome(t)
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(path, []byte("not json {"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := CheckStatus()
	if got.Authenticated || got.Reason != ReasonInvalidCredentials {
		t.Errorf("got %+v want Authenticated=false Reason=%q", got, ReasonInvalidCredentials)
	}
}

func TestCheckStatus_ClaudeAiOAuthFreshToken(t *testing.T) {
	claudeDir := withFakeHome(t)
	expires := float64(time.Now().Add(time.Hour).UnixMilli())
	writeCreds(t, claudeDir, map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accessToken":      "valid-token",
			"subscriptionType": "pro",
			"expiresAt":        expires,
		},
	})
	got := CheckStatus()
	if !got.Authenticated {
		t.Errorf("expected authenticated, got %+v", got)
	}
	if got.Method != "subscription" {
		t.Errorf("method=%q want subscription", got.Method)
	}
	if got.SubscriptionType != "pro" {
		t.Errorf("subscriptionType=%q want pro", got.SubscriptionType)
	}
}

func TestCheckStatus_OAuthExpiredToken(t *testing.T) {
	claudeDir := withFakeHome(t)
	// Token expired 1 hour ago.
	expires := float64(time.Now().Add(-time.Hour).UnixMilli())
	writeCreds(t, claudeDir, map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accessToken": "expired",
			"expiresAt":   expires,
		},
	})
	got := CheckStatus()
	if got.Authenticated || got.Reason != ReasonTokenExpired {
		t.Errorf("got %+v want Authenticated=false Reason=%q", got, ReasonTokenExpired)
	}
}

func TestCheckStatus_OAuthAccountAlternateTokenKey(t *testing.T) {
	// oauthAccount stores the token under "token" (older format)
	// rather than "accessToken". The check should handle both.
	claudeDir := withFakeHome(t)
	expires := float64(time.Now().Add(time.Hour).UnixMilli())
	writeCreds(t, claudeDir, map[string]interface{}{
		"oauthAccount": map[string]interface{}{
			"token":            "alternate-key",
			"subscriptionType": "team",
			"expiresAt":        expires,
		},
	})
	got := CheckStatus()
	if !got.Authenticated {
		t.Errorf("expected authenticated for alternate token key, got %+v", got)
	}
	if got.SubscriptionType != "team" {
		t.Errorf("subscriptionType=%q want team", got.SubscriptionType)
	}
}

func TestCheckStatus_APIKey(t *testing.T) {
	claudeDir := withFakeHome(t)
	writeCreds(t, claudeDir, map[string]interface{}{
		"apiKey": "sk-ant-xyz",
	})
	got := CheckStatus()
	if !got.Authenticated {
		t.Errorf("expected authenticated for API key, got %+v", got)
	}
	if got.Method != "api_key" {
		t.Errorf("method=%q want api_key", got.Method)
	}
}

func TestCheckStatus_NoValidTokenInAnyField(t *testing.T) {
	claudeDir := withFakeHome(t)
	writeCreds(t, claudeDir, map[string]interface{}{
		"someOtherField": "doesn't trigger any check",
	})
	got := CheckStatus()
	if got.Authenticated || got.Reason != ReasonNoValidToken {
		t.Errorf("got %+v want Reason=%q", got, ReasonNoValidToken)
	}
}

func TestCheckStatus_BlankSubscriptionTypeDefaultsToUnknown(t *testing.T) {
	claudeDir := withFakeHome(t)
	expires := float64(time.Now().Add(time.Hour).UnixMilli())
	writeCreds(t, claudeDir, map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accessToken": "valid",
			"expiresAt":   expires,
			// no subscriptionType field
		},
	})
	got := CheckStatus()
	if got.SubscriptionType != "unknown" {
		t.Errorf("subscriptionType=%q want unknown (default)", got.SubscriptionType)
	}
}

func TestCheckStatus_FallsBackToSymlinkTargetWhenCanonicalMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Only the symlink-target path exists; the canonical
	// ~/.claude/.credentials.json doesn't.
	targetDir := filepath.Join(home, ".claude-auth")
	expires := float64(time.Now().Add(time.Hour).UnixMilli())
	writeCreds(t, targetDir, map[string]interface{}{
		"apiKey": "sk-ant-fallback",
	})
	got := CheckStatus()
	if !got.Authenticated || got.Method != "api_key" {
		t.Errorf("expected api_key fallback, got %+v", got)
	}
	_ = expires // unused but kept for clarity
}
