package handler

// Coverage for the live-preview completion gate on OfficePostWrapUp: a
// web-app mission with no healthy dev instance is blocked with 409
// needsLivePreview unless {force:true} is passed.

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestOfficePostWrapUp_BlockedWithoutLivePreview(t *testing.T) {
	// Arrange: a spawned team on a web-app mission with no dev instance.
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetSpawnContext("tmpl", t.TempDir())
	ctx.OfficeManager.SetTeamMission("Build a React dashboard web app")

	// Act
	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})

	// Assert
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409 without live preview: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["needsLivePreview"] != true {
		t.Errorf("needsLivePreview=%v, want true", resp["needsLivePreview"])
	}
	if ctx.OfficeManager.IsWrappingUp() {
		t.Error("wrap-up should not have started when the gate blocks")
	}
}

func TestOfficePostWrapUp_ForceOverridesGate(t *testing.T) {
	// Arrange: same blocked setup, but force the override.
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetSpawnContext("tmpl", t.TempDir())
	ctx.OfficeManager.SetTeamMission("Build a React dashboard web app")

	// Act
	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{"force": true})

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 with force=true: %s", rec.Code, rec.Body.String())
	}
	if !ctx.OfficeManager.IsWrappingUp() {
		t.Error("wrap-up should have started with force=true")
	}
}

func TestOfficePostWrapUp_NonWebMissionProceeds(t *testing.T) {
	// Arrange: a non-web mission does not trip the gate even without a
	// dev instance.
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetSpawnContext("tmpl", t.TempDir())
	ctx.OfficeManager.SetTeamMission("Refactor the data pipeline parser library")

	// Act
	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 for non-web mission: %s", rec.Code, rec.Body.String())
	}
	if !ctx.OfficeManager.IsWrappingUp() {
		t.Error("wrap-up should have started for a non-web mission")
	}
}
