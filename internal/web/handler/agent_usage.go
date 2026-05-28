package handler

// P13.7 · GET /api/office/agent/{sessionId}/usage-series — exposes
// the agent's bounded ring of recent per-turn cost values so the
// dashboard can render a sparkline next to each agent card.
//
// The underlying data already lives on AgentState.RecentTurnCosts
// (populated by RecordTurnUsage on every pty.EventTokens turn).
// This handler is a thin shape-cast over that slice plus the
// summary scalars (turn count, last turn timestamp, total cost) so
// callers can hydrate a sparkline tooltip without a second round
// trip.

import (
	"net/http"
	"strings"

	"github.com/kshaham/jarvis/internal/web/request"
	"github.com/go-chi/chi/v5"
)

// usageSeriesResponse is the JSON envelope the dashboard receives.
// Cost values are floats in USD; recentTurnCosts is the bounded ring
// in chronological order (oldest → newest). The cap is enforced
// inside AgentState; callers can render the whole slice without
// fearing unbounded growth.
type usageSeriesResponse struct {
	SessionID         string    `json:"sessionId"`
	TurnCount         int64     `json:"turnCount"`
	TotalCostUSD      float64   `json:"totalCostUSD"`
	TotalInputTokens  int64     `json:"totalInputTokens"`
	TotalOutputTokens int64     `json:"totalOutputTokens"`
	LastTurnAt        string    `json:"lastTurnAt,omitempty"`
	RecentTurnCosts   []float64 `json:"recentTurnCosts"`
	// CostPerTurnAvg is a convenience field — front-ends use it to
	// label the sparkline ("avg $0.03/turn"); avoids a div-by-zero
	// in the client.
	CostPerTurnAvg float64 `json:"costPerTurnAvg"`
}

// OfficeAgentUsageSeries returns the recent per-turn cost series for
// one agent. 404s when the session isn't a registered office member.
//
// GET /api/office/agent/{sessionId}/usage-series
func (ctx *Context) OfficeAgentUsageSeries(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionId"))
	if sessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if ctx.OfficeManager == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "office manager offline")
		return
	}
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "no office agent for session "+sessionID)
		return
	}
	usage := agent.Usage()
	resp := usageSeriesResponse{
		SessionID:         sessionID,
		TurnCount:         usage.TurnCount,
		TotalCostUSD:      usage.TotalCostUSD,
		TotalInputTokens:  usage.TotalInputTokens,
		TotalOutputTokens: usage.TotalOutputTokens,
		RecentTurnCosts:   usage.RecentTurnCosts,
	}
	if !usage.LastTurnAt.IsZero() {
		resp.LastTurnAt = usage.LastTurnAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if usage.TurnCount > 0 {
		resp.CostPerTurnAvg = usage.TotalCostUSD / float64(usage.TurnCount)
	}
	request.WriteJSON(w, http.StatusOK, resp)
}
