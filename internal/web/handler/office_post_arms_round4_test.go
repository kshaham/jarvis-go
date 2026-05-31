package handler

// Round-4 fills for office.go POST-handler arms:
//   - OfficePostWhiteboard: decodeBody bad-JSON 400 (1382-1384)
//   - OfficePostStandupUserMessage: decodeBody bad-JSON 400 (1794-1796)
//   - OfficeTemplateFromTeam: decodeBody bad-JSON 400 after a team is spawned
//     (457-459, gated behind IsTeamSpawned)
//   - OfficePostWrapUp: the missed-agents warn arm (1682-1685) — a registered
//     agent with no live session means writeSessionPrompt returns false.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficePostWhiteboard_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficePostWhiteboard(rec, badJSONReq("/api/office/whiteboard"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficePostStandupUserMessage_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupUserMessage(rec, badJSONReq("/api/office/standup/message"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficeTemplateFromTeam_BadJSONAfterSpawn400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// IsTeamSpawned guards the decode; mark a team spawned so the decode runs and
	// then fails on the malformed body. FinalizeTeamSpawn -> SetTeamSpawned spins
	// up the idle-monitoring goroutine (which reads library.Dir() via memify);
	// stop it in cleanup so it can't outlive the test and race other tests'
	// library.SetDirForTest.
	ctx.OfficeManager.FinalizeTeamSpawn(spawnTeamRequest{WorkingDir: t.TempDir()}, "office-tpl", true)
	t.Cleanup(ctx.OfficeManager.StopIdleMonitoring)
	rec := httptest.NewRecorder()
	ctx.OfficeTemplateFromTeam(rec, badJSONReq("/api/office/template-from-team"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON after spawn, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostWrapUp_MissedAgentWarn(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Spawn a team and register an agent whose session is NOT in the
	// SessionManager, so writeSessionPrompt returns false -> the agent is
	// "missed" and the warn arm fires. Still a 200 response.
	ctx.OfficeManager.FinalizeTeamSpawn(spawnTeamRequest{WorkingDir: t.TempDir()}, "office-wrap", true)
	t.Cleanup(ctx.OfficeManager.StopIdleMonitoring)
	ctx.OfficeManager.RegisterAgent("ghost-sess", "cfg-g", &office.TeamMemberConfig{ID: "cfg-g", Name: "Ghosty"})

	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from wrap-up, got %d: %s", rec.Code, rec.Body.String())
	}
}
