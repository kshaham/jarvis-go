package handler

import (
	"net/http"
	"time"

	"github.com/claudeman/jarvis-go/internal/state"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// spendingCapSettingKey is the settings map key that holds the daily USD cap.
const spendingCapSettingKey = "dailyCostCapUSD"

// InitSpendingTracker initializes the private threshold-tracking state. Safe
// to call multiple times; subsequent calls are no-ops.
func (ctx *Context) InitSpendingTracker() {
	ctx.spendingMu.Lock()
	defer ctx.spendingMu.Unlock()
	if ctx.spendingThresholdsFired == nil {
		ctx.spendingThresholdsFired = map[int]bool{}
	}
}

// todayCost returns the running USD total for today, taken from the daily
// stats slice (updated by RecordDailyStats on every turn).
func todayCost(stats []state.DailyTokenStats) float64 {
	today := time.Now().Format("2006-01-02")
	for _, ds := range stats {
		if ds.Date == today {
			return ds.Cost
		}
	}
	return 0
}

// dailyCapUSD reads the daily cap from settings, returning 0 when unset
// (which means unlimited).
func (ctx *Context) dailyCapUSD() float64 {
	settings := ctx.StateStore.GetSettings()
	switch v := settings[spendingCapSettingKey].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

// checkSpendingThresholds is called after every token-event so it can notify
// the UI when today's cost crosses 50/75/90/100% of the cap. Idempotent --
// repeated calls at the same threshold are deduplicated per day.
func (ctx *Context) checkSpendingThresholds() {
	cap := ctx.dailyCapUSD()
	if cap <= 0 {
		return
	}
	cost := todayCost(ctx.StateStore.GetDailyStats())
	pct := cost / cap * 100
	today := time.Now().Format("2006-01-02")

	ctx.spendingMu.Lock()
	defer ctx.spendingMu.Unlock()

	// Reset threshold tracking if the day rolled over.
	if ctx.spendingDay != today {
		ctx.spendingDay = today
		ctx.spendingThresholdsFired = map[int]bool{}
	}

	for _, t := range []int{50, 75, 90, 100} {
		if pct >= float64(t) && !ctx.spendingThresholdsFired[t] {
			ctx.spendingThresholdsFired[t] = true
			ctx.SSEBroker.Broadcast("spending:threshold", map[string]interface{}{
				"threshold":   t,
				"todayCost":   cost,
				"dailyCap":    cap,
				"percentUsed": pct,
				"frozen":      t >= 100,
			})
		}
	}
}

// IsSpendingFrozen returns true when today's cost has reached the daily cap.
// Session-create endpoints consult this to refuse new work until the next
// day or until the cap is raised.
func (ctx *Context) IsSpendingFrozen() bool {
	cap := ctx.dailyCapUSD()
	if cap <= 0 {
		return false
	}
	return todayCost(ctx.StateStore.GetDailyStats()) >= cap
}

// GetSpendingStatus returns the current daily cost, cap, and freeze state.
// GET /api/spending/status
func (ctx *Context) GetSpendingStatus(w http.ResponseWriter, r *http.Request) {
	stats := ctx.StateStore.GetDailyStats()
	cost := todayCost(stats)
	cap := ctx.dailyCapUSD()
	var pct float64
	if cap > 0 {
		pct = cost / cap * 100
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"todayCostUSD": cost,
		"dailyCapUSD":  cap,
		"percentUsed":  pct,
		"frozen":       cap > 0 && cost >= cap,
		"dailyStats":   stats,
	})
}

// SetSpendingCap updates the daily cap. Pass 0 to disable (unlimited).
// PUT /api/spending/cap  { "dailyCapUSD": 25.0 }
func (ctx *Context) SetSpendingCap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DailyCapUSD float64 `json:"dailyCapUSD"`
	}
	if !request.DecodeAndValidateMax(w, r, &req, 1<<10) {
		return
	}
	if req.DailyCapUSD < 0 {
		request.WriteError(w, http.StatusBadRequest, "dailyCapUSD must be >= 0")
		return
	}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		spendingCapSettingKey: req.DailyCapUSD,
	})

	// Reset threshold tracking so new alerts fire after a cap change.
	ctx.spendingMu.Lock()
	ctx.spendingThresholdsFired = map[int]bool{}
	ctx.spendingMu.Unlock()

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"dailyCapUSD": req.DailyCapUSD,
	})
}
