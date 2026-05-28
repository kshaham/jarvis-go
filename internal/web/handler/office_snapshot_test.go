package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
	"github.com/go-chi/chi/v5"
)

// newSnapshotCtx builds a Context isolated against the host's
// ~/.jarvis/office-memory dir so snapshot files land in t.TempDir().
func newSnapshotCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
}

// seedSnapshotFile writes a snapshot directly to disk so we can test
// the restore handler without running the full spawn pipeline.
func seedSnapshotFile(t *testing.T, snap *office.TeamSnapshot) {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".jarvis", "office-memory", "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, snap.Name+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// restoreReq fires the restore handler with the right chi URL params.
func restoreReq(t *testing.T, ctx *Context, name, query string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/office/snapshots/" + name + "/restore"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeRestoreSnapshot(rec, req)
	return rec
}

func TestOfficeRestoreSnapshot_RejectsWhenTeamSpawned(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	rec := restoreReq(t, ctx, "anything", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when team already spawned, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeRestoreSnapshot_RejectsInvalidName(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// "/" would be a path-traversal risk; the validator strips it.
	rec := restoreReq(t, ctx, "../../etc/passwd", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name, got %d", rec.Code)
	}
}

func TestOfficeRestoreSnapshot_RejectsMissingWorkingDir(t *testing.T) {
	ctx := newSnapshotCtx(t)
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:    "no-wd",
		Members: []office.SnapshotMemberEntry{{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}}},
	})
	rec := restoreReq(t, ctx, "no-wd", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for snapshot without workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeRestoreSnapshot_RejectsMissingMembers(t *testing.T) {
	ctx := newSnapshotCtx(t)
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:       "no-members",
		WorkingDir: "/tmp",
	})
	rec := restoreReq(t, ctx, "no-members", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for snapshot with zero members, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeRestoreSnapshot_MalformedForkQueryReturns400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:       "ok",
		WorkingDir: "/tmp",
		Members:    []office.SnapshotMemberEntry{{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}}},
	})
	rec := restoreReq(t, ctx, "ok", "fork=maybe")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed fork query, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForkApplied_TableDriven(t *testing.T) {
	cases := []struct {
		name      string
		requested bool
		applied   int
		want      bool
	}{
		{"not requested, none applied", false, 0, false},
		{"not requested, some applied (shouldn't happen)", false, 3, false},
		{"requested, none applied (snapshot too old)", true, 0, false},
		{"requested, some applied", true, 2, true},
		{"requested, all applied", true, 7, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forkApplied(tc.requested, tc.applied); got != tc.want {
				t.Errorf("forkApplied(%v, %d) = %v, want %v", tc.requested, tc.applied, got, tc.want)
			}
		})
	}
}

func TestOfficeCreateSnapshot_HarvestsClaudeSessionIDs(t *testing.T) {
	ctx := newSnapshotCtx(t)

	// Register an agent + its live SDK session. The SDK session is a
	// shell; we seed its ClaudeSessionID directly via the
	// test-only seam so the harvest loop has something to capture.
	ctx.OfficeManager.RegisterAgent("sess-vex", "cfg-vex", &office.TeamMemberConfig{ID: "cfg-vex", Name: "Vex"})
	ctx.OfficeManager.SetTeamSpawned(true)

	sdk := pty.NewSDKSession(pty.SessionConfig{
		ID:         "sess-vex",
		WorkingDir: "/tmp/team-workdir",
		Mode:       pty.ModeClaudeSDK,
		Name:       "Vex",
	})
	sdk.SetClaudeSessionIDForTest("claude-sid-vex")
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	// Drive the handler.
	body, _ := json.Marshal(map[string]string{"name": "harvest-snap"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeCreateSnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Re-load the persisted snapshot and assert the member entry
	// carries the harvested ClaudeSessionID.
	loaded, err := office.LoadSnapshot("harvest-snap")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(loaded.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(loaded.Members))
	}
	if got := loaded.Members[0].ClaudeSessionID; got != "claude-sid-vex" {
		t.Errorf("ClaudeSessionID=%q, want claude-sid-vex", got)
	}
	if loaded.WorkingDir != "/tmp/team-workdir" {
		t.Errorf("WorkingDir=%q, want /tmp/team-workdir", loaded.WorkingDir)
	}
}

func TestBuildRestoreOverrides_NoForkLeavesResumeEmpty(t *testing.T) {
	snap := &office.TeamSnapshot{
		Members: []office.SnapshotMemberEntry{
			{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}, ClaudeSessionID: "claude-sid-1"},
			{ConfigID: "cfg-2", Config: office.TeamMemberConfig{Name: "Bolt"}, ClaudeSessionID: "claude-sid-2"},
		},
	}
	got, count := buildRestoreOverrides(snap, false)
	if count != 0 {
		t.Errorf("expected forkAppliedCount=0 when fork not requested, got %d", count)
	}
	for i, ov := range got {
		if ov.ResumeClaudeSessionID != "" {
			t.Errorf("override[%d] ResumeClaudeSessionID=%q, want empty", i, ov.ResumeClaudeSessionID)
		}
		if ov.ForkResumedSession {
			t.Errorf("override[%d] ForkResumedSession=true, want false", i)
		}
	}
}

