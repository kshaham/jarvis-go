package handler

// Branch fills for OfficeGenerateReport: the empty-body path (trigger defaults
// to "manual") and the bad-JSON path (decodeBody failure -> 400).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeGenerateReport_EmptyBodyDefaultsManual(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamMission("Ship it.")
	// No body at all -> ContentLength == 0 -> skip decode -> trigger="manual".
	req := httptest.NewRequest("POST", "/api/office/reports/generate", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGenerateReport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with empty body, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Report office.WrapUpReportMeta `json:"report"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Report.Trigger != "manual" {
		t.Errorf("empty-body trigger=%q, want manual", resp.Report.Trigger)
	}
}

func TestOfficeGenerateReport_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/reports/generate", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeGenerateReport(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON body, got %d", rec.Code)
	}
}
