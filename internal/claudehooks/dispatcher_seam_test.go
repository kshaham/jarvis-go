package claudehooks

// Dispatcher-level seam tests not covered by dispatcher_test.go:
//   - SetToolPolicy + PreToolUse deny path (onPreToolUse 50% -> full)
//   - onStop transcript edge cases (no usage / read failure)
//   - onPostToolUse failure broadcast branch
//   - unregistered-agent fast-path (no body read, fail-open 204)
//   - per-event JSON decode failures inside dispatch (fail-open)
//   - concurrent fire under -race against a shared Dispatcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fireRaw POSTs a raw (possibly malformed) JSON body string through the
// chi router, bypassing json.Marshal so we can exercise the decode-error
// branches inside dispatch(). Distinct from fireEvent (which marshals a
// typed payload) to avoid a helper collision.
func fireRaw(t *testing.T, d *Dispatcher, sessionID, event, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/claude/hooks/{sessionId}/{event}", d.HandleEvent)
	req := httptest.NewRequest(http.MethodPost,
		"/api/claude/hooks/"+sessionID+"/"+event, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDispatcher_PreToolUse_DenyViaPolicy(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	// Default policy denies recursive rm of system paths.
	d.SetToolPolicy(NewToolPolicyStore(""))

	rec := fireEvent(t, d, "sess-0001", "PreToolUse", PreToolUseEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PreToolUse"},
		ToolName:  "Bash",
		ToolInput: mustMarshal(t, map[string]string{"command": "rm -rf /etc/passwd"}),
		ToolUseID: "toolu_deny",
	})
	// Deny path returns a HookResponse -> 200 + JSON body.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (deny response)", rec.Code)
	}
	var resp HookResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, rec.Body.String())
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("permissionDecision = %q, want deny", resp.HookSpecificOutput.PermissionDecision)
	}
	if resp.HookSpecificOutput.PermissionDecisionReason == "" {
		t.Error("expected a non-empty deny reason")
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", resp.HookSpecificOutput.HookEventName)
	}
	// ActiveTool is still set before the deny (so the dashboard shows what
	// the agent tried), and a toolDenied broadcast fires.
	if reg.state["sess-0001"].activeTool != "Bash" {
		t.Errorf("activeTool = %q, want Bash", reg.state["sess-0001"].activeTool)
	}
	if _, ok := findBroadcast(reg, "office:toolDenied"); !ok {
		t.Error("expected office:toolDenied broadcast on deny")
	}
}

func TestDispatcher_PreToolUse_PolicyAllows(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	d.SetToolPolicy(NewToolPolicyStore(""))

	rec := fireEvent(t, d, "sess-0001", "PreToolUse", PreToolUseEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PreToolUse"},
		ToolName:  "Bash",
		ToolInput: mustMarshal(t, map[string]string{"command": "ls -la"}),
	})
	// Allowed -> nil response -> 204, no deny broadcast.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (allowed)", rec.Code)
	}
	if _, ok := findBroadcast(reg, "office:toolDenied"); ok {
		t.Error("benign command should not produce office:toolDenied")
	}
	if reg.state["sess-0001"].activeTool != "Bash" {
		t.Errorf("activeTool = %q, want Bash", reg.state["sess-0001"].activeTool)
	}
}

