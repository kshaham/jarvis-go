package handler

// R24 · Dollar-based cost caps removed. The operator uses subscription
// plans (Z.AI Coding, Claude subscription) which are quota-based, not
// pay-per-use — the SDK reports cost as $0 for subscription turns,
// making every dollar cap effectively inert.
//
// What was removed:
//   - Per-session MaxCostUSD cap + EventCostExceeded (R18)
//   - Daily USD cap threshold firing (50/75/90/100% alerts)
//   - stopActiveSessionsForDailyCap / IsSpendingFrozen enforcement
//   - PUT /api/spending/cap + SetSpendingCap handler
//
// What remains (purely informational):
//   - todayCost helper, read from RecordDailyStats
//   - GetSpendingStatus for the header cost widget in app.js
//   - The Settings page teamDailyBudget field + office dashboard pill
//     (driven by officeSettings.teamDailyBudget, not this module)
//
// Runaway defense still lives in the R18 per-session turn cap and R19
// wedged-stop — both quota-agnostic and independent of billing model.

import (
	"net/http"
	"time"

	"github.com/claudeman/jarvis-go/internal/state"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// todayCost returns the running USD total for today, taken from the daily
// stats slice (updated by RecordDailyStats on every turn). Still emitted
// by the SDK for observability — informational only.
func todayCost(stats []state.DailyTokenStats) float64 {
	today := time.Now().Format("2006-01-02")
	for _, ds := range stats {
		if ds.Date == today {
			return ds.Cost
		}
	}
	return 0
}

// GetSpendingStatus returns today's cost + recent daily stats for the
// header cost widget. Cap-related fields dropped; the widget now shows
// a plain "today's reported spend" readout without any enforcement
// or percentage-used implication.
// GET /api/spending/status
func (ctx *Context) GetSpendingStatus(w http.ResponseWriter, r *http.Request) {
	stats := ctx.StateStore.GetDailyStats()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"todayCostUSD": todayCost(stats),
		"dailyStats":   stats,
	})
}
