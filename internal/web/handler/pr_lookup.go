package handler

// SECURITY NOTE — IDOR / information disclosure (intentional, documented).
//
// This endpoint is behind the standard auth middleware so anonymous
// callers can't reach it. However, ANY authenticated caller — including
// spawned agents carrying an agent token — can look up the current
// team's `workingDir` (absolute host filesystem path) and `mission`
// (full prose) for any PR number.
//
// Why this is acceptable today:
//   - Jarvis is single-operator in current deployments; agents inherit
//     full read trust from the operator who spawned them.
//   - The same workingDir is already returned by GET /api/office/status
//     (the dashboard's main poll), so withholding it here would not
//     materially raise the attacker bar against a leaked agent token.
//
// Why this needs revisiting before multi-user:
//   - In a deployment with mutually-distrustful operators, a leaked or
//     stolen agent token from team A would let an attacker enumerate
//     team B's PRs by walking sequential PR numbers and reading their
//     missions + filesystem paths.
//   - Remediation: scope this endpoint to operator-session-cookie auth
//     (exclude agent tokens) OR omit `workingDir` from the response.
//     The user explicitly deferred this fix in the Phase 7 review;
//     tracked as a follow-up under that plan.

import (
	"net/http"
	"strconv"

	"github.com/kshaham/jarvis/internal/web/request"
	"github.com/go-chi/chi/v5"
)

// R3.6 · OfficeLookupPR maps an inbound PR number to the live team
// working on it. Used by external tooling (CI bots, the operator's
// shell `claude --from-pr <n>`) to discover whether jarvis already
// has agents on the PR before spawning a duplicate team.
//
// Returns 404 when no team is currently spawned, or when the spawned
// team isn't linked to the requested PR. On hit, returns the team's
// mission, working dir, and the session IDs (jarvis-side; not the
// underlying Claude session IDs) so the caller can deep-link into
// the dashboard.
//
// GET /api/office/pr/{number}
func (ctx *Context) OfficeLookupPR(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "number")
	prNumber, err := strconv.Atoi(raw)
	if err != nil || prNumber <= 0 {
		request.WriteError(w, http.StatusBadRequest, "invalid PR number")
		return
	}
	if ctx.OfficeManager == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "office manager offline")
		return
	}
	if !ctx.OfficeManager.IsTeamSpawned() {
		request.WriteError(w, http.StatusNotFound, "no team currently spawned")
		return
	}
	linked := ctx.OfficeManager.GetTeamPRNumber()
	if linked != prNumber {
		request.WriteError(w, http.StatusNotFound, "no team linked to PR #"+raw)
		return
	}

	roster := ctx.OfficeManager.GetRoster()
	sessions := make([]map[string]interface{}, 0, len(roster))
	for _, a := range roster {
		if a == nil {
			continue
		}
		entry := map[string]interface{}{
			"sessionId": a.SessionID(),
			"name":      "",
			"role":      "",
		}
		if a.Config != nil {
			entry["name"] = a.Config.Name
			entry["role"] = a.Config.Role
		}
		sessions = append(sessions, entry)
	}
	_, workingDir := ctx.OfficeManager.GetSpawnContext()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"prNumber":   prNumber,
		"workingDir": workingDir,
		"mission":    ctx.OfficeManager.GetTeamMission(),
		"sessions":   sessions,
	})
}