func TestDispatcher_PostToolUseFailure_BroadcastsFailure(t *testing.T) {
	var observed int
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].activeTool = "Bash"
	d := NewDispatcher(reg, func(_, _, _ string) { observed++ })

	rec := fireEvent(t, d, "sess-0001", "PostToolUseFailure", PostToolUseEvent{
		BaseEvent:  BaseEvent{SessionID: "sess-0001", HookEventName: "PostToolUseFailure"},
		ToolName:   "Bash",
		ToolResult: json.RawMessage(`{"error":"exit 1"}`),
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	// ActiveTool cleared, loop detector fed, AND failure broadcast emitted.
	if reg.state["sess-0001"].activeTool != "" {
		t.Errorf("activeTool not cleared: %q", reg.state["sess-0001"].activeTool)
	}
	if observed != 1 {
		t.Errorf("loop observer called %d times, want 1", observed)
	}
	b, ok := findBroadcast(reg, "office:agentToolFailure")
	if !ok {
		t.Fatal("expected office:agentToolFailure broadcast on failure")
	}
	m := broadcastData(t, b)
	if m["tool"] != "Bash" {
		t.Errorf("tool = %v, want Bash", m["tool"])
	}
}

func TestDispatcher_PostToolUse_NoObserver(t *testing.T) {
	// observeToolInvocation nil: must not panic and must clear the tool.
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].activeTool = "Read"
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PostToolUse", PostToolUseEvent{
		BaseEvent:  BaseEvent{SessionID: "sess-0001", HookEventName: "PostToolUse"},
		ToolName:   "Read",
		ToolResult: json.RawMessage(`"contents"`),
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].activeTool != "" {
		t.Errorf("activeTool not cleared: %q", reg.state["sess-0001"].activeTool)
	}
	// Success PostToolUse must NOT emit a failure broadcast.
	if _, ok := findBroadcast(reg, "office:agentToolFailure"); ok {
		t.Error("success PostToolUse should not broadcast failure")
	}
}

