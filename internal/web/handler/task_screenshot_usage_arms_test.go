package handler

// Round-4 fills:
//   - OfficeAgentTaskScreenshot: SaveTaskScreenshot error -> 400 (60-67), reached
//     with a valid base64 payload but an unsupported mime type (decode + base64
//     succeed, the office save rejects the mime).
//   - OfficeAgentUsageSeries: nil OfficeManager -> 503 (51-54).

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestTaskScreenshot_UnsupportedMimeReturns400(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})

	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot, "/api/office/agent/task/screenshot", map[string]interface{}{
		"sessionId":  "s-vex",
		"taskId":     "task-mime",
		"mimeType":   "image/gif", // not in mimeToExt -> SaveTaskScreenshot errors
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("GIF89a-fake")),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported mime, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported mime type") {
		t.Errorf("expected 'unsupported mime type' in body, got %s", rec.Body.String())
	}
}

func TestOfficeAgentUsageSeries_NilManager503(t *testing.T) {
	// A Context with a nil OfficeManager triggers the office-offline 503 arm
	// (after the sessionId guard passes).
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("GET", "/api/office/agent/usage/s1", nil), "sessionId", "s1")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentUsageSeries(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with a nil OfficeManager, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "office manager offline") {
		t.Errorf("expected 'office manager offline', got %s", rec.Body.String())
	}
}
