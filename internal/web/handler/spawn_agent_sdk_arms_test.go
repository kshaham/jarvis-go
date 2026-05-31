package handler

// Round-4 branch fills for spawnAgentSession's SDK-path residual arms:
//   - DisableWorkflows env injection (63-67)
//   - AgentTokenMgr token resolution + JARVIS_AGENT_TOKEN injection (93-95,
//     104-106)
//   - per-member MaxTurns + Model overrides (111-113, 120-122)
//   - WriteAppendSystemPromptFile failure -> inline fallback (149-150)

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

func spawnAgentCtxWithToken(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  mgr,
		AgentTokenMgr:  auth.NewAgentTokenManager(t.TempDir()),
	}
}

// TestSpawnAgentSession_SDKTokenOverridesWorkflows drives the SDK path with an
// agent token, a per-member override (MaxTurns + Model), and DisableWorkflows
// set, covering all four override/env arms at once.
func TestSpawnAgentSession_SDKTokenOverridesWorkflows(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-ov","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-ov","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	installClaudeOnPATH(t, stream)

	ctx := spawnAgentCtxWithToken(t)
	if ctx.AgentTokenMgr.Token() == "" {
		t.Fatal("expected a non-empty agent token")
	}
	member := teamtemplates.TeamTemplateMember{ID: "vex", Name: "Vex", Role: "Engineer", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship it", DisableWorkflows: true}
	teamConfigs := []*office.TeamMemberConfig{{ID: "vex", Name: "Vex", Tier: "worker"}}

	ov := &memberOverride{MaxTurns: 9, Model: "claude-sonnet-4-6"}
	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{}, teamConfigs,
		true, true, ov, "claude-subscription", "", spawnAgentSpec{})

	if mode != "sdk" || sessionID == "" {
		t.Fatalf("expected sdk mode, got %q (sessionID=%q)", mode, sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSDKSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
	if _, ok := ctx.SessionManager.GetSDKSession(sessionID); !ok {
		t.Errorf("SDK session %s not registered", sessionID)
	}
}

// TestSpawnAgentSession_SDKAppendPromptFileFallback drives the
// WriteAppendSystemPromptFile failure arm: a read-only working dir means the
// per-tier prompt file can't be written, so spawnAgentSession logs and falls
// back to the inline AppendSystemPrompt (still spawning the SDK session).
func TestSpawnAgentSession_SDKAppendPromptFileFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-ro","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-ro","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installClaudeOnPATH(t, stream)

	ctx := spawnAgentCtx(t)
	roWD := t.TempDir()
	if err := os.Chmod(roWD, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roWD, 0o755) })

	member := teamtemplates.TeamTemplateMember{ID: "ro", Name: "ReadOnly", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: roWD, Mission: "prompt file fails"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "ro", Name: "ReadOnly"}}

	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{}, teamConfigs,
		false, true, nil, "claude-subscription", "claude-opus-4-8", spawnAgentSpec{})

	// The SDK spawn must still succeed via the inline-prompt fallback.
	if mode != "sdk" || sessionID == "" {
		t.Fatalf("expected sdk mode despite prompt-file write failure, got %q (sessionID=%q)", mode, sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSDKSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
}