func TestDispatcher_Stop_NoUsageInTranscript(t *testing.T) {
	// Transcript with no assistant usage -> ReadLatestUsage returns
	// ErrNoUsage; onStop must still flip working=false but record nothing.
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	// Only a user line, no assistant usage.
	if err := os.WriteFile(path, []byte(`{"type":"user","text":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].sdkWorking = true
	d := NewDispatcher(reg, nil)

	rec := fireEvent(t, d, "sess-0001", "Stop", StopEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", TranscriptPath: path, HookEventName: "Stop"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].sdkWorking {
		t.Error("Stop should flip sdkWorking=false even with no usage")
	}
	if reg.state["sess-0001"].cumInput != 0 || reg.state["sess-0001"].cumOutput != 0 {
		t.Errorf("no usage should not record tokens: in=%d out=%d",
			reg.state["sess-0001"].cumInput, reg.state["sess-0001"].cumOutput)
	}
	if _, ok := findBroadcast(reg, "office:tokens"); ok {
		t.Error("no usage should not broadcast office:tokens")
	}
}

func TestDispatcher_Stop_TranscriptReadFailure(t *testing.T) {
	// Nonexistent path -> ReadLatestUsage returns a non-ErrNoUsage error;
	// onStop logs a warning, flips working=false, records nothing.
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].sdkWorking = true
	d := NewDispatcher(reg, nil)

	rec := fireEvent(t, d, "sess-0001", "Stop", StopEvent{
		BaseEvent: BaseEvent{
			SessionID:      "sess-0001",
			TranscriptPath: "/nonexistent/path/transcript.jsonl",
			HookEventName:  "Stop",
		},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].sdkWorking {
		t.Error("Stop should flip sdkWorking=false even on transcript read failure")
	}
	if reg.state["sess-0001"].cumInput != 0 {
		t.Errorf("read failure should not record tokens, got cumInput=%d", reg.state["sess-0001"].cumInput)
	}
}

func TestDispatcher_Stop_EmptyTranscriptPath(t *testing.T) {
	// Empty transcript path is a degenerate read failure; still fail-open.
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].sdkWorking = true
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "Stop", StopEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "Stop"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].sdkWorking {
		t.Error("Stop should flip sdkWorking=false with empty transcript path")
	}
}

func TestDispatcher_UnregisteredAgent_SkipsBodyDispatch(t *testing.T) {
	// An unknown session must short-circuit BEFORE body read/dispatch:
	// TouchAgentAPI fails (returns false) and no handler-level broadcast
	// fires, but the response is still fail-open 204.
	reg := newFakeRegistry() // empty registry
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-ghost", "SessionStart", SessionStartEvent{
		BaseEvent: BaseEvent{SessionID: "sess-ghost", HookEventName: "SessionStart"},
		Source:    "startup",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (fail-open for unknown session)", rec.Code)
	}
	// onSessionStart never runs, so no broadcast.
	if _, ok := findBroadcast(reg, "office:claudeSessionStarted"); ok {
		t.Error("unregistered session should not reach onSessionStart broadcast")
	}
	// firstSeen is still recorded for the loud-first-event log.
	if !d.firstSeen["sess-ghost"] {
		t.Error("firstSeen should be recorded even for unregistered sessions")
	}
}

// TestDispatcher_DecodeErrors_FailOpen drives malformed JSON through the
// per-event decode branches in dispatch(). All must fail-open with 204
// (the dispatch returns an error -> HandleEvent writes 204).
func TestDispatcher_DecodeErrors_FailOpen(t *testing.T) {
	events := []string{
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
		"PostToolUseFailure", "Stop", "SessionEnd", "Notification",
		"PreCompact", "PostCompact", "PostToolBatch", "StopFailure",
		"PermissionRequest", "PermissionDenied", "TaskCreated", "TaskCompleted",
		"SubagentStart", "SubagentStop", "CwdChanged", "InstructionsLoaded",
		"ConfigChange", "Elicitation", "ElicitationResult",
		"WorktreeCreate", "WorktreeRemove",
	}
	for _, ev := range events {
		t.Run(ev, func(t *testing.T) {
			reg := newFakeRegistry("sess-0001")
			d := NewDispatcher(reg, nil)
			rec := fireRaw(t, d, "sess-0001", ev, `{"tool_calls": not-json`)
			if rec.Code >= 400 {
				t.Errorf("%s with bad JSON: status = %d, want < 400 (fail-open)", ev, rec.Code)
			}
		})
	}
}

func TestDispatcher_ConcurrentFire_RaceSafe(t *testing.T) {
	// Many sessions firing assorted events concurrently against one
	// Dispatcher. Exercises the firstSeen mutex + fakeRegistry mutex
	// under -race.
	reg := newFakeRegistry(
		"sess-0001", "sess-0002", "sess-0003", "sess-0004",
		"sess-0005", "sess-0006", "sess-0007", "sess-0008",
	)
	d := NewDispatcher(reg, func(_, _, _ string) {})
	d.SetToolPolicy(NewToolPolicyStore(""))

	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		sid := []string{
			"sess-0001", "sess-0002", "sess-0003", "sess-0004",
			"sess-0005", "sess-0006", "sess-0007", "sess-0008",
		}[i-1]
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = fireEvent(t, d, sessionID, "PreToolUse", PreToolUseEvent{
					BaseEvent: BaseEvent{SessionID: sessionID, HookEventName: "PreToolUse"},
					ToolName:  "Read",
					ToolInput: mustMarshal(t, map[string]string{"file_path": "/tmp/x"}),
				})
				_ = fireEvent(t, d, sessionID, "PostToolUse", PostToolUseEvent{
					BaseEvent:  BaseEvent{SessionID: sessionID, HookEventName: "PostToolUse"},
					ToolName:   "Read",
					ToolResult: json.RawMessage(`"ok"`),
				})
				_ = fireEvent(t, d, sessionID, "Notification", NotificationEvent{
					BaseEvent: BaseEvent{SessionID: sessionID, HookEventName: "Notification"},
					Message:   "ping",
				})
			}
		}(sid)
	}
	wg.Wait()

	// Every session should have logged its first event exactly once.
	for i := 1; i <= 8; i++ {
		sid := []string{
			"sess-0001", "sess-0002", "sess-0003", "sess-0004",
			"sess-0005", "sess-0006", "sess-0007", "sess-0008",
		}[i-1]
		if !d.firstSeen[sid] {
			t.Errorf("firstSeen[%s] not set after concurrent fire", sid)
		}
	}
}
