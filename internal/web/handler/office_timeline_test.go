package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/web/middleware"
	"github.com/claudeman/jarvis-go/internal/web/sse"
)

type stubSessions struct{}

func (stubSessions) WriteViaScreen(sessionID, input string) bool  { return true }
func (stubSessions) WriteInkPrompt(sessionID, prompt string) bool { return true }
func (stubSessions) GetSessionIDs() []string                      { return nil }
func (stubSessions) IsSessionAlive(sessionID string) bool         { return true }

func newTimelineContext(t *testing.T) *Context {
	t.Helper()
	// Isolate message/standup persistence from the host machine. office.Manager
	// writes to ~/.jarvis/office-memory; overriding HOME routes it to a temp dir.
	t.Setenv("HOME", t.TempDir())

	// SendMessage kicks off an async persistMessages goroutine. If the test exits
	// before it finishes, the t.TempDir cleanup races with the write -- register
	// a cleanup that runs BEFORE the tempdir removal (LIFO order) so the async
	// goroutine has time to settle.
	t.Cleanup(func() { time.Sleep(150 * time.Millisecond) })

	mgr := office.NewManager(stubSessions{}, func(event string, data interface{}) {}, nil)

	ctx := &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
	return ctx
}

func decodeTimeline(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestOfficeTimeline_FilterByType(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager

	// Register two agents so task entries have a label source.
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	mgr.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	// Two messages (one cross-agent, one to team) -> 2 timeline entries of kind=message.
	mgr.SendMessage("s-vex", "s-bolt", "hello")
	mgr.SendMessage("s-bolt", "", "team update")

	req := httptest.NewRequest("GET", "/api/office/timeline?type=message&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)

	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("expected 2 message entries, got %d", len(entries))
	}
	for _, e := range entries {
		m := e.(map[string]interface{})
		if m["kind"] != "message" {
			t.Errorf("expected kind=message, got %v", m["kind"])
		}
	}
}

func TestOfficeTimeline_FilterByAgent(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	mgr.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	mgr.SendMessage("s-vex", "s-bolt", "for you")
	mgr.SendMessage("s-bolt", "s-vex", "thanks")

	req := httptest.NewRequest("GET", "/api/office/timeline?agent=s-vex&type=message", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)

	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	// Only the message sent BY s-vex should match; the handler filters on AgentID which
	// is set to the sender's session ID.
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for agent s-vex, got %d", len(entries))
	}
	m := entries[0].(map[string]interface{})
	if m["agentId"] != "s-vex" {
		t.Errorf("expected agentId s-vex, got %v", m["agentId"])
	}
}

func TestOfficeTimeline_PaginationAndSortDesc(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// Send 5 messages from the same agent so we get 5 deterministic entries.
	for i := 0; i < 5; i++ {
		mgr.SendMessage("s-vex", "", "msg")
	}

	// Page 1: first 2 entries.
	req := httptest.NewRequest("GET", "/api/office/timeline?type=message&limit=2&offset=0", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)
	resp := decodeTimeline(t, rec)

	total, _ := resp["total"].(float64)
	if int(total) != 5 {
		t.Errorf("expected total=5, got %v", total)
	}
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries in page, got %d", len(entries))
	}
	hasMore, _ := resp["hasMore"].(bool)
	if !hasMore {
		t.Error("expected hasMore=true")
	}

	// Entries should be sorted descending by timestamp -- verify first is >= second.
	e0 := entries[0].(map[string]interface{})
	e1 := entries[1].(map[string]interface{})
	if e0["timestamp"].(string) < e1["timestamp"].(string) {
		t.Errorf("expected descending order; got %v then %v", e0["timestamp"], e1["timestamp"])
	}

	// Page 2: next 2.
	req2 := httptest.NewRequest("GET", "/api/office/timeline?type=message&limit=2&offset=2", nil)
	rec2 := httptest.NewRecorder()
	ctx.OfficeTimeline(rec2, req2)
	resp2 := decodeTimeline(t, rec2)
	entries2, _ := resp2["entries"].([]interface{})
	if len(entries2) != 2 {
		t.Fatalf("expected 2 entries on page 2, got %d", len(entries2))
	}

	// Past last page.
	req3 := httptest.NewRequest("GET", "/api/office/timeline?type=message&limit=2&offset=10", nil)
	rec3 := httptest.NewRecorder()
	ctx.OfficeTimeline(rec3, req3)
	resp3 := decodeTimeline(t, rec3)
	entries3, _ := resp3["entries"].([]interface{})
	if len(entries3) != 0 {
		t.Errorf("expected 0 entries past end, got %d", len(entries3))
	}
	if hm, _ := resp3["hasMore"].(bool); hm {
		t.Error("expected hasMore=false past end")
	}
}
