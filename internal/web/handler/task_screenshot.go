package handler

// R34 · Task screenshot upload + download.
//
// POST /api/office/agent/task/screenshot (agent-auth)
// GET  /api/office/task-screenshots/{filename} (browser-auth)
//
// Agents call the POST endpoint from `cm task complete --screenshot
// <path>` to attach concrete visual evidence to a task; operators
// load the image from the dashboard via the GET endpoint.

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

type taskScreenshotRequest struct {
	SessionID  string `json:"sessionId"`
	TaskID     string `json:"taskId"`
	MimeType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
}

// OfficeAgentTaskScreenshot uploads a screenshot for a task.
// Enforces its own 4 MB body cap (vs the default 1 MB) so a 2 MB
// PNG + base64 overhead fits.
func (ctx *Context) OfficeAgentTaskScreenshot(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)
	var req taskScreenshotRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	if req.TaskID == "" {
		request.WriteError(w, http.StatusBadRequest, "taskId is required")
		return
	}
	if req.DataBase64 == "" {
		request.WriteError(w, http.StatusBadRequest, "dataBase64 is required")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.DataBase64)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "dataBase64 is not valid base64")
		return
	}
	filename, err := office.SaveTaskScreenshot(req.TaskID, req.MimeType, raw, time.Now().Unix())
	if err != nil {
		slog.Warn("save task screenshot", "taskId", req.TaskID, "err", err)
		// Surface the specific error (size, mime, empty) since they're
		// all client-fixable; path/traversal cases were already caught
		// upstream by office.taskIDValidRE.
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	relURL := "/api/office/task-screenshots/" + filename
	ctx.SSEBroker.Broadcast("office:taskScreenshotAttached", map[string]interface{}{
		"sessionId": req.SessionID,
		"taskId":    req.TaskID,
		"filename":  filename,
		"url":       relURL,
	})
	ok(w, map[string]interface{}{
		"filename": filename,
		"url":      relURL,
	})
}

// OfficeGetTaskScreenshot serves the image bytes.
// GET /api/office/task-screenshots/{filename}
func (ctx *Context) OfficeGetTaskScreenshot(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !office.ValidateTaskScreenshotFilename(filename) {
		request.WriteError(w, http.StatusBadRequest, "invalid screenshot filename")
		return
	}
	data, err := office.LoadTaskScreenshot(filename)
	if err != nil {
		slog.Warn("load task screenshot", "filename", filename, "err", err)
		request.WriteError(w, http.StatusNotFound, "screenshot not found")
		return
	}
	// Mime from the extension -- we only accept png/jpg/webp on
	// upload so this map is complete.
	mime := "application/octet-stream"
	switch {
	case hasSuffix(filename, ".png"):
		mime = "image/png"
	case hasSuffix(filename, ".jpg"), hasSuffix(filename, ".jpeg"):
		mime = "image/jpeg"
	case hasSuffix(filename, ".webp"):
		mime = "image/webp"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func hasSuffix(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return s[len(s)-len(suf):] == suf
}
