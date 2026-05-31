package handler

// Final office fills: ConciergeSpawnTeam reaching the shared spawn flow (valid
// built-in template, default working dir that fails the allowlist resolver),
// and the project-invariant mutators.

import (
	"net/http"
	"testing"
)

func TestConciergeSpawnTeam_ValidTemplateReachesSpawn(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// No defaultWorkingDir in settings + no workingDir in the request means
	// the handler falls back to "/app/workspace", which fails the allowlist
	// resolver inside spawnTeamFromRequest -> 400. This still drives the
	// template-validation + working-dir-fallback body of ConciergeSpawnTeam.
	rec := postJSON(t, ctx.ConciergeSpawnTeam, "/api/concierge/spawn", map[string]interface{}{
		"templateId": "office-7", "mission": "ship the thing",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from the spawn resolver, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostProjectInvariant_HappyAndValidation(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// Missing text -> 400.
	recBad := postJSON(t, ctx.OfficePostProjectInvariant, "/api/office/invariants", map[string]interface{}{})
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("missing text: expected 400, got %d", recBad.Code)
	}

	// Happy: text present -> 200 + the invariant is stored.
	rec := postJSON(t, ctx.OfficePostProjectInvariant, "/api/office/invariants", map[string]interface{}{
		"text": "never break the build",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("post invariant: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	invariants, _ := resp["invariants"].([]interface{})
	if len(invariants) != 1 {
		t.Errorf("expected 1 invariant, got %d", len(invariants))
	}
}
