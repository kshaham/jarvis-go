package handler

// Round 2 coverage for the agent-control happy paths that the validation-only
// tests can't reach: OfficeAgentBumpWorkdir's SetWorktreePath + redirect-prompt
// dispatch (both the delivered and the undelivered branch), the reason-default
// fill, and OfficeAgentRestart's pre-spawn resolution branches (template-member
// build, working-dir resolution, the "no working dir" 500, and the spawn-failed
// 500 with the claude binary removed from PATH).

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// rejectingSessions is a SessionStore whose WriteInkPrompt always reports the
// session is not accepting prompts (returns false). Used to drive the
// "redirect prompt not delivered" warn-branch in OfficeAgentBumpWorkdir.
type rejectingSessions struct{ stubSessions }

func (rejectingSessions) WriteInkPrompt(sessionID, prompt string) bool { return false }

// newAgentCtlCtxWithStore wires a Context whose OfficeManager uses the supplied
// session store and whose StateStore allowlists `dir` so ResolveWorkingDir
// accepts it.
func newAgentCtlCtxWithStore(t *testing.T, sess office.SessionStore, dir string) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(100 * time.Millisecond) })
	mgr := office.NewManager(sess, func(string, interface{}) {}, nil)
	ctx := &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(t.Context()),
		OfficeManager:  mgr,
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{"workingDirAllowlist": []interface{}{dir}})
	return ctx
}

func TestOfficeAgentBumpWorkdir_HappyDeliversRedirect(t *testing.T) {
	dir := t.TempDir()
	sess := &recordingSessions{}
	ctx := newAgentCtlCtxWithStore(t, sess, dir)
	ctx.OfficeManager.RegisterAgent("sess-bump", "qa", &office.TeamMemberConfig{Name: "QA Bot"})
	// ResolveWorkingDir dereferences symlinks (e.g. macOS /tmp -> /private/tmp),
	// so the response/bookkeeping carry the resolved form, not the raw temp path.
	wantDir, err := ctx.ResolveWorkingDir(dir)
	if err != nil {
		t.Fatalf("resolve dir: %v", err)
	}

	rec := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId":  "sess-bump",
		"workingDir": dir,
		"reason":     "priorities shifted to frontend",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["workingDir"] != wantDir {
		t.Errorf("workingDir=%v, want %s", resp["workingDir"], wantDir)
	}
	// The agent's WorktreePath must now point at the resolved dir.
	agent := ctx.OfficeManager.GetAgent("sess-bump")
	if got := agent.GetWorktreePath(); got != wantDir {
		t.Errorf("WorktreePath=%q, want %q", got, wantDir)
	}
	// The redirect prompt must have been delivered (recordingSessions captures it).
	got, ok := sess.injected["sess-bump"]
	if !ok {
		t.Fatal("expected redirect prompt delivered to sess-bump")
	}
	if !strings.HasPrefix(got, "[WORKING DIR CHANGE]") {
		t.Errorf("prompt missing redirect header, got %q", got)
	}
}

func TestOfficeAgentBumpWorkdir_ReasonDefaultAndUndelivered(t *testing.T) {
	dir := t.TempDir()
	// rejectingSessions makes WriteInkPrompt return false -> warn branch, but the
	// request still succeeds because the workdir bookkeeping already applied.
	ctx := newAgentCtlCtxWithStore(t, rejectingSessions{}, dir)
	ctx.OfficeManager.RegisterAgent("sess-x", "qa", &office.TeamMemberConfig{Name: "QA Bot"})
	wantDir, err := ctx.ResolveWorkingDir(dir)
	if err != nil {
		t.Fatalf("resolve dir: %v", err)
	}

	// No "reason" -> the handler fills "(no reason given)".
	rec := postJSON(t, ctx.OfficeAgentBumpWorkdir, "/api/office/agent/workdir", map[string]interface{}{
		"sessionId":  "sess-x",
		"workingDir": dir,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when prompt undelivered, got %d: %s", rec.Code, rec.Body.String())
	}
	if ctx.OfficeManager.GetAgent("sess-x").GetWorktreePath() != wantDir {
		t.Error("WorktreePath should still be updated when prompt undelivered")
	}
}

// TestOfficeAgentRestart_NoWorkingDir500 drives the restart flow up to the
// ResolveRestartWorkingDir guard: a registered agent with a valid Config but no
// worktree path and no live SDK session yields an empty working dir -> 500.
func TestOfficeAgentRestart_NoWorkingDir500(t *testing.T) {
	ctx := newAgentControlsContext(t, &recordingSessions{})
	ctx.OfficeManager.RegisterAgent("sess-r", "lead", &office.TeamMemberConfig{Name: "Lead"})

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "sess-r",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown working dir, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "working directory") {
		t.Errorf("expected working-directory error, got %q", body)
	}
}

// TestOfficeAgentRestart_SpawnFails500 registers an agent WITH a worktree path
// (so ResolveRestartWorkingDir succeeds and the flow proceeds to the spawn),
// then strips the claude binary from PATH so spawnAgentSession fails to create
// a replacement session -> 500 "failed to create replacement session".
func TestOfficeAgentRestart_SpawnFails500(t *testing.T) {
	dir := t.TempDir()
	ctx := newAgentCtlCtxWithStore(t, &recordingSessions{}, dir)
	// Force the PTY (legacy) spawn path and break PATH so the binary lookup +
	// PTY start both fail deterministically (no real claude subprocess).
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
		"officeSessionMode":   "legacy",
	})
	emptyPath := t.TempDir()
	t.Setenv("PATH", emptyPath)

	ctx.OfficeManager.RegisterAgent("sess-spawn", "lead", &office.TeamMemberConfig{Name: "Lead"})
	ctx.OfficeManager.GetAgent("sess-spawn").SetWorktreePath(dir)

	rec := postJSON(t, ctx.OfficeAgentRestart, "/api/office/agent/restart", map[string]interface{}{
		"sessionId": "sess-spawn",
		"reason":    "context bloated",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when spawn fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "replacement session") {
		t.Errorf("expected replacement-session error, got %q", body)
	}
}