func TestBuildRestoreOverrides_ForkPopulatesEveryMemberWithCSID(t *testing.T) {
	snap := &office.TeamSnapshot{
		Members: []office.SnapshotMemberEntry{
			{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}, ClaudeSessionID: "claude-sid-1"},
			{ConfigID: "cfg-2", Config: office.TeamMemberConfig{Name: "Bolt"}, ClaudeSessionID: "claude-sid-2"},
		},
	}
	got, count := buildRestoreOverrides(snap, true)
	if count != 2 {
		t.Errorf("expected forkAppliedCount=2, got %d", count)
	}
	for i, ov := range got {
		if !ov.ForkResumedSession {
			t.Errorf("override[%d] ForkResumedSession should be true under fork", i)
		}
		if ov.ResumeClaudeSessionID == "" {
			t.Errorf("override[%d] ResumeClaudeSessionID should be populated", i)
		}
	}
}

func TestBuildRestoreOverrides_MixedSnapshotOnlyForksMembersWithCSID(t *testing.T) {
	// A snapshot from before R3.5 may have some members without
	// ClaudeSessionID. Fork mode should fork the ones it can and
	// leave the rest as fresh-session overrides.
	snap := &office.TeamSnapshot{
		Members: []office.SnapshotMemberEntry{
			{ConfigID: "cfg-1", Config: office.TeamMemberConfig{Name: "Ada"}, ClaudeSessionID: "claude-sid-1"},
			{ConfigID: "cfg-2", Config: office.TeamMemberConfig{Name: "Bolt"} /* no ClaudeSessionID */},
			{ConfigID: "cfg-3", Config: office.TeamMemberConfig{Name: "Vex"}, ClaudeSessionID: "claude-sid-3"},
		},
	}
	got, count := buildRestoreOverrides(snap, true)
	if count != 2 {
		t.Errorf("expected forkAppliedCount=2, got %d", count)
	}
	// Member 0 (cfg-1): forked.
	if !got[0].ForkResumedSession || got[0].ResumeClaudeSessionID != "claude-sid-1" {
		t.Errorf("cfg-1 override should be forked: %+v", got[0])
	}
	// Member 1 (cfg-2): no CSID, fresh.
	if got[1].ForkResumedSession || got[1].ResumeClaudeSessionID != "" {
		t.Errorf("cfg-2 override should be fresh: %+v", got[1])
	}
	// Member 2 (cfg-3): forked.
	if !got[2].ForkResumedSession || got[2].ResumeClaudeSessionID != "claude-sid-3" {
		t.Errorf("cfg-3 override should be forked: %+v", got[2])
	}
}

func TestBuildRestoreOverrides_CopiesConfigFields(t *testing.T) {
	snap := &office.TeamSnapshot{
		Members: []office.SnapshotMemberEntry{
			{
				ConfigID: "cfg-1",
				Config: office.TeamMemberConfig{
					Name:         "Ada",
					Role:         "scout",
					Tier:         "worker",
					Color:        "#aabbcc",
					Personality:  "curious",
					SystemPrompt: "you are Ada",
					Skills:       []string{"go", "react"},
				},
			},
		},
	}
	got, _ := buildRestoreOverrides(snap, false)
	if len(got) != 1 {
		t.Fatalf("expected 1 override, got %d", len(got))
	}
	ov := got[0]
	if ov.Name != "Ada" || ov.Role != "scout" || ov.Tier != "worker" || ov.Color != "#aabbcc" {
		t.Errorf("config fields not copied: %+v", ov)
	}
	if ov.SystemPrompt != "you are Ada" || ov.Personality != "curious" {
		t.Errorf("personality/prompt not copied: %+v", ov)
	}
	if len(ov.Skills) != 2 {
		t.Errorf("skills not copied: %v", ov.Skills)
	}
}

func TestSnapshotMemberEntry_RoundTripsClaudeSessionID(t *testing.T) {
	// Confirms the new ClaudeSessionID field survives JSON round-trip.
	// Regression guard for the fork-restore happy path — if json tag
	// drift drops the field, fork restore silently falls back to fresh.
	orig := office.SnapshotMemberEntry{
		ConfigID:        "cfg-9",
		Config:          office.TeamMemberConfig{Name: "Vex"},
		ClaudeSessionID: "claude-session-deadbeef",
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got office.SnapshotMemberEntry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ClaudeSessionID != orig.ClaudeSessionID {
		t.Errorf("ClaudeSessionID lost: got %q, want %q", got.ClaudeSessionID, orig.ClaudeSessionID)
	}
}
