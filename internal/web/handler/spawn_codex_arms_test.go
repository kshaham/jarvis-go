package handler

// Round-4 branch fills for spawnCodexAgentSession's residual arms that the
// earlier custom-provider happy-path tests don't reach:
//
//   - AgentTokenMgr token injection (477-479, 625-627)
//   - default model fill for non-subscription providers (502-504)
//   - the openai-codex `default` switch arm with an API key (534-544)
//   - per-member MaxTurns override (631-633)
//   - codexcfg.Write failure -> "","" (594-597)
//   - CreateCodexSession failure (max sessions) -> "","" + CODEX_HOME cleanup
//     (654-658)
//   - stageCodexAuth WriteFile failure (462-464)
//
// These avoid spawning a live `codex` end-to-end except where a stub binary is
// cheap; the failure arms are driven by fault injection (read-only HOME,
// maxSessions=0, unwritable staging target).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// codexSpawnCtxWithToken is codexSpawnCtx + a real AgentTokenManager so the
// `agentToken != ""` branches fire.
func codexSpawnCtxWithToken(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
		AgentTokenMgr:  auth.NewAgentTokenManager(t.TempDir()),
	}
	return ctx
}

// TestSpawnCodexAgentSession_TokenAndDefaultModelAndMaxTurns drives the
// AgentTokenMgr token injection, the empty-model default fill (gpt-5.5), and
// the per-member MaxTurns override in one openai-custom happy spawn.
func TestSpawnCodexAgentSession_TokenAndDefaultModelAndMaxTurns(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-tok"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	ctx := codexSpawnCtxWithToken(t)
	if ctx.AgentTokenMgr.Token() == "" {
		t.Fatal("expected a non-empty agent token for this test")
	}
	member := teamtemplates.TeamTemplateMember{ID: "tk", Name: "Tokey", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "build"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "tk", Name: "Tokey"}}

	// model "" + openai-custom (not subscription) => default fill to gpt-5.5.
	// ov.MaxTurns > 0 => per-member cap branch.
	ov := &memberOverride{MaxTurns: 7}
	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-test"},
		teamConfigs, true, ov, "openai-custom", "")
	if mode != "codex" || sessionID == "" {
		t.Fatalf("expected codex session, got mode=%q id=%q", mode, sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
	// The agent token is threaded into the subprocess env via JARVIS_AGENT_TOKEN.
	cx, ok := ctx.SessionManager.GetCodexSession(sessionID)
	if !ok {
		t.Fatalf("codex session %s not registered", sessionID)
	}
	if cx.WorkingDir != req.WorkingDir {
		t.Errorf("workingDir=%q, want %q", cx.WorkingDir, req.WorkingDir)
	}
}

// TestSpawnCodexAgentSession_OpenAICodexWithKey drives the switch `default`
// (openai-codex) arm with an API key present -> prov != nil custom block.
func TestSpawnCodexAgentSession_OpenAICodexWithKey(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-codex"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	ctx := codexSpawnCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "oc", Name: "OcKey", Tier: "lead"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "oc", Name: "OcKey"}}

	// provider="openai-codex" + OPENAI_API_KEY set => hasKey true => prov non-nil,
	// no auth.json staging required, reaching CreateCodexSession.
	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-codex"},
		teamConfigs, false, nil, "openai-codex", "gpt-5.5")
	if mode != "codex" || sessionID == "" {
		t.Fatalf("expected codex session for openai-codex+key, got mode=%q id=%q", mode, sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
}

// TestSpawnCodexAgentSession_ConfigWriteFails drives the codexcfg.Write error
// arm (594-597): a read-only HOME means MkdirAll under ~/.jarvis/codex fails.
func TestSpawnCodexAgentSession_ConfigWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	skipIfNoStubShell(t)

	roHome := t.TempDir()
	// Make HOME read-only so MkdirAll(home/.jarvis/codex/<id>) fails inside
	// codexcfg.Write.
	if err := os.Chmod(roHome, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roHome, 0o700) })
	t.Setenv("HOME", roHome)

	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	member := teamtemplates.TeamTemplateMember{ID: "rw", Name: "RoWriter", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "x"}

	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-x"},
		nil, false, nil, "openai-custom", "gpt-5.5")
	if sessionID != "" || mode != "" {
		t.Fatalf("expected empty result when config write fails, got (%q,%q)", sessionID, mode)
	}
}

// TestSpawnCodexAgentSession_CreateFailsMaxSessions drives the
// CreateCodexSession failure arm (654-658): a manager with maxSessions=0
// rejects the create, and the handler removes the CODEX_HOME it just wrote.
func TestSpawnCodexAgentSession_CreateFailsMaxSessions(t *testing.T) {
	skipIfNoStubShell(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx := &Context{
		SessionManager: pty.NewSessionManager(0), // every create is rejected
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	fixedID := "codex-create-fail-fixed"
	prev := newSpawnSessionID
	newSpawnSessionID = func() string { return fixedID }
	t.Cleanup(func() { newSpawnSessionID = prev })

	member := teamtemplates.TeamTemplateMember{ID: "mx", Name: "MaxOut", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "x"}

	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-x"},
		nil, false, nil, "openai-custom", "gpt-5.5")
	if sessionID != "" || mode != "" {
		t.Fatalf("expected empty result when CreateCodexSession fails, got (%q,%q)", sessionID, mode)
	}
	// The agent-token-bearing CODEX_HOME must be cleaned up on failure.
	codexHome := filepath.Join(home, ".jarvis", "codex", fixedID)
	if _, err := os.Stat(codexHome); !os.IsNotExist(err) {
		t.Errorf("expected CODEX_HOME %s removed on failure, stat err=%v", codexHome, err)
	}
}

// TestStageCodexAuth_WriteFails drives stageCodexAuth's WriteFile error arm
// (462-464): a readable source auth.json but an unwritable destination dir.
func TestStageCodexAuth_WriteFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	home := t.TempDir()
	// Stage a source auth.json in ~/.codex/auth.json so the candidate read succeeds.
	srcDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "auth.json"), []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatalf("write src auth: %v", err)
	}

	// Destination codexHome exists but is read-only so WriteFile(dst) fails.
	codexHome := t.TempDir()
	if err := os.Chmod(codexHome, 0o500); err != nil {
		t.Fatalf("chmod dst: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(codexHome, 0o700) })

	err := stageCodexAuth(home, codexHome)
	if err == nil {
		t.Fatal("expected stageCodexAuth to fail writing into a read-only codexHome")
	}
	if !strings.Contains(err.Error(), "stage codex auth") {
		t.Errorf("expected 'stage codex auth' in error, got %v", err)
	}
}
