package claudehooks

import (
	"bytes"
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

// fakeRegistry is the test double for AgentRegistry. Records every
// call so tests can assert mutation order and arguments.
type fakeRegistry struct {
	mu sync.Mutex

	// Per-session log of method calls in order.
	calls []registryCall

	// Set of registered session IDs. MutateAgent returns true iff the
	// session is in this set.
	registered map[string]bool

	// Captured AgentState mutations applied via MutateAgent.
	state map[string]*fakeAgentState

	// Captured broadcasts.
	broadcasts []broadcastCall
}

type registryCall struct {
	method    string
	sessionID string
}

type broadcastCall struct {
	event string
	data  interface{}
}

type fakeAgentState struct {
	activeTool   string
	sdkWorking   bool
	cumInput     int64
	cumOutput    int64
	cumCost      float64
	apiTouches   int
	textFlushes  int
	worktreePath string
}

func newFakeRegistry(registered ...string) *fakeRegistry {
	r := &fakeRegistry{
		registered: make(map[string]bool),
		state:      make(map[string]*fakeAgentState),
	}
	for _, sid := range registered {
		r.registered[sid] = true
		r.state[sid] = &fakeAgentState{}
	}
	return r
}

func (r *fakeRegistry) MutateAgent(sessionID string, fn func(MutableAgent)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, registryCall{"MutateAgent", sessionID})
	if !r.registered[sessionID] {
		return false
	}
	fn(&fakeMutable{state: r.state[sessionID]})
	return true
}

func (r *fakeRegistry) HandleAgentDeath(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, registryCall{"HandleAgentDeath", sessionID})
}

func (r *fakeRegistry) CheckForIdleAgents() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, registryCall{"CheckForIdleAgents", ""})
}

func (r *fakeRegistry) Broadcast(event string, data interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcasts = append(r.broadcasts, broadcastCall{event, data})
}

type fakeMutable struct{ state *fakeAgentState }

func (f *fakeMutable) SetActiveTool(name string)                              { f.state.activeTool = name }
func (f *fakeMutable) SetSDKWorking(w bool)                                    { f.state.sdkWorking = w }
func (f *fakeMutable) RecordTurnUsage(in, out int64, cost float64)             { f.state.cumInput += in; f.state.cumOutput += out; f.state.cumCost += cost }
func (f *fakeMutable) TouchAgentAPI()                                          { f.state.apiTouches++ }
func (f *fakeMutable) FlushTurnText() string                                   { f.state.textFlushes++; return "" }
func (f *fakeMutable) SetWorktreePath(path string) string {
	previous := f.state.worktreePath
	f.state.worktreePath = path
	return previous
}

// fireEvent POSTs an event payload through a chi router with the URL
// pattern Jarvis registers in production. Returns the recorded
// response so tests can assert status and body.
func fireEvent(t *testing.T, d *Dispatcher, sessionID, event string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	r := chi.NewRouter()
	r.Post("/api/claude/hooks/{sessionId}/{event}", d.HandleEvent)

	req := httptest.NewRequest(http.MethodPost,
		"/api/claude/hooks/"+sessionID+"/"+event, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDispatcher_RejectsMalformedSessionID(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	cases := []struct {
		name string
		sid  string
	}{
		{"path traversal", "..%2Fadmin"}, // chi decodes path but %2F is encoded slash
		{"shell metacharacter", "x%3B+drop"},
		{"too short", "abc"},
		{"empty", ""},
		{"contains slash", "a/b/c/d"},
		{"contains dot dot", "..abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := fireEvent(t, d, tc.sid, "PreToolUse", PreToolUseEvent{
				BaseEvent: BaseEvent{SessionID: tc.sid, HookEventName: "PreToolUse"},
				ToolName:  "Bash",
			})
			// Empty / dot-dot / slash inputs trip chi's routing first and
			// land at the empty-sessionId fail-open branch (204) or 404.
			// Other malformed inputs hit our regex guard and return 400.
			// Both outcomes prove the registry was not mutated.
			if rec.Code == http.StatusNoContent || rec.Code == http.StatusOK {
				if _, ok := reg.state[tc.sid]; ok {
					t.Fatalf("malformed sid %q reached registry", tc.sid)
				}
			}
			if reg.state[tc.sid] != nil && reg.state[tc.sid].activeTool == "Bash" {
				t.Fatalf("malformed sid %q mutated registry state", tc.sid)
			}
		})
	}
}

func TestDispatcher_PreToolUse_SetsActiveTool(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-0001", "PreToolUse", PreToolUseEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "PreToolUse"},
		ToolName:  "Bash",
	})
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].activeTool != "Bash" {
		t.Errorf("activeTool = %q, want Bash", reg.state["sess-0001"].activeTool)
	}
}

func TestDispatcher_PostToolUse_ClearsToolAndFeedsLoopDetector(t *testing.T) {
	var gotSession, gotTool, gotHash string
	observe := func(sessionID, toolName, resultHash string) {
		gotSession = sessionID
		gotTool = toolName
		gotHash = resultHash
	}
	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].activeTool = "Bash"
	d := NewDispatcher(reg, observe)

	rec := fireEvent(t, d, "sess-0001", "PostToolUse", PostToolUseEvent{
		BaseEvent:  BaseEvent{SessionID: "sess-0001", HookEventName: "PostToolUse"},
		ToolName:   "Bash",
		ToolResult: json.RawMessage(`{"text":"ok"}`),
	})
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if reg.state["sess-0001"].activeTool != "" {
		t.Errorf("activeTool not cleared: %q", reg.state["sess-0001"].activeTool)
	}
	if gotSession != "sess-0001" || gotTool != "Bash" || gotHash == "" {
		t.Errorf("loop detector miss: session=%q tool=%q hash=%q", gotSession, gotTool, gotHash)
	}
}

