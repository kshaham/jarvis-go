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

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// OfficeListReports returns the metadata for every persisted wrap-up
// report, newest-first. GET /api/office/reports
func (ctx *Context) OfficeListReports(w http.ResponseWriter, r *http.Request) {
	list, err := office.ListWrapUpReports()
	if err != nil {
		slog.Error("list wrap-up reports failed", "err", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to list reports")
		return
	}
	if list == nil {
		list = []office.WrapUpReportMeta{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"reports": list,
	})
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
	ctx.SSEBroker.Broadcast("office:reportGenerated", meta)
	ok(w, map[string]interface{}{"report": meta})
}
