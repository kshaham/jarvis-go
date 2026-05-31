package handler

// Round-3 batch branch fills across office.go GET/POST handlers:
//   - OfficeGetTaskQueues / OfficeAgentTeamRoster: the populated-roster loop body
//   - OfficeAgentMessage / OfficeAgentAsk / OfficeBrainstorm: decode-fail 400
//   - OfficeAgentSearchSkills: the no-skills-dir ReadDir-error arm
//   - OfficeHealth: the uptime arm (teamSpawnedAt > 0)
//   - allTeamTemplates: the custom-template loop body
//   - OfficeGetStandupByID: read-error + JSON-unmarshal-fallback arms
//   - OfficeAgentPostMemorySection / OfficeAgentGetMemorySection: happy write +
//     append + read paths

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

func badJSONReq(path string) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestOfficeRosterAndQueues_PopulatedLoopBodies(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Role: "scout", Tier: "worker"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t1", Title: "do it", Status: office.TaskPending})

	roster := getJSON(t, ctx.OfficeAgentTeamRoster, "/api/office/agent/roster")
	if r, _ := roster["roster"].([]interface{}); len(r) != 1 {
		t.Errorf("expected 1 roster entry, got %v", roster["roster"])
	}
	queues := getJSON(t, ctx.OfficeGetTaskQueues, "/api/office/task-queues")
	if q, _ := queues["queues"].([]interface{}); len(q) != 1 {
		t.Errorf("expected 1 task queue, got %v", queues["queues"])
	}
}

func TestOfficePostHandlers_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"OfficeAgentMessage": ctx.OfficeAgentMessage,
		"OfficeAgentAsk":     ctx.OfficeAgentAsk,
		"OfficeBrainstorm":   ctx.OfficeBrainstorm,
	} {
		rec := httptest.NewRecorder()
		h(rec, badJSONReq("/api/office/x"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 on malformed body, got %d", name, rec.Code)
		}
	}
}

func TestOfficeAgentSearchSkills_NoSkillsDir(t *testing.T) {
	// newTimelineContext sets HOME to a fresh temp dir with no ~/.claude/skills,
	// so os.ReadDir fails -> the empty-skills arm.
	ctx := newTimelineContext(t)
	rec := getJSON(t, ctx.OfficeAgentSearchSkills, "/api/office/agent/skills/search?q=foo")
	skills, _ := rec["skills"].([]interface{})
	if len(skills) != 0 {
		t.Errorf("expected empty skills when ~/.claude/skills missing, got %v", rec["skills"])
	}
}

func TestOfficeHealth_UptimeArm(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Mark the team spawned so GetOfficeSettings carries a positive teamSpawnedAt.
	// SetTeamSpawned starts the idle-monitoring goroutine (reads library.Dir()
	// via memify); stop it in cleanup so it can't outlive the test and race
	// another test's library.SetDirForTest.
	ctx.OfficeManager.FinalizeTeamSpawn(spawnTeamRequest{WorkingDir: t.TempDir()}, "office-7", true)
	t.Cleanup(ctx.OfficeManager.StopIdleMonitoring)

	rec := getJSON(t, ctx.OfficeHealth, "/api/office/health")
	if rec["status"] != "healthy" {
		t.Errorf("expected healthy status, got %v", rec["status"])
	}
	// uptime should be >= 0 (the teamSpawnedAt>0 arm computed it).
	if _, ok := rec["uptime"]; !ok {
		t.Error("expected an uptime field")
	}
}

func TestAllTeamTemplates_IncludesCustom(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Create a custom template so the custom-loop appends it.
	created := ctx.TeamTemplateStore.Create(teamtemplates.TeamTemplate{ID: "my-custom", Name: "My Custom Team"})
	if created == nil {
		t.Fatal("create custom template returned nil")
	}
	all := ctx.allTeamTemplates()
	found := false
	for _, tpl := range all {
		if tpl.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom template not present in allTeamTemplates result")
	}
}

func TestOfficeGetStandupByID_ReadErrorAndJSONFallback(t *testing.T) {
	ctx := newTimelineContext(t)
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// A standup file with NON-JSON content -> the json.Unmarshal fallback to
	// string (office.go:1927-1929).
	if err := os.WriteFile(filepath.Join(dir, "plain.json"), []byte("not-json-at-all"), 0o644); err != nil {
		t.Fatalf("write plain: %v", err)
	}
	recJSON := httptest.NewRecorder()
	ctx.OfficeGetStandupByID(recJSON, withURLParam(httptest.NewRequest("GET", "/api/office/standups/plain", nil), "standupId", "plain"))
	if recJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 with the string fallback, got %d: %s", recJSON.Code, recJSON.Body.String())
	}

	// A standup "file" that's actually a directory -> os.ReadFile fails with a
	// non-IsNotExist error -> the 500 read-error arm (1922-1923).
	if err := os.Mkdir(filepath.Join(dir, "adir.json"), 0o755); err != nil {
		t.Fatalf("mkdir adir: %v", err)
	}
	recErr := httptest.NewRecorder()
	ctx.OfficeGetStandupByID(recErr, withURLParam(httptest.NewRequest("GET", "/api/office/standups/adir", nil), "standupId", "adir"))
	if recErr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory as a standup file, got %d: %s", recErr.Code, recErr.Body.String())
	}
}

func TestOfficeAgentMemorySection_WriteAppendRead(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-mem", "cfg-mem", &office.TeamMemberConfig{ID: "cfg-mem", Name: "Memo"})

	// Write a section.
	recW := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-mem", "name": "learnings", "content": "first lesson", "append": false,
	})
	if recW.Code != http.StatusOK {
		t.Fatalf("write section: expected 200, got %d: %s", recW.Code, recW.Body.String())
	}

	// Append to it.
	recA := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-mem", "name": "learnings", "content": "second lesson", "append": true,
	})
	if recA.Code != http.StatusOK {
		t.Fatalf("append section: expected 200, got %d: %s", recA.Code, recA.Body.String())
	}

	// Read it back.
	recR := getJSON(t, ctx.OfficeAgentGetMemorySection, "/api/office/agent/memory/section?sessionId=s-mem&name=learnings")
	content, _ := recR["content"].(string)
	if !strings.Contains(content, "first lesson") || !strings.Contains(content, "second lesson") {
		t.Errorf("expected both lessons in the section, got %q", content)
	}
}
