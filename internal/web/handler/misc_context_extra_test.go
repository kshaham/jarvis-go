package handler

// Coverage for remaining low/zero-coverage helpers that don't require a
// subprocess: the provider env-var builders + OAuth token reader (context.go),
// resolveOfficeSessionMode (office.go), CancelPlanGeneration + RalphWizard file
// handlers (misc.go), and the respawnSessionWriter adapter (session.go).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// --- context.go: getProviderEnvVarsFor ---

func TestGetProviderEnvVarsFor_ClaudeAPIAndZAI(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}

	// claude-api with a key.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"claudeApiKey": "sk-ant-xyz"})
	env := ctx.getProviderEnvVarsFor("claude-api", "", "")
	if env["ANTHROPIC_API_KEY"] != "sk-ant-xyz" {
		t.Errorf("ANTHROPIC_API_KEY=%q, want sk-ant-xyz", env["ANTHROPIC_API_KEY"])
	}
	// Opus default pin present for Claude providers.
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "claude-opus-4-8" {
		t.Errorf("opus pin=%q, want claude-opus-4-8", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}

	// zai-glm with key + sonnet override.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"provider": "zai-glm", "zaiApiKey": "zai-123",
	})
	envZ := ctx.getProviderEnvVarsFor("zai-glm", "glm-custom", "glm-sonnet-override")
	if envZ["ANTHROPIC_AUTH_TOKEN"] != "zai-123" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN=%q, want zai-123", envZ["ANTHROPIC_AUTH_TOKEN"])
	}
	if envZ["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-custom" {
		t.Errorf("opus=%q, want glm-custom", envZ["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if envZ["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-sonnet-override" {
		t.Errorf("sonnet override=%q, want glm-sonnet-override", envZ["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	if envZ["ANTHROPIC_BASE_URL"] == "" {
		t.Error("expected a base URL for zai-glm")
	}
}

func TestGetProviderEnvVarsFor_CodexDropsOpusPin(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-openai-key")
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	env := ctx.getProviderEnvVarsFor("openai-codex", "", "")
	if _, present := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; present {
		t.Error("Codex provider must drop the Anthropic Opus pin")
	}
	if env["OPENAI_API_KEY"] != "env-openai-key" {
		t.Errorf("OPENAI_API_KEY=%q, want env-openai-key", env["OPENAI_API_KEY"])
	}
}

// --- context.go: readClaudeOAuthToken ---

func TestReadClaudeOAuthToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No credentials file -> empty.
	if tok := readClaudeOAuthToken(); tok != "" {
		t.Errorf("expected empty token with no credentials file, got %q", tok)
	}

	// Write a credentials file with claudeAiOauth.accessToken.
	credDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"oauth-tok-123"}}`
	if err := os.WriteFile(filepath.Join(credDir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	if tok := readClaudeOAuthToken(); tok != "oauth-tok-123" {
		t.Errorf("token=%q, want oauth-tok-123", tok)
	}
}

// --- office.go: resolveOfficeSessionMode ---

func TestResolveOfficeSessionMode(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}

	// Default when unset -> "sdk".
	if got := ctx.resolveOfficeSessionMode(); got != "sdk" {
		t.Errorf("default mode=%q, want sdk", got)
	}

	// Known modes pass through.
	for _, mode := range []string{"sdk", "hooks", "legacy"} {
		ctx.StateStore.UpdateSettings(map[string]interface{}{"officeSessionMode": mode})
		if got := ctx.resolveOfficeSessionMode(); got != mode {
			t.Errorf("mode=%q, want %q", got, mode)
		}
	}

	// Unknown mode falls back to "sdk".
	ctx.StateStore.UpdateSettings(map[string]interface{}{"officeSessionMode": "bogus"})
	if got := ctx.resolveOfficeSessionMode(); got != "sdk" {
		t.Errorf("unknown mode should fall back to sdk, got %q", got)
	}
}

// --- misc.go: CancelPlanGeneration ---

func TestCancelPlanGeneration_SpecificNotFound(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		ActivePlanOrchestrators: map[string]string{},
		SessionManager:          pty.NewSessionManager(10),
	}
	rec := postJSON(t, ctx.CancelPlanGeneration, "/api/cancel-plan-generation", map[string]interface{}{
		"orchestratorId": "ghost",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown orchestrator, got %d", rec.Code)
	}
}

func TestCancelPlanGeneration_CancelAllEmpty(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		ActivePlanOrchestrators: map[string]string{},
		SessionManager:          pty.NewSessionManager(10),
		SSEBroker:               sse.NewBroker(100, 10),
	}
	rec := postJSON(t, ctx.CancelPlanGeneration, "/api/cancel-plan-generation", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel all (empty): expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	data, _ := resp["data"].(map[string]interface{})
	cancelled, _ := data["cancelled"].([]interface{})
	if len(cancelled) != 0 {
		t.Errorf("expected 0 cancelled, got %d", len(cancelled))
	}
}

// --- misc.go: GetRalphWizardFiles / GetRalphWizardFile ---

func TestGetRalphWizardFiles_CaseNotFound(t *testing.T) {
	t.Parallel()
	ctx := &Context{CaseManager: cases.NewCaseManager(t.TempDir())}
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/ghost/ralph-wizard/files", nil), "caseName", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetRalphWizardFiles(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown case, got %d", rec.Code)
	}
}

func TestGetRalphWizardFiles_NoWizardDir404(t *testing.T) {
	t.Parallel()
	cm := cases.NewCaseManager(t.TempDir())
	if _, err := cm.Create("rw"); err != nil {
		t.Fatalf("create case: %v", err)
	}
	ctx := &Context{CaseManager: cm}
	// A freshly created case has no ralph-wizard directory, so the lister
	// returns a not-found error which the handler surfaces as 404.
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/rw/ralph-wizard/files", nil), "caseName", "rw")
	rec := httptest.NewRecorder()
	ctx.GetRalphWizardFiles(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing wizard dir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRalphWizardFile_Validation(t *testing.T) {
	t.Parallel()
	cm := cases.NewCaseManager(t.TempDir())
	if _, err := cm.Create("rw2"); err != nil {
		t.Fatalf("create case: %v", err)
	}
	ctx := &Context{CaseManager: cm}

	// Case not found.
	reqNF := twoParamReq("GET", "/api/cases/ghost/ralph-wizard/file/x", "", "caseName", "ghost", "*", "x")
	recNF := httptest.NewRecorder()
	ctx.GetRalphWizardFile(recNF, reqNF)
	if recNF.Code != http.StatusNotFound {
		t.Fatalf("case not found: expected 404, got %d", recNF.Code)
	}

	// Empty file path -> 400.
	reqEmpty := twoParamReq("GET", "/api/cases/rw2/ralph-wizard/file/", "", "caseName", "rw2", "*", "")
	recEmpty := httptest.NewRecorder()
	ctx.GetRalphWizardFile(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("empty path: expected 400, got %d", recEmpty.Code)
	}

	// Missing wizard file -> 404.
	reqMiss := twoParamReq("GET", "/api/cases/rw2/ralph-wizard/file/none.md", "", "caseName", "rw2", "*", "none.md")
	recMiss := httptest.NewRecorder()
	ctx.GetRalphWizardFile(recMiss, reqMiss)
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("missing file: expected 404, got %d", recMiss.Code)
	}
}

// --- session.go: respawnSessionWriter.WriteToSession ---

func TestRespawnSessionWriter_WriteToSession_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	sess, _ := ctx.SessionManager.GetSession(id)
	writer := &respawnSessionWriter{session: sess}
	// A live shell accepts the write via its screen.
	if !writer.WriteToSession(id, "echo hi") {
		t.Error("expected WriteToSession to succeed for a live session")
	}
}
