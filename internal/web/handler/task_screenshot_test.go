package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/office"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func TestTaskScreenshot_UploadHappyPath(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot,
		"/api/office/agent/task/screenshot",
		map[string]interface{}{
			"sessionId":  "s-vex",
			"taskId":     "task-42",
			"mimeType":   "image/png",
			"dataBase64": base64.StdEncoding.EncodeToString(tinyPNG),
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	fn, _ := resp["filename"].(string)
	if !strings.HasPrefix(fn, "task-42-") || !strings.HasSuffix(fn, ".png") {
		t.Errorf("unexpected filename: %q", fn)
	}
	// Also verify HasTaskScreenshot now returns true.
	if !office.HasTaskScreenshot("task-42") {
		t.Error("HasTaskScreenshot should return true after upload")
	}
}

func TestTaskScreenshot_UploadOversizeRejected(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	big := bytes.Repeat([]byte{0xAB}, office.TaskScreenshotMaxBytes+1)
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot,
		"/api/office/agent/task/screenshot",
		map[string]interface{}{
			"sessionId":  "s-vex",
			"taskId":     "task-42",
			"mimeType":   "image/png",
			"dataBase64": base64.StdEncoding.EncodeToString(big),
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskScreenshot_UploadInvalidBase64(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot,
		"/api/office/agent/task/screenshot",
		map[string]interface{}{
			"sessionId":  "s-vex",
			"taskId":     "task-42",
			"mimeType":   "image/png",
			"dataBase64": "not base64!!!",
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTaskScreenshot_DownloadViaChi(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	// Seed via the library path so the test doesn't depend on the
	// upload handler path being green.
	filename, err := office.SaveTaskScreenshot("task-99", "image/png", tinyPNG, 1700000000)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/office/task-screenshots/{filename}", ctx.OfficeGetTaskScreenshot)
	req := httptest.NewRequest("GET", "/api/office/task-screenshots/"+filename, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(context.Background()))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected image/png, got %s", rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
		t.Error("response body does not match original PNG")
	}
}

func TestTaskScreenshot_DownloadInvalidFilename(t *testing.T) {
	ctx := newTimelineContext(t)
	r := chi.NewRouter()
	r.Get("/api/office/task-screenshots/{filename}", ctx.OfficeGetTaskScreenshot)
	req := httptest.NewRequest("GET", "/api/office/task-screenshots/..%2Fpasswd", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(context.Background()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for traversal, got %d", rec.Code)
	}
}
