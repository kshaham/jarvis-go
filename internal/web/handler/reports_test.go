package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeListReports_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/reports", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeListReports(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Reports []office.WrapUpReportMeta `json:"reports"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Reports) != 0 {
		t.Errorf("expected 0 reports, got %d", len(resp.Reports))
	}
}

func TestOfficeGenerateReport_HappyPath(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamMission("Ship it.")
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeGenerateReport, "/api/office/reports/generate", map[string]interface{}{
		"trigger": "manual",
		"reason":  "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Report office.WrapUpReportMeta `json:"report"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !office.ValidateReportFilename(resp.Report.Filename) {
		t.Errorf("bad filename in response: %s", resp.Report.Filename)
	}
	if resp.Report.Trigger != "manual" {
		t.Errorf("meta.Trigger = %q", resp.Report.Trigger)
	}
	// Now list should show it.
	listReq := httptest.NewRequest("GET", "/api/office/reports", nil)
	listRec := httptest.NewRecorder()
	ctx.OfficeListReports(listRec, listReq)
	var listResp struct {
		Reports []office.WrapUpReportMeta `json:"reports"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	if len(listResp.Reports) != 1 || listResp.Reports[0].Filename != resp.Report.Filename {
		t.Errorf("list did not contain the generated report; got %+v", listResp.Reports)
	}
}

// getReportViaChi builds a chi-routed request so URLParam resolves.
func getReportViaChi(t *testing.T, ctx *Context, filename string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/office/reports/{filename}", ctx.OfficeGetReport)
	req := httptest.NewRequest("GET", "/api/office/reports/"+filename, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(context.Background()))
	return rec
}

func TestOfficeGetReport_HappyPath(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.SetTeamMission("Ship it.")
	meta, err := office.GenerateWrapUpReport(ctx.OfficeManager, "manual", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	rec := getReportViaChi(t, ctx, meta.Filename)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Errorf("expected markdown Content-Type, got %s", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), meta.Filename) {
		t.Errorf("Content-Disposition missing filename: %s", rec.Header().Get("Content-Disposition"))
	}
	if !strings.Contains(rec.Body.String(), "# Team Wrap-Up Report") {
		t.Error("body missing header")
	}
}

func TestOfficeGetReport_InvalidFilename(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := getReportViaChi(t, ctx, "..%2F..%2Fetc%2Fpasswd")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for traversal attempt, got %d", rec.Code)
	}
}

func TestOfficeGetReport_NotFound(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := getReportViaChi(t, ctx, "wrap-up-20260101-000000.md")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing report, got %d", rec.Code)
	}
}
