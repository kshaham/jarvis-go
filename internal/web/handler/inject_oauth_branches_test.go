package handler

// Branch fills: OfficeAgentInjectPrompt happy path (registered agent + a
// session store that accepts the prompt) and readClaudeOAuthToken's
// oauthAccount + token-field fallbacks.

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeAgentInjectPrompt_HappyDelivers(t *testing.T) {
	rec := &recordingSessions{}
	ctx := newAgentControlsContext(t, rec)
	ctx.OfficeManager.RegisterAgent("sess-i", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	resp := postJSON(t, ctx.OfficeAgentInjectPrompt, "/api/office/agent/inject", map[string]interface{}{
		"sessionId": "sess-i", "prompt": "refocus on the goal",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("inject: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	// The decorated prompt should have been delivered to the session store.
	if got := rec.injected["sess-i"]; got == "" {
		t.Fatal("expected the prompt to be injected into the session")
	}
}

func TestReadClaudeOAuthToken_OAuthAccountFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No claudeAiOauth -> falls back to oauthAccount; no accessToken -> falls
	// back to the "token" field.
	creds := `{"oauthAccount":{"token":"legacy-tok"}}`
	if err := os.WriteFile(filepath.Join(credDir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	if tok := readClaudeOAuthToken(); tok != "legacy-tok" {
		t.Errorf("token=%q, want legacy-tok (oauthAccount.token fallback)", tok)
	}
}

func TestReadClaudeOAuthToken_MalformedJSONReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credDir := filepath.Join(home, ".claude")
	_ = os.MkdirAll(credDir, 0o755)
	if err := os.WriteFile(filepath.Join(credDir, ".credentials.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	if tok := readClaudeOAuthToken(); tok != "" {
		t.Errorf("expected empty token for malformed credentials, got %q", tok)
	}
}
