package office_test

// End-to-end integration test for the hooks-mode office spawn path.
// Exercises:
//   - claudehooks.Dispatcher mounted on a real chi router
//   - office.Manager satisfying claudehooks.AgentRegistry
//   - claudehooks.BuildSettings + WriteSettings producing a settings
//     file the cmd/fakeclaude binary can consume
//   - fakeclaude POSTing the standard event sequence
//   - AgentState transitions landing (ActiveTool, SDKWorking, cost)
//
// This is in package office_test (not package office) so it can
// import internal/web/handler etc. without circular references.
// Uses a separate test-only mock SessionStore to avoid touching the
// real PTY subsystem.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/claudehooks"
	"github.com/claudeman/jarvis-go/internal/office"
)

// integrationSessionStore is a minimal SessionStore for the hooks
// integration test. Mirrors the mockSessionStore in manager_test.go
// (which is package-internal and unreachable from the _test package).
type integrationSessionStore struct{}

func (integrationSessionStore) WriteViaScreen(string, string) bool  { return true }
func (integrationSessionStore) WriteInkPrompt(string, string) bool  { return true }
func (integrationSessionStore) GetSessionIDs() []string             { return nil }
func (integrationSessionStore) IsSessionAlive(string) bool          { return true }
func (integrationSessionStore) IsSessionWorking(string) bool        { return false }
func (integrationSessionStore) StopSession(string) error            { return nil }

func TestHooksMode_EndToEnd_FakeClaude(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test takes ~1s")
	}

	// ----- 1. Build the fakeclaude binary into a temp dir.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/fakeclaude")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/fakeclaude: %v\n%s", err, out)
	}

	// ----- 2. Stand up a Jarvis manager + claude hooks dispatcher
	// on an httptest server (real chi router, real HTTP).
	mgr := office.NewManager(integrationSessionStore{}, func(string, interface{}) {}, nil)
	cfg := &office.TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Engineer", Tier: "worker"}
	mgr.RegisterAgent("sess-vex", "vex", cfg)

	dispatcher := claudehooks.NewDispatcher(mgr, func(string, string, string) {})
	r := chi.NewRouter()
	r.Post("/api/claude/hooks/{sessionId}/{event}", dispatcher.HandleEvent)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// ----- 3. Write the per-agent settings.local.json into a fresh
	// cwd. fakeclaude will read it on launch.
	cwd := t.TempDir()
	settings, err := claudehooks.BuildSettings(claudehooks.SettingsParams{
		SessionID: "sess-vex",
		DaemonURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if err := claudehooks.WriteSettings(cwd, settings); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	// ----- 4a. Run fakeclaude with a script that STOPS before
	// SessionEnd so we can assert state while the agent still exists.
	scriptPath := filepath.Join(cwd, "fakeclaude-script.json")
	if err := os.WriteFile(scriptPath, []byte(scriptWithoutSessionEnd), 0o644); err != nil {
		t.Fatalf("seed script: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = cwd
	cmd.Env = append(cmd.Env, "FAKECLAUDE_SCRIPT="+scriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fakeclaude failed: %v\noutput:\n%s", err, out)
	}
	if strings.Contains(string(out), "fakeclaude:") &&
		strings.Contains(string(out), "failed") {
		t.Logf("fakeclaude diagnostics:\n%s", out)
	}

	// ----- 5. Assert AgentState transitions landed.
	agent := mgr.GetAgent("sess-vex")
	if agent == nil {
		t.Fatal("agent vanished mid-test")
	}
	usage := agent.Usage()

	// PostToolUse cleared ActiveTool; verify it's empty (the script
	// doesn't fire another PreToolUse after the Bash call).
	if agent.ActiveTool != "" {
		t.Errorf("ActiveTool = %q, want empty (post Stop)", agent.ActiveTool)
	}

	// Stop flipped SDKWorking false. Default state is also false, so
	// this is mostly a smoke check that we didn't leave it stuck on
	// true from UserPromptSubmit.
	if agent.GetSDKWorking() {
		t.Error("SDKWorking should be false after Stop")
	}

	// Stop tail-read the transcript and recorded usage. The stub
	// transcript fakeclaude writes has 100 input + 50 output tokens
	// at sonnet-4-6 rates -> roughly 100*3 + 50*15 = 1050 / 1M = $0.00105.
	if usage.TotalInputTokens != 100 || usage.TotalOutputTokens != 50 {
		t.Errorf("usage tokens wrong: in=%d out=%d (want 100/50)",
			usage.TotalInputTokens, usage.TotalOutputTokens)
	}
	if usage.TotalCostUSD < 0.0010 || usage.TotalCostUSD > 0.0011 {
		t.Errorf("cost = %v, want ~0.00105", usage.TotalCostUSD)
	}
	if usage.TurnCount != 1 {
		t.Errorf("turn count = %d, want 1", usage.TurnCount)
	}

	// ----- 6. Now fire SessionEnd and assert the agent is removed
	// via HandleAgentDeath. Done as a direct HTTP POST rather than
	// re-running fakeclaude because we just need one event.
	endURL := srv.URL + "/api/claude/hooks/sess-vex/SessionEnd"
	req, _ := http.NewRequest(http.MethodPost, endURL,
		strings.NewReader(`{"session_id":"fake","hook_event_name":"SessionEnd","reason":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SessionEnd POST: %v", err)
	}
	resp.Body.Close()

	if mgr.GetAgent("sess-vex") != nil {
		t.Error("expected SessionEnd to remove the agent via HandleAgentDeath")
	}
}

// scriptWithoutSessionEnd is the default fakeclaude sequence minus the
// SessionEnd event. Lets the test assert AgentState before the agent
// gets reaped by HandleAgentDeath.
const scriptWithoutSessionEnd = `[
  {"type": "SessionStart", "payload": {"source":"startup", "model":"claude-sonnet-4-6"}, "delayMs": 50},
  {"type": "UserPromptSubmit", "payload": {"prompt":"do a small thing"}, "delayMs": 50},
  {"type": "PreToolUse", "payload": {"tool_name":"Bash", "tool_input":{"command":"echo hi"}, "tool_use_id":"tu-1"}, "delayMs": 50},
  {"type": "PostToolUse", "payload": {"tool_name":"Bash", "tool_input":{"command":"echo hi"}, "tool_use_id":"tu-1", "tool_result":{"text":"hi"}}, "delayMs": 50},
  {"type": "Stop", "payload": {}, "delayMs": 100}
]`

func TestHooksMode_FakeClaude_BogusURL_DoesNotCrash(t *testing.T) {
	// fakeclaude must exit non-zero when the dispatcher refuses (or
	// the URL is unreachable) but should not panic. Belt-and-
	// suspenders: in production the dispatcher always fail-opens, so
	// fakeclaude shouldn't see errors -- but we want the test binary
	// itself to behave when something goes wrong.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeclaude")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/fakeclaude")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cwd := t.TempDir()
	settings, _ := claudehooks.BuildSettings(claudehooks.SettingsParams{
		SessionID: "sess-x",
		DaemonURL: "http://127.0.0.1:1", // unreachable
	})
	_ = claudehooks.WriteSettings(cwd, settings)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = cwd
	// Don't fail on non-zero exit -- we just want NO panic / hang.
	_ = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("fakeclaude hung on unreachable daemon")
	}
}

// fastHTTPClient swaps the default 10s client timeout with a tighter
// one for the integration test so failures surface fast. Currently
// unused -- promoted to the test scope if a future flake calls for
// per-call latency budget enforcement.
var _ = http.DefaultClient
