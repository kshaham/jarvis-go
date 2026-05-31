package handler

// Round-3 branch fills for collectTimelineEntries (office_timeline.go) and the
// labels add/remove error arms (labels.go).
//
// Timeline: a fully-transitioned task (AssignedAt + StartedAt + CompletedAt all
// set) produces three task entries; a message with empty From/To names exercises
// the nameByID fallback; a `since` query filters out an older entry. A persisted
// standup file exercises the standup loop + listPersistedStandups read path.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestCollectTimelineEntries_TaskTransitionsMessagesStandups(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager

	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	mgr.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	now := time.Now()
	// A task with all three transition timestamps -> 3 task timeline entries.
	mgr.GetAgent("s-vex").AddTask(&office.Task{
		ID:          "t-full",
		Title:       "wire the pipeline",
		Status:      office.TaskCompleted,
		AssignedAt:  now.Add(-3 * time.Minute),
		StartedAt:   now.Add(-2 * time.Minute),
		CompletedAt: now.Add(-1 * time.Minute),
	})

	// Message between agents: From/To carry session IDs; FromName/ToName are
	// empty so the nameByID fallback (195, 207 region) runs.
	mgr.SendMessage("s-vex", "s-bolt", "ping")
	// Team broadcast (empty To) -> "sender → team" title arm.
	mgr.SendMessage("s-bolt", "", "team note")

	// Persist a standup file so listPersistedStandups + the standup loop run.
	homeDir, _ := os.UserHomeDir()
	standupDir := filepath.Join(homeDir, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(standupDir, 0o755); err != nil {
		t.Fatalf("mkdir standups: %v", err)
	}
	standupJSON := `{"id":"su-1","completedAt":` +
		itoaMillis(now.Add(-30*time.Second)) +
		`,"participants":["Vex","Bolt"],"participantCount":2,"respondedCount":2,"timedOutCount":0}`
	if err := os.WriteFile(filepath.Join(standupDir, "su-1.json"), []byte(standupJSON), 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}

	entries := ctx.collectTimelineEntries()

	var taskCount, msgCount, standupCount int
	for _, e := range entries {
		switch e.Kind {
		case "task":
			taskCount++
		case "message":
			msgCount++
		case "standup":
			standupCount++
		}
	}
	if taskCount != 3 {
		t.Errorf("expected 3 task entries (assigned/started/completed), got %d", taskCount)
	}
	if msgCount != 2 {
		t.Errorf("expected 2 message entries, got %d", msgCount)
	}
	if standupCount != 1 {
		t.Errorf("expected 1 standup entry, got %d", standupCount)
	}
}

// itoaMillis renders t.UnixMilli() as a JSON number string.
func itoaMillis(t time.Time) string {
	ms := t.UnixMilli()
	// strconv via fmt-free path
	digits := []byte{}
	neg := ms < 0
	if neg {
		ms = -ms
	}
	if ms == 0 {
		digits = append(digits, '0')
	}
	for ms > 0 {
		digits = append([]byte{byte('0' + ms%10)}, digits...)
		ms /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

// TestOfficeTimeline_SinceFilter exercises the `since` query parameter arm
// (office_timeline.go:67-70, 84-85): an entry older than `since` is filtered out.
func TestOfficeTimeline_SinceFilter(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// One old message (before the since cutoff) and one fresh one.
	mgr.SendMessage("s-vex", "", "old then new")
	cutoff := time.Now().Add(500 * time.Millisecond)
	time.Sleep(600 * time.Millisecond)
	mgr.SendMessage("s-vex", "", "after cutoff")

	req := httptest.NewRequest("GET",
		"/api/office/timeline?type=message&since="+itoaMillis(cutoff), nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)
	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 message after the since cutoff, got %d", len(entries))
	}
}

// TestOfficeAgentTaskLabels_RemoveUnknownTask drives the RemoveLabel error arm
// (labels.go:76-79): a remove against a taskID the agent doesn't own -> 400.
func TestOfficeAgentTaskLabels_RemoveUnknownTask(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "ghost-task", "remove": []string{"auth"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 removing label from unknown task, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestOfficeAgentTaskLabels_AddUnknownTask drives the AddLabel error arm
// (labels.go:82-85): a valid label added to a taskID the agent doesn't own -> 400.
func TestOfficeAgentTaskLabels_AddUnknownTask(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "ghost-task", "add": []string{"auth"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 adding label to unknown task, got %d: %s", rec.Code, rec.Body.String())
	}
}
