package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/go-chi/chi/v5"
)

func usageReq(t *testing.T, ctx *Context, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/office/agent/"+sessionID+"/usage-series", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeAgentUsageSeries(rec, req)
	return rec
}

func TestOfficeAgentUsageSeries_RequiresSessionID(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := usageReq(t, ctx, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty sessionId, got %d", rec.Code)
	}
}

func TestOfficeAgentUsageSeries_404OnUnknownSession(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := usageReq(t, ctx, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOfficeAgentUsageSeries_HappyPath(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	// Record three turns at different costs.
	agent.RecordTurnUsage(100, 50, 0.05)
	agent.RecordTurnUsage(200, 80, 0.07)
	agent.RecordTurnUsage(150, 60, 0.06)

	rec := usageReq(t, ctx, "s-vex")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID       string    `json:"sessionId"`
		TurnCount       int64     `json:"turnCount"`
		TotalCostUSD    float64   `json:"totalCostUSD"`
		RecentTurnCosts []float64 `json:"recentTurnCosts"`
		CostPerTurnAvg  float64   `json:"costPerTurnAvg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "s-vex" {
		t.Errorf("sessionId=%q want s-vex", resp.SessionID)
	}
	if resp.TurnCount != 3 {
		t.Errorf("turnCount=%d want 3", resp.TurnCount)
	}
	if len(resp.RecentTurnCosts) != 3 {
		t.Errorf("RecentTurnCosts len=%d want 3", len(resp.RecentTurnCosts))
	}
	if resp.CostPerTurnAvg <= 0 {
		t.Errorf("costPerTurnAvg should be positive, got %f", resp.CostPerTurnAvg)
	}
}
