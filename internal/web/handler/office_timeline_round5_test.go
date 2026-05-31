package handler

// Round 5 -- listPersistedStandups: the os.ReadFile-error skip arm. An
// unreadable *.json entry under the standups dir must be silently skipped
// rather than aborting the whole listing.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestCollectTimelineEntries_AuditEmptyNameLooksUpByID(t *testing.T) {
	ctx := newTimelineContext(t)
	// Register an agent whose Config.Name is empty. The memberJoined audit
	// entry is then logged with an empty AgentName, forcing collectTimelineEntries
	// to fall back to the sessionID -> name map (which is ALSO empty here, but
	// the fallback branch still executes).
	ctx.OfficeManager.RegisterAgent("sess-empty", "cfg-empty", &office.TeamMemberConfig{Name: ""})

	req := httptest.NewRequest("GET", "/api/office/timeline?type=audit&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	// The memberJoined audit entry for our empty-named agent should be present.
	found := false
	for _, e := range entries {
		row, _ := e.(map[string]interface{})
		if row["agentId"] == "sess-empty" && row["kind"] == "audit" {
			found = true
		}
	}
	if !found {
		t.Error("expected the empty-name agent's audit entry in the timeline")
	}
}

func TestListPersistedStandups_SkipsUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 read semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permission checks")
	}
	ctx := newTimelineContext(t) // isolates HOME to a temp dir
	_ = ctx

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A readable, valid standup that SHOULD surface.
	good := office.PersistedStandup{ID: "good", CompletedAt: time.Now().UnixMilli()}
	writePersistedStandup(t, good)

	// An unreadable .json that must be skipped without aborting the listing.
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{"id":"bad"}`), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o644) })

	got := listPersistedStandups()
	// The good file surfaces; the unreadable one is skipped.
	foundGood := false
	for _, s := range got {
		if s.ID == "good" {
			foundGood = true
		}
		if s.ID == "bad" {
			t.Error("unreadable standup should have been skipped, but appeared")
		}
	}
	if !foundGood {
		t.Error("expected the readable standup to surface")
	}
}
