package claudehooks

// Tests for the per-event handlers that were uncovered by
// dispatcher_test.go: Notification, Compact, PostToolBatch, StopFailure,
// PermissionRequest, PermissionDenied, TaskCreated, TaskCompleted,
// Subagent{Start,Stop}, CwdChanged, InstructionsLoaded, ConfigChange,
// Elicitation{,Result}, SessionStart broadcast, plus the unknown-event
// and summariseBatchTools/truncate helpers.
//
// All drive through Dispatcher.HandleEvent via fireEvent (defined in
// dispatcher_test.go) so the full decode -> dispatch -> handler ->
// broadcast path is exercised, then assert the resulting broadcasts and
// state mutations.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// findBroadcast returns the first captured broadcast with the given
// event name and a bool indicating whether one was found. Reads under
// the fakeRegistry mutex so it is safe to call after a -race fire.
func findBroadcast(reg *fakeRegistry, event string) (broadcastCall, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, b := range reg.broadcasts {
		if b.event == event {
			return b, true
		}
	}
	return broadcastCall{}, false
}

// broadcastData pulls a typed field from a broadcast's map payload.
func broadcastData(t *testing.T, b broadcastCall) map[string]interface{} {
	t.Helper()
	m, ok := b.data.(map[string]interface{})
	if !ok {
		t.Fatalf("broadcast %q data is %T, want map[string]interface{}", b.event, b.data)
	}
	return m
}

func TestDispatcher_SessionStart_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "SessionStart", SessionStartEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SessionStart"},
		Source:    "startup",
		Model:     "claude-opus-4-8",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:claudeSessionStarted")
	if !ok {
		t.Fatal("expected office:claudeSessionStarted broadcast")
	}
	m := broadcastData(t, b)
	if m["source"] != "startup" || m["model"] != "claude-opus-4-8" {
		t.Errorf("broadcast payload = %v, want source/model populated", m)
	}
	if m["sessionId"] != "sess-0001" {
		t.Errorf("sessionId = %v, want sess-0001", m["sessionId"])
	}
}

func TestDispatcher_Notification_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "Notification", NotificationEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "Notification"},
		Message:   "needs your input",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:claudeNotification")
	if !ok {
		t.Fatal("expected office:claudeNotification broadcast")
	}
	m := broadcastData(t, b)
	if m["message"] != "needs your input" {
		t.Errorf("message = %v, want 'needs your input'", m["message"])
	}
	// Notification must NOT mutate agent state.
	if reg.state["sess-0001"].activeTool != "" || reg.state["sess-0001"].sdkWorking {
		t.Error("Notification should not mutate agent state")
	}
}

func TestDispatcher_Compact_PreAndPost(t *testing.T) {
	cases := []struct {
		event   string
		trigger string
	}{
		{"PreCompact", "auto"},
		{"PostCompact", "manual"},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			reg := newFakeRegistry("sess-0001")
			d := NewDispatcher(reg, nil)
			rec := fireEvent(t, d, "sess-0001", tc.event, CompactEvent{
				BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: tc.event},
				Trigger:   tc.trigger,
			})
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", rec.Code)
			}
			b, ok := findBroadcast(reg, "office:claudeCompact")
			if !ok {
				t.Fatal("expected office:claudeCompact broadcast")
			}
			m := broadcastData(t, b)
			if m["event"] != tc.event {
				t.Errorf("event field = %v, want %q", m["event"], tc.event)
			}
			if m["trigger"] != tc.trigger {
				t.Errorf("trigger = %v, want %q", m["trigger"], tc.trigger)
			}
		})
	}
}

func TestDispatcher_PostToolBatch_BroadcastsDedupedTools(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PostToolBatch", PostToolBatchEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PostToolBatch"},
		ToolCalls: []ToolCallSummary{
			{ToolName: "Read"},
			{ToolName: "Read"}, // duplicate -> collapsed
			{ToolName: "Bash"},
		},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentBatchCompleted")
	if !ok {
		t.Fatal("expected office:agentBatchCompleted broadcast")
	}
	m := broadcastData(t, b)
	// batchSize counts the raw tool calls (3), not the deduped names.
	if bs, _ := m["batchSize"].(int); bs != 3 {
		t.Errorf("batchSize = %v, want 3", m["batchSize"])
	}
	tools, ok := m["tools"].([]string)
	if !ok {
		t.Fatalf("tools is %T, want []string", m["tools"])
	}
	if len(tools) != 2 {
		t.Errorf("tools = %v, want 2 unique (Read, Bash)", tools)
	}
}

