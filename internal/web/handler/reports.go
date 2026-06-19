package handler

// R33 · Wrap-up / post-mortem report endpoints.
//
// Three routes:
//   GET  /api/office/reports                -- list metadata
//   GET  /api/office/reports/{filename}     -- download one report
//   POST /api/office/reports/generate       -- build one now (CSRF)
//
// Automatic generation on team wrap-up is wired in
// OfficePostWrapUp (handler/office.go).

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

// OfficeListReports returns the report library, newest-first:
// every persisted wrap-up report PLUS the agent sticky-note feed
// (Feature B) merged in as lightweight, file-less entries.
// GET /api/office/reports
//
// Each item carries a "type" discriminator: "report" for the on-disk
// wrap-up reports, "note" for sticky notes. Backward-compatible fields
// (filename, preview, sizeBytes, modTime) are present on every item so
// the existing SPA rows keep rendering even without "type" awareness;
// notes use a stable synthetic "note-<id>" filename (NOT a real file).
func (ctx *Context) OfficeListReports(w http.ResponseWriter, r *http.Request) {
	list, err := office.ListWrapUpReports()
	if err != nil {
		slog.Error("list wrap-up reports failed", "err", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to list reports")
		return
	}

	merged := make([]map[string]interface{}, 0, len(list)+8)
	for _, meta := range list {
		merged = append(merged, map[string]interface{}{
			"type":        "report",
			"filename":    meta.Filename,
			"generatedAt": meta.GeneratedAt,
			"modTime":     meta.GeneratedAt,
			"sizeBytes":   meta.SizeBytes,
			"preview":     stickyReportPreview(meta.TeamMission, meta.Trigger, meta.Reason),
		})
	}
	for _, note := range ctx.OfficeManager.ListStickyNotes() {
		merged = append(merged, map[string]interface{}{
			"type":        "note",
			"filename":    "note-" + note.ID,
			"author":      note.Author,
			"generatedAt": note.CreatedAt,
			"modTime":     note.CreatedAt,
			"sizeBytes":   len(note.Content),
			"preview":     truncatePreview(note.Content, 200),
			"content":     note.Content,
			"color":       note.Color,
		})
	}
	// Newest-first across both kinds.
	sort.Slice(merged, func(i, j int) bool {
		return reportSortTime(merged[i]).After(reportSortTime(merged[j]))
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"reports": merged,
	})
}

// reportSortTime extracts the sort timestamp from a merged report/note
// item. Both kinds carry "generatedAt" as a time.Time.
func reportSortTime(item map[string]interface{}) time.Time {
	if t, ok := item["generatedAt"].(time.Time); ok {
		return t
	}
	return time.Time{}
}

// stickyReportPreview builds a one-line preview for a wrap-up report row
// from its parsed header fields, falling back to the trigger/reason.
func stickyReportPreview(mission, trigger, reason string) string {
	if mission != "" {
		return truncatePreview(mission, 200)
	}
	if reason != "" {
		return truncatePreview(trigger+": "+reason, 200)
	}
	return trigger
}

// truncatePreview trims s to max runes, appending an ellipsis when cut.
func truncatePreview(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// OfficeGetReport downloads a single report as Markdown.
// GET /api/office/reports/{filename}
//
// Filename must match the `wrap-up-YYYYMMDD-HHMMSS.md` pattern;
// anything else is rejected before the filesystem is touched.
func (ctx *Context) OfficeGetReport(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !office.ValidateReportFilename(filename) {
		request.WriteError(w, http.StatusBadRequest, "invalid report filename")
		return
	}
	body, err := office.LoadWrapUpReport(filename)
	if err != nil {
		// Differentiate not-found from other errors without leaking
		// filesystem paths in the response (R28 hygiene).
		slog.Warn("load wrap-up report failed", "filename", filename, "err", err)
		request.WriteError(w, http.StatusNotFound, "report not found")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

type generateReportRequest struct {
	Trigger string `json:"trigger,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// OfficeGenerateReport builds a wrap-up report on demand.
// POST /api/office/reports/generate
func (ctx *Context) OfficeGenerateReport(w http.ResponseWriter, r *http.Request) {
	var req generateReportRequest
	// Empty body is allowed: defaults to trigger="manual".
	if r.ContentLength > 0 {
		if !decodeBody(w, r, &req) {
			return
		}
	}
	trigger := req.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	meta, err := office.GenerateWrapUpReport(ctx.OfficeManager, trigger, req.Reason)
	if err != nil {
		slog.Error("generate wrap-up report failed", "err", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to generate report")
		return
	}
	ctx.emit("office:reportGenerated", meta)
	ok(w, map[string]interface{}{"report": meta})
}