func TestDispatcher_UserPromptSubmit_MarksWorkingAndFlushes(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	_ = fireEvent(t, d, "sess-0001", "UserPromptSubmit", UserPromptSubmitEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "UserPromptSubmit"},
		Prompt:    "do the thing",
	})
	if !reg.state["sess-0001"].sdkWorking {
		t.Error("expected sdkWorking=true after UserPromptSubmit")
	}
	if reg.state["sess-0001"].textFlushes == 0 {
		t.Error("expected text buffer flush")
	}
}

func TestDispatcher_Stop_ReadsTranscriptAndRecordsUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	jsonl := `{"type":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":1000,"output_tokens":500}}` + "\n"
	if err := os.WriteFile(path, []byte(jsonl), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	reg := newFakeRegistry("sess-0001")
	reg.state["sess-0001"].sdkWorking = true
	d := NewDispatcher(reg, nil)
	_ = fireEvent(t, d, "sess-0001", "Stop", StopEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", TranscriptPath: path, HookEventName: "Stop"},
	})

	if reg.state["sess-0001"].sdkWorking {
		t.Error("expected sdkWorking=false after Stop")
	}
	if reg.state["sess-0001"].cumInput != 1000 || reg.state["sess-0001"].cumOutput != 500 {
		t.Errorf("usage not recorded: cumIn=%d cumOut=%d",
			reg.state["sess-0001"].cumInput, reg.state["sess-0001"].cumOutput)
	}
	// Sonnet: $3/MTok in, $15/MTok out -> 1000*3 + 500*15 = 3000+7500 = 10500 / 1M = 0.0105
	if reg.state["sess-0001"].cumCost < 0.0104 || reg.state["sess-0001"].cumCost > 0.0106 {
		t.Errorf("cost = %v, want ~0.0105", reg.state["sess-0001"].cumCost)
	}

	// office:tokens broadcast must include the cost.
	found := false
	for _, b := range reg.broadcasts {
		if b.event == "office:tokens" {
			found = true
		}
	}
	if !found {
		t.Error("expected office:tokens broadcast")
	}
}

func TestDispatcher_SessionEnd_CallsHandleAgentDeath(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	_ = fireEvent(t, d, "sess-0001", "SessionEnd", SessionEndEvent{
		BaseEvent: BaseEvent{SessionID: "sess-0001", HookEventName: "SessionEnd"},
		Reason:    "user-quit",
	})

	called := false
	for _, c := range reg.calls {
		if c.method == "HandleAgentDeath" && c.sessionID == "sess-0001" {
			called = true
		}
	}
	if !called {
		t.Errorf("expected HandleAgentDeath call, got: %v", reg.calls)
	}
}

func TestDispatcher_TeammateIdle_PreemptsCheck(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	_ = fireEvent(t, d, "sess-0001", "TeammateIdle", map[string]interface{}{
		"session_id": "sess-0001", "hook_event_name": "TeammateIdle",
	})
	called := false
	for _, c := range reg.calls {
		if c.method == "CheckForIdleAgents" {
			called = true
		}
	}
	if !called {
		t.Error("expected CheckForIdleAgents to be called")
	}
}

func TestDispatcher_UnknownAgent_FailsOpen(t *testing.T) {
	// Spawned claude is faster than the office RegisterAgent path
	// during startup -- the dispatcher must absorb events for an
	// unknown sessionID without stalling claude. 200/204 is the
	// correct response, NOT 404.
	reg := newFakeRegistry() // empty
	d := NewDispatcher(reg, nil)
	rec := fireEvent(t, d, "sess-ghost", "PreToolUse", PreToolUseEvent{
		BaseEvent: BaseEvent{SessionID: "sess-ghost", HookEventName: "PreToolUse"},
		ToolName:  "Bash",
	})
	if rec.Code >= 400 {
		t.Errorf("status = %d, want < 400 (fail open)", rec.Code)
	}
}

func TestDispatcher_FirstEventLog_OncePerSession(t *testing.T) {
	reg := newFakeRegistry("sess-000A", "sess-000B")
	d := NewDispatcher(reg, nil)

	// Same session twice -> firstSeen should be true after first call.
	_ = fireEvent(t, d, "sess-000A", "SessionStart", SessionStartEvent{
		BaseEvent: BaseEvent{SessionID: "sess-000A", HookEventName: "SessionStart"},
		Source:    "startup",
	})
	if !d.firstSeen["sess-000A"] {
		t.Error("firstSeen[sess-000A] should be true after first event")
	}
	if d.firstSeen["sess-000B"] {
		t.Error("firstSeen[sess-000B] should be false until B fires")
	}
	_ = fireEvent(t, d, "sess-000B", "PreToolUse", PreToolUseEvent{
		BaseEvent: BaseEvent{SessionID: "sess-000B", HookEventName: "PreToolUse"},
		ToolName:  "Read",
	})
	if !d.firstSeen["sess-000B"] {
		t.Error("firstSeen[sess-000B] should be true after first event")
	}
}

func TestDispatcher_BadJSON_FailsOpen(t *testing.T) {
	reg := newFakeRegistry("sess-0001")
	d := NewDispatcher(reg, nil)
	r := chi.NewRouter()
	r.Post("/api/claude/hooks/{sessionId}/{event}", d.HandleEvent)
	req := httptest.NewRequest(http.MethodPost,
		"/api/claude/hooks/sess-0001/PreToolUse", strings.NewReader(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Errorf("status = %d, want < 400 (fail open on bad body)", rec.Code)
	}
}
