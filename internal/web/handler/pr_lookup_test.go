package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/go-chi/chi/v5"
)

func prLookupReq(t *testing.T, ctx *Context, raw string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/office/pr/"+raw, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("number", raw)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeLookupPR(rec, req)
	return rec
}

func TestOfficeLookupPR_RejectsZero(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := prLookupReq(t, ctx, "0")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for pr=0, got %d", rec.Code)
	}
}

func TestOfficeLookupPR_RejectsNegative(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := prLookupReq(t, ctx, "-7")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative pr, got %d", rec.Code)
	}
}

func TestOfficeLookupPR_RejectsNonNumeric(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := prLookupReq(t, ctx, "main")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric pr, got %d", rec.Code)
	}
}

func TestOfficeLookupPR_404WhenNoTeamSpawned(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// teamSpawned is false by default; lookup should 404 before
	// touching teamPRNumber.
	rec := prLookupReq(t, ctx, "42")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no team spawned, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeLookupPR_404WhenDifferentPR(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetTeamPRNumber(99)
	rec := prLookupReq(t, ctx, "42")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when team linked to a different PR, got %d", rec.Code)
	}
}

func TestOfficeLookupPR_HappyPathReturnsRosterAndMission(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{
		ID: "vex", Name: "Vex", Role: "scout",
	})
	ctx.OfficeManager.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{
		ID: "bolt", Name: "Bolt", Role: "builder",
	})
	ctx.OfficeManager.SetTeamSpawned(true)
	ctx.OfficeManager.SetSpawnContext("ship-it", "/srv/ship-it")
	ctx.OfficeManager.SetTeamMission("ship the dropdown fix")
	ctx.OfficeManager.SetTeamPRNumber(1234)

	rec := prLookupReq(t, ctx, "1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["prNumber"].(float64); int(got) != 1234 {
		t.Errorf("prNumber=%v, want 1234", resp["prNumber"])
	}
	if resp["workingDir"] != "/srv/ship-it" {
		t.Errorf("workingDir=%v, want /srv/ship-it", resp["workingDir"])
	}
	if resp["mission"] != "ship the dropdown fix" {
		t.Errorf("mission=%v, want 'ship the dropdown fix'", resp["mission"])
	}
	sessions, _ := resp["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Fatalf("sessions count = %d, want 2", len(sessions))
	}
}

func TestOfficeLookupPR_503WhenOfficeManagerNil(t *testing.T) {
	t.Parallel()
	ctx := &Context{} // no OfficeManager
	rec := prLookupReq(t, ctx, "1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when OfficeManager nil, got %d", rec.Code)
	}
}
