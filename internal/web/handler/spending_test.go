package handler

// Coverage for spending.go: the read-only spending-status endpoint and
// its todayCost helper. We seed the StateStore via RecordDailyStats and
// assert the JSON shape + that today's cost is surfaced.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
)

func TestTodayCost_NoStats(t *testing.T) {
	t.Parallel()
	if c := todayCost(nil); c != 0 {
		t.Errorf("expected 0 for nil stats, got %v", c)
	}
}

func TestTodayCost_MatchesToday(t *testing.T) {
	t.Parallel()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	// claude-opus has a non-zero per-token cost, so this produces a positive total.
	store.RecordDailyStats(1000, 1000, "claude-opus-4-8")
	stats := store.GetDailyStats()
	c := todayCost(stats)
	if c <= 0 {
		t.Errorf("expected positive cost for today, got %v", c)
	}
}

func TestTodayCost_IgnoresOtherDays(t *testing.T) {
	t.Parallel()
	// A stats slice containing only a different date should return 0.
	stats := []state.DailyTokenStats{
		{Date: "1999-01-01", Cost: 9.99},
	}
	if c := todayCost(stats); c != 0 {
		t.Errorf("expected 0 when no entry for today, got %v", c)
	}
}

func TestGetSpendingStatus_Empty(t *testing.T) {
	t.Parallel()
	ctx := newTestContext()
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "state.json"))

	req := httptest.NewRequest("GET", "/api/spending/status", nil)
	rec := httptest.NewRecorder()
	ctx.GetSpendingStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["todayCostUSD"] != float64(0) {
		t.Errorf("expected todayCostUSD=0, got %v", resp["todayCostUSD"])
	}
	// dailyStats must be present (empty array or null) -- the widget reads it.
	if _, ok := resp["dailyStats"]; !ok {
		t.Error("expected dailyStats key in response")
	}
}

func TestGetSpendingStatus_WithStats(t *testing.T) {
	t.Parallel()
	ctx := newTestContext()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.RecordDailyStats(500, 500, "claude-opus-4-8")
	ctx.StateStore = store

	req := httptest.NewRequest("GET", "/api/spending/status", nil)
	rec := httptest.NewRecorder()
	ctx.GetSpendingStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got, _ := resp["todayCostUSD"].(float64); got <= 0 {
		t.Errorf("expected positive todayCostUSD, got %v", resp["todayCostUSD"])
	}
	stats, _ := resp["dailyStats"].([]interface{})
	if len(stats) != 1 {
		t.Errorf("expected 1 daily-stats entry, got %d", len(stats))
	}
}