func TestDispatcher_PostToolBatch_EmptyBatch(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PostToolBatch", PostToolBatchEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PostToolBatch"},
		ToolCalls: nil,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentBatchCompleted")
	if !ok {
		t.Fatal("expected broadcast even on empty batch")
	}
	m := broadcastData(t, b)
	if bs, _ := m["batchSize"].(int); bs != 0 {
		t.Errorf("batchSize = %v, want 0", m["batchSize"])
	}
	tools, _ := m["tools"].([]string)
	if len(tools) != 0 {
		t.Errorf("tools = %v, want empty", tools)
	}
}

func TestDispatcher_StopFailure_ClearsWorkingAndBroadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].sdkWorking = true
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "StopFailure", StopFailureEvent{
		BaseEvent:    BaseEvent{SessionID: "sess-0001", HookEventName: "StopFailure"},
		ErrorType:    "rate_limit",
		ErrorMessage: "429 too many requests",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].sdkWorking {
		t.Error("StopFailure should flip sdkWorking=false")
	}
	b, ok := findBroadcast(reg, "office:agentTurnError")
	if !ok {
		t.Fatal("expected office:agentTurnError broadcast")
	}
	m := broadcastData(t, b)
	if m["errorType"] != "rate_limit" {
		t.Errorf("errorType = %v, want rate_limit", m["errorType"])
	}
	if m["errorMessage"] != "429 too many requests" {
		t.Errorf("errorMessage = %v", m["errorMessage"])
	}
}

func TestDispatcher_PermissionRequest_BroadcastsNoDecision(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PermissionRequest", PermissionRequestEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PermissionRequest"},
		ToolName:  "Bash",
		ToolUseID: "toolu_123",
	})
	// No HookResponse => 204, no decision returned (operator must decide).
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (no decision)", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (no auto-decision)", rec.Body.String())
	}
	b, ok := findBroadcast(reg, "office:agentPermissionPrompt")
	if !ok {
		t.Fatal("expected office:agentPermissionPrompt broadcast")
	}
	m := broadcastData(t, b)
	if m["tool"] != "Bash" || m["toolUseId"] != "toolu_123" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_PermissionDenied_BroadcastsAndRequestsRetry(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PermissionDenied", PermissionDeniedEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PermissionDenied"},
		ToolName:  "WebFetch",
		ToolUseID: "toolu_999",
	})
	// PermissionDenied returns a HookResponse with retry=true -> 200 + JSON.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (retry response)", rec.Code)
	}
	var resp HookResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, rec.Body.String())
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput in response")
	}
	if !resp.HookSpecificOutput.Retry {
		t.Error("expected retry=true in PermissionDenied response")
	}
	if resp.HookSpecificOutput.HookEventName != "PermissionDenied" {
		t.Errorf("hookEventName = %q, want PermissionDenied", resp.HookSpecificOutput.HookEventName)
	}
	b, ok := findBroadcast(reg, "office:agentPermissionDenied")
	if !ok {
		t.Fatal("expected office:agentPermissionDenied broadcast")
	}
	m := broadcastData(t, b)
	if m["tool"] != "WebFetch" || m["toolUseId"] != "toolu_999" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_TaskCreated_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "TaskCreated", TaskCreatedEvent{
		BaseEvent:       BaseEvent{SessionID: "sess-0001", HookEventName: "TaskCreated"},
		TaskName:        "refactor auth",
		TaskDescription: "extract JWT verification into middleware",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentInternalTaskCreated")
	if !ok {
		t.Fatal("expected office:agentInternalTaskCreated broadcast")
	}
	m := broadcastData(t, b)
	if m["taskName"] != "refactor auth" {
		t.Errorf("taskName = %v", m["taskName"])
	}
	if m["description"] != "extract JWT verification into middleware" {
		t.Errorf("description = %v", m["description"])
	}
}

