package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

type stubSessions struct{}

func (stubSessions) WriteViaScreen(sessionID, input string) bool  { return true }
func (stubSessions) WriteInkPrompt(sessionID, prompt string) bool { return true }
func (stubSessions) GetSessionIDs() []string                      { return nil }
func (stubSessions) IsSessionAlive(sessionID string) bool         { return true }
func (stubSessions) IsSessionWorking(sessionID string) bool       { return false }
func (stubSessions) StopSession(sessionID string) error           { return nil }

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

// TestOfficeTimeline_WithTask verifies that a task assigned to an agent
// appears in the timeline as a "task" kind entry. This also exercises the
// GetTasksCopy path (the race fix) — run with -race to catch regressions.
func TestOfficeTimeline_WithTask(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	agent := mgr.GetAgent("s-vex")
	agent.AddTask(&office.Task{
		ID:         "t1",
		Title:      "Build the thing",
		Status:     office.TaskPending,
		AssignedAt: time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/office/timeline?type=task", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)

	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) == 0 {
		t.Fatal("expected at least one task entry in timeline")
	}
	e := entries[0].(map[string]interface{})
	if e["kind"] != "task" {
		t.Errorf("entry kind=%v want task", e["kind"])
	}
	if e["agentId"] != "s-vex" {
		t.Errorf("agentId=%v want s-vex", e["agentId"])
	}
}

// TestOfficeTimeline_WithTask_RaceFree verifies that concurrent task status
// mutations don't race with timeline collection. The test is a no-op without
// -race; with -race it catches data races on task field reads.
func TestOfficeTimeline_WithTask_RaceFree(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := mgr.GetAgent("s-vex")
	agent.AddTask(&office.Task{
		ID:         "t-race",
		Title:      "concurrent task",
		Status:     office.TaskPending,
		AssignedAt: time.Now(),
	})

	var wg sync.WaitGroup
	// Writer goroutine: cycle through task statuses.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			agent.UpdateTaskStatus("t-race", office.TaskInProgress)
			agent.UpdateTaskStatus("t-race", office.TaskBlocked)
			agent.UpdateTaskStatus("t-race", office.TaskPending)
		}
	}()
	// Reader goroutine: collect timeline entries while writer is mutating.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			req := httptest.NewRequest("GET", "/api/office/timeline?type=task", nil)
			rec := httptest.NewRecorder()
			ctx.OfficeTimeline(rec, req)
		}
	}()
	wg.Wait()
}

// TestListPersistedStandups_Cap verifies that listPersistedStandups only
// parses at most listPersistedStandupsMax files even when more exist on disk.
func TestListPersistedStandups_Cap(t *testing.T) {
	// Override HOME so standup files land in a temp dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	standupDir := filepath.Join(tmpHome, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(standupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed cap+10 standup JSON files.
	total := listPersistedStandupsMax + 10
	for i := 0; i < total; i++ {
		content := fmt.Sprintf(`{"id":"s%d","completedAt":%d,"participantCount":1,"respondedCount":1,"timedOutCount":0,"participants":["agent"]}`,
			i, time.Now().UnixMilli()+int64(i))
		path := filepath.Join(standupDir, fmt.Sprintf("standup-%04d.json", i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := listPersistedStandups()
	if len(got) > listPersistedStandupsMax {
		t.Errorf("listPersistedStandups returned %d entries, want at most %d", len(got), listPersistedStandupsMax)
	}
	if len(got) == 0 {
		t.Error("listPersistedStandups returned 0 entries; expected some")
	}
}
