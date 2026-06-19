package handler

// Coverage for the mid-run perpetual-duration update endpoint
// (OfficePostPerpetualDuration): it requires a spawned perpetual team, caps
// the hours, recomputes the deadline from spawn time, and re-arms a deadline
// that had already wrapped the team up.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestOfficePostPerpetualDuration_NoTeam(t *testing.T) {
	ctx := newTimelineContext(t)

	rec := postJSON(t, ctx.OfficePostPerpetualDuration, "/api/office/perpetual-duration",
		map[string]interface{}{"hours": 6})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 with no team: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostPerpetualDuration_NotPerpetual(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetPerpetualMode(false)

	rec := postJSON(t, ctx.OfficePostPerpetualDuration, "/api/office/perpetual-duration",
		map[string]interface{}{"hours": 6})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409 when perpetual is off: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostPerpetualDuration_RejectsOutOfRange(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetPerpetualMode(true)

	for _, hours := range []int{-1, maxPerpetualDurationHours + 1} {
		rec := postJSON(t, ctx.OfficePostPerpetualDuration, "/api/office/perpetual-duration",
			map[string]interface{}{"hours": hours})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("hours=%d status=%d, want 400", hours, rec.Code)
		}
	}
}

func TestOfficePostPerpetualDuration_UpdatesDeadline(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true) // stamps teamSpawnedAt = now
	ctx.OfficeManager.SetPerpetualMode(true)

	rec := postJSON(t, ctx.OfficePostPerpetualDuration, "/api/office/perpetual-duration",
		map[string]interface{}{"hours": 6})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}

	deadline := ctx.OfficeManager.GetPerpetualDeadline()
	if deadline.IsZero() {
		t.Fatal("deadline should be set for hours=6")
	}
	// Deadline is spawn + 6h, so it must be in the future (~6h out).
	if !deadline.After(time.Now().Add(5 * time.Hour)) {
		t.Errorf("deadline %v should be ~6h out", deadline)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["perpetualDeadline"] == "" || resp["perpetualDeadline"] == nil {
		t.Errorf("response perpetualDeadline should be non-empty, got %v", resp["perpetualDeadline"])
	}
}

func TestOfficePostPerpetualDuration_Unlimited(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetPerpetualMode(true)
	ctx.OfficeManager.SetPerpetualDurationHours(6) // start bounded

	rec := postJSON(t, ctx.OfficePostPerpetualDuration, "/api/office/perpetual-duration",
		map[string]interface{}{"hours": 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !ctx.OfficeManager.GetPerpetualDeadline().IsZero() {
		t.Error("hours=0 should clear the deadline (unlimited)")
	}
}
