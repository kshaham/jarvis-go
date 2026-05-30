package handler

// P13.8 · Team-wide search across in-memory office state. Linear scan
// for now — adequate at office scale (typical 5-15 agents, hundreds
// of messages, dozens of active tasks). Transcripts are out of scope
// for the first ship; can be added when the FTS index lands.
//
// POST /api/office/search
//   {
//     "query": "string to match (case-insensitive substring)",
//     "types": ["message", "task"],   // optional; default = all
//     "limit": 50                      // optional; default = 50, max = 200
//   }
//
// Returns a flat ranked list of hits, each tagged with its type so
// the UI can render them in groups + offer type-specific deep links
// (e.g. tap a message → open the chat thread; tap a task → open the
// task detail).

import (
	"net/http"
	"sort"
	"strings"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

type searchRequest struct {
	Query string   `json:"query"`
	Types []string `json:"types,omitempty"`
	Limit int      `json:"limit,omitempty"`
}

// SearchHit is one matched row. `Kind` is one of "message" or "task"
// today; future kinds (transcript, file-lock, snapshot) extend the
// union without breaking older clients.
type SearchHit struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	SessionID string  `json:"sessionId,omitempty"`
	AgentName string  `json:"agentName,omitempty"`
	Score     float64 `json:"score"`
	// Timestamp millis since epoch when the hit was created/updated,
	// for "recently changed" sorting on the client.
	TimestampMs int64 `json:"timestampMs,omitempty"`
}

type searchResponse struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
	Total int         `json:"total"`
}

const (
	searchMaxLimit     = 200
	searchDefaultLimit = 50
	searchSnippetWidth = 120
)

// OfficeSearch handles POST /api/office/search.
func (ctx *Context) OfficeSearch(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<14) {
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		request.WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	if ctx.OfficeManager == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "office manager offline")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = searchDefaultLimit
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}

	includeMsg, includeTask := selectorEnabled(req.Types, "message"), selectorEnabled(req.Types, "task")

	hits := make([]SearchHit, 0, limit*2)
	qLower := strings.ToLower(q)

	if includeMsg {
		for _, m := range ctx.OfficeManager.GetMessages() {
			score, snippet := scoreSubstring(qLower, m.Content)
			if score == 0 {
				continue
			}
			tsMs := int64(0)
			// OfficeMessage.Timestamp is an RFC3339 string in some
			// builds + time.Time in others; rely on the JSON shape
			// the dashboard already consumes. We expose a millisecond
			// epoch for client sorting; leave 0 if parsing would
			// require a format guess.
			hits = append(hits, SearchHit{
				Kind:        "message",
				ID:          m.ID,
				Title:       fromOr(m.FromName, m.From) + " → " + fromOr(m.ToName, m.To),
				Snippet:     snippet,
				AgentName:   fromOr(m.FromName, m.From),
				Score:       score,
				TimestampMs: tsMs,
			})
		}
	}

	if includeTask {
		for _, agent := range ctx.OfficeManager.GetRoster() {
			if agent == nil {
				continue
			}
			agentName := ""
			if agent.Config != nil {
				agentName = agent.Config.Name
			}
			for _, t := range agent.GetTasks() {
				if t == nil {
					continue
				}
				// Score the task across title + description; pick the
				// stronger match for the visible snippet.
				titleScore, titleSnippet := scoreSubstring(qLower, t.Title)
				descScore, descSnippet := scoreSubstring(qLower, t.Description)
				if titleScore == 0 && descScore == 0 {
					continue
				}
				score := titleScore
				snippet := titleSnippet
				if descScore > score {
					score = descScore
					snippet = descSnippet
				}
				hits = append(hits, SearchHit{
					Kind:      "task",
					ID:        t.ID,
					Title:     t.Title,
					Snippet:   snippet,
					SessionID: agent.SessionID(),
					AgentName: agentName,
					Score:     score,
				})
			}
		}
	}

	// Sort by score desc, then by title for stable presentation.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Title < hits[j].Title
	})

	total := len(hits)
	if total > limit {
		hits = hits[:limit]
	}
	request.WriteJSON(w, http.StatusOK, searchResponse{
		Query: q,
		Hits:  hits,
		Total: total,
	})
}

// selectorEnabled reports whether the given kind is in `types`. An
// empty `types` slice means "all kinds enabled" — the typical case
// when the operator just wants to search everything.
func selectorEnabled(types []string, kind string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == kind {
			return true
		}
	}
	return false
}

// scoreSubstring returns a (score, snippet) pair. Score is 0 when the
// query doesn't appear in `text`; otherwise it weights an exact
// case-sensitive match higher than a case-insensitive one, and an
// earlier match higher than a later one. The snippet is a
// ±searchSnippetWidth/2 window around the first match for context.
func scoreSubstring(qLower, text string) (float64, string) {
	if text == "" {
		return 0, ""
	}
	textLower := strings.ToLower(text)
	idx := strings.Index(textLower, qLower)
	if idx < 0 {
		return 0, ""
	}
	score := 1.0
	if strings.Contains(text, qLower) {
		// Exact-case match — slight bump so "Foo" outranks "foo" when
		// the operator capitalised on purpose.
		score = 1.5
	}
	// Earlier matches are usually titles or leading description text
	// — bump those over deep-in-body matches.
	if idx < 40 {
		score += 0.3
	}
	return score, makeSnippet(text, idx, len(qLower))
}

// makeSnippet trims to a ±width/2 window around the match, padding
// with ellipses on either side when truncated.
func makeSnippet(text string, matchIdx, matchLen int) string {
	width := searchSnippetWidth
	half := width / 2
	start := matchIdx - half/2
	if start < 0 {
		start = 0
	}
	end := matchIdx + matchLen + half
	if end > len(text) {
		end = len(text)
	}
	out := text[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out = out + "…"
	}
	// Strip newlines so dropdown rows don't wrap unexpectedly.
	out = strings.ReplaceAll(out, "\n", " ")
	out = strings.ReplaceAll(out, "\r", "")
	return out
}

// fromOr returns the first non-empty string.
func fromOr(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

// _ keeps the office import live in case future extensions reference
// office types directly (Task / OfficeMessage shapes evolve over time).
var _ = office.TaskStatus("")
