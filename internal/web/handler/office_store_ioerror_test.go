package handler

// Round-2 branch fills for the office.* store IO-error arms and the
// remaining edge branches in snapshot / invariants / standup handlers.
//
// The store helpers (office.ListSnapshots, office.CreateSnapshot,
// office.ListWrapUpReports) all resolve their directory under
// $HOME/.jarvis/office-memory/<sub>. By pointing HOME at a temp dir and
// then planting a *regular file* where the handler expects a directory,
// os.ReadDir / os.MkdirAll return a non-IsNotExist error (ENOTDIR),
// driving the 500 / 400 arms HONESTLY -- no mocks, real syscalls.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// plantFileAt makes the parent of `dir` exist and writes `dir` itself as
// a regular file, so any os.ReadDir(dir) / os.MkdirAll(dir) inside the
// handler fails with ENOTDIR.
func plantFileAt(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestOfficeListSnapshots_ReadDirError500(t *testing.T) {
	ctx := newSnapshotCtx(t)
	home, _ := os.UserHomeDir()
	// snapshots is supposed to be a directory; make it a file.
	plantFileAt(t, filepath.Join(home, ".jarvis", "office-memory", "snapshots"))

	rec := httptest.NewRecorder()
	ctx.OfficeListSnapshots(rec, httptest.NewRequest("GET", "/api/office/snapshots", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when snapshots dir is unreadable, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeCreateSnapshot_BadJSON400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeCreateSnapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeCreateSnapshot_InvalidName400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	body, _ := json.Marshal(map[string]string{"name": "../etc/passwd"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeCreateSnapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsafe snapshot name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeCreateSnapshot_CreateError400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// Register an agent + mark the team spawned so CreateSnapshot gets past
	// its "no team spawned" guard and reaches the MkdirAll, which we sabotage.
	ctx.OfficeManager.RegisterAgent("sess-x", "cfg-x", &office.TeamMemberConfig{ID: "cfg-x", Name: "X"})
	ctx.OfficeManager.SetTeamSpawned(true)
	home, _ := os.UserHomeDir()
	// Plant a file where snapshots/ should be a directory -> os.MkdirAll fails.
	plantFileAt(t, filepath.Join(home, ".jarvis", "office-memory", "snapshots"))

	body, _ := json.Marshal(map[string]string{"name": "valid-name"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeCreateSnapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when snapshot persistence fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestOfficeCreateSnapshot_SDKLookupMissSucceeds exercises the lookup
// closure's `!ok` arm: a registered agent whose session has NO live SDK
// session, so the harvest closure returns (zero, false). The snapshot
// still succeeds; the member just carries no ClaudeSessionID.
func TestOfficeCreateSnapshot_SDKLookupMissSucceeds(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("ghost-sess", "cfg-ghost", &office.TeamMemberConfig{ID: "cfg-ghost", Name: "Ghost"})
	ctx.OfficeManager.SetTeamSpawned(true)
	// Deliberately do NOT register any SDK session for "ghost-sess".

	body, _ := json.Marshal(map[string]string{"name": "no-sdk-snap"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/snapshots", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeCreateSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	loaded, err := office.LoadSnapshot("no-sdk-snap")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(loaded.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(loaded.Members))
	}
	if loaded.Members[0].ClaudeSessionID != "" {
		t.Errorf("expected empty ClaudeSessionID for SDK-less member, got %q", loaded.Members[0].ClaudeSessionID)
	}
}

func TestOfficeListReports_ReadDirError500(t *testing.T) {
	ctx := newSnapshotCtx(t)
	home, _ := os.UserHomeDir()
	// reports/ planted as a file -> office.ListWrapUpReports() ReadDir fails
	// with a non-IsNotExist error -> handler 500 arm.
	plantFileAt(t, filepath.Join(home, ".jarvis", "office-memory", "reports"))

	rec := httptest.NewRecorder()
	ctx.OfficeListReports(rec, httptest.NewRequest("GET", "/api/office/reports", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when reports dir is unreadable, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestOfficePutProjectInvariants_BadJSON400 + NilNormalizes cover the two
// uncovered branches: decodeBody failure (400) and the nil->[] normalize.
func TestOfficePutProjectInvariants_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest(http.MethodPut, "/api/office/project-invariants", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutProjectInvariants(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePutProjectInvariants_NilNormalizesToEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	// Seed a non-empty list first so we can prove the nil body clears it.
	ctx.OfficeManager.SetProjectInvariants([]string{"never push to main"})

	// Body with explicit null invariants -> req.Invariants == nil -> normalized to [].
	req := httptest.NewRequest(http.MethodPut, "/api/office/project-invariants", strings.NewReader(`{"invariants":null}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutProjectInvariants(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Invariants []string `json:"invariants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Invariants == nil {
		t.Fatal("expected non-nil (empty) invariants slice in response")
	}
	if len(resp.Invariants) != 0 {
		t.Errorf("expected invariants cleared to empty, got %v", resp.Invariants)
	}
	// And the manager state reflects the clear.
	if got := ctx.OfficeManager.GetProjectInvariants(); len(got) != 0 {
		t.Errorf("manager invariants not cleared: %v", got)
	}
}

// TestOfficeGetStandup_NoActiveStandup covers the su == nil arm: no
// standup in progress -> handler returns standup:null.
func TestOfficeGetStandup_NoActiveStandup(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandup(rec, httptest.NewRequest("GET", "/api/office/standup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, present := resp["standup"]; !present || v != nil {
		t.Errorf("expected standup=null when no standup active, got %v", resp["standup"])
	}
}
