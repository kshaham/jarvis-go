package handler

// R30 · Task dependency-graph endpoint.
//
// Read-only, browser-auth. Aggregates every task across the roster
// and returns nodes + edges in a shape ready for the Office UI's
// SVG renderer. Safe to call at any frequency — O(tasks + edges).

import (
	"net/http"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

// OfficeGetTaskGraph returns the aggregated dependency DAG.
// GET /api/office/task-graph
func (ctx *Context) OfficeGetTaskGraph(w http.ResponseWriter, r *http.Request) {
	graph := office.BuildTaskGraph(ctx.OfficeManager)
	request.WriteJSON(w, http.StatusOK, graph)
}