func TestDispatcher_TaskCompleted_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "TaskCompleted", TaskCompletedEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "TaskCompleted"},
		TaskName:  "refactor auth",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentInternalTaskCompleted")
	if !ok {
		t.Fatal("expected office:agentInternalTaskCompleted broadcast")
	}
	m := broadcastData(t, b)
	if m["taskName"] != "refactor auth" {
		t.Errorf("taskName = %v", m["taskName"])
	}
}

func TestDispatcher_SubagentStart_BroadcastsAndInvokesObserver(t *testing.T) {
	var gotSession, gotType, gotID string
	var observerCalls int
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	d.SetSubagentObservers(
		func(sessionID, agentType, agentID string) {
			observerCalls++
			gotSession, gotType, gotID = sessionID, agentType, agentID
		},
		nil,
	)
	rec := fireEvent(t, d, "sess-0001", "SubagentStart", SubagentStartEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SubagentStart"},
		AgentType: "code-reviewer",
		AgentID:   "agent-77",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if observerCalls != 1 {
		t.Fatalf("subagentStart observer called %d times, want 1", observerCalls)
	}
	if gotSession != "sess-0001" || gotType != "code-reviewer" || gotID != "agent-77" {
		t.Errorf("observer args: session=%q type=%q id=%q", gotSession, gotType, gotID)
	}
	b, ok := findBroadcast(reg, "subagent:discovered")
	if !ok {
		t.Fatal("expected subagent:discovered broadcast")
	}
	m := broadcastData(t, b)
	if m["agentType"] != "code-reviewer" || m["agentId"] != "agent-77" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_SubagentStart_NilObserver(t *testing.T) {
	// No observer configured: must still broadcast and not panic.
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "SubagentStart", SubagentStartEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SubagentStart"},
		AgentType: "planner",
		AgentID:   "agent-1",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, ok := findBroadcast(reg, "subagent:discovered"); !ok {
		t.Fatal("expected subagent:discovered broadcast even without observer")
	}
}

func TestDispatcher_SubagentStop_BroadcastsAndInvokesObserver(t *testing.T) {
	var gotSession, gotID string
	var observerCalls int
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	d.SetSubagentObservers(
		nil,
		func(sessionID, agentID string) {
			observerCalls++
			gotSession, gotID = sessionID, agentID
		},
	)
	rec := fireEvent(t, d, "sess-0001", "SubagentStop", SubagentStopEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SubagentStop"},
		AgentType: "code-reviewer",
		AgentID:   "agent-77",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if observerCalls != 1 {
		t.Fatalf("subagentStop observer called %d times, want 1", observerCalls)
	}
	if gotSession != "sess-0001" || gotID != "agent-77" {
		t.Errorf("observer args: session=%q id=%q", gotSession, gotID)
	}
	b, ok := findBroadcast(reg, "subagent:completed")
	if !ok {
		t.Fatal("expected subagent:completed broadcast")
	}
	m := broadcastData(t, b)
	if m["agentType"] != "code-reviewer" || m["agentId"] != "agent-77" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_SubagentStop_NilObserver(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "SubagentStop", SubagentStopEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SubagentStop"},
		AgentType: "planner",
		AgentID:   "agent-2",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, ok := findBroadcast(reg, "subagent:completed"); !ok {
		t.Fatal("expected subagent:completed broadcast even without observer")
	}
}

func TestDispatcher_CwdChanged_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "CwdChanged", CwdChangedEvent{
		BaseEvent:   BaseEvent{SessionID: "sess-0001", HookEventName: "CwdChanged"},
		PreviousCwd: "/repo",
		NewCwd:      "/repo/internal",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentCwdChanged")
	if !ok {
		t.Fatal("expected office:agentCwdChanged broadcast")
	}
	m := broadcastData(t, b)
	if m["previous"] != "/repo" || m["new"] != "/repo/internal" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_InstructionsLoaded_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "InstructionsLoaded", InstructionsLoadedEvent{
		BaseEvent:  BaseEvent{SessionID: "sess-0001", HookEventName: "InstructionsLoaded"},
		FilePath:   "/repo/CLAUDE.md",
		MemoryType: "project",
		LoadReason: "startup",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentInstructionsLoaded")
	if !ok {
		t.Fatal("expected office:agentInstructionsLoaded broadcast")
	}
	m := broadcastData(t, b)
	if m["filePath"] != "/repo/CLAUDE.md" || m["memoryType"] != "project" || m["loadReason"] != "startup" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_ConfigChange_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "ConfigChange", ConfigChangeEvent{
		BaseEvent:    BaseEvent{SessionID: "sess-0001", HookEventName: "ConfigChange"},
		ConfigSource: "settings.local.json",
		Changes:      map[string]interface{}{"model": "opus"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:agentConfigChanged")
	if !ok {
		t.Fatal("expected office:agentConfigChanged broadcast")
	}
	m := broadcastData(t, b)
	if m["source"] != "settings.local.json" {
		t.Errorf("source = %v", m["source"])
	}
	changes, ok := m["changes"].(map[string]interface{})
	if !ok || changes["model"] != "opus" {
		t.Errorf("changes = %v", m["changes"])
	}
}

func TestDispatcher_Elicitation_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "Elicitation", ElicitationEvent{
		BaseEvent:     BaseEvent{SessionID: "sess-0001", HookEventName: "Elicitation"},
		ServerName:    "github",
		ToolName:      "create_pr",
		ElicitationID: "elic-1",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:elicitation")
	if !ok {
		t.Fatal("expected office:elicitation broadcast")
	}
	m := broadcastData(t, b)
	if m["serverName"] != "github" || m["tool"] != "create_pr" || m["elicitationId"] != "elic-1" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_ElicitationResult_Broadcasts(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "ElicitationResult", ElicitationResultEvent{
		BaseEvent:     BaseEvent{SessionID: "sess-0001", HookEventName: "ElicitationResult"},
		ServerName:    "github",
		ElicitationID: "elic-1",
		Action:        "accept",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	b, ok := findBroadcast(reg, "office:elicitationResult")
	if !ok {
		t.Fatal("expected office:elicitationResult broadcast")
	}
	m := broadcastData(t, b)
	if m["serverName"] != "github" || m["elicitationId"] != "elic-1" || m["action"] != "accept" {
		t.Errorf("broadcast payload = %v", m)
	}
}

func TestDispatcher_UnknownEvent_AcceptsSilently(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "SomeFutureEvent", map[string]interface{}{
		"session_id": "sess-0001", "hook_event_name": "SomeFutureEvent",
	})
	// Unknown events return nil,nil -> 204 and no broadcasts.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	reg.mu.Lock()
	n := len(reg.broadcasts)
	reg.mu.Unlock()
	if n != 0 {
		t.Errorf("unknown event produced %d broadcasts, want 0", n)
	}
}

// TestSummariseBatchTools_Dedup exercises the helper directly for empty,
// duplicate, and order-preserving cases.
func TestSummariseBatchTools_Dedup(t *testing.T) {
	cases := []struct {
		name  string
		in    []ToolCallSummary
		want  []string
	}{
		{"nil", nil, []string{}},
		{"empty", []ToolCallSummary{}, []string{}},
		{
			"dedup preserves first-seen order",
			[]ToolCallSummary{{ToolName: "Bash"}, {ToolName: "Read"}, {ToolName: "Bash"}, {ToolName: "Edit"}},
			[]string{"Bash", "Read", "Edit"},
		},
		{
			"all identical",
			[]ToolCallSummary{{ToolName: "Grep"}, {ToolName: "Grep"}, {ToolName: "Grep"}},
			[]string{"Grep"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summariseBatchTools(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestTruncate_Boundaries covers the byte-fast-path, the rune-count
// branch, and the multibyte cut-on-boundary path.
func TestTruncate_Boundaries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than n", "hello", 10, "hello"},
		{"exactly n", "hello", 5, "hello"},
		{"longer ascii", "hello world", 5, "hello…"},
		{"empty", "", 5, ""},
		{"zero n empty", "", 0, ""},
		// Multibyte: 6 runes but >6 bytes; n=6 hits the rune-count
		// early-return (len(r) <= n) so the string is returned intact.
		{"multibyte within n", "héllo!", 6, "héllo!"},
		// n smaller than rune count: cut on rune boundary, append ellipsis.
		{"multibyte cut", "héllo!", 3, "hél…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}
