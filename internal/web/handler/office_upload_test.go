package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// buildCodebaseUpload returns a multipart body (and its Content-Type)
// carrying a projectName field and a small zip file part.
func buildCodebaseUpload(t *testing.T, projectName string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()

	// Build the zip payload in memory.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	// Wrap it in a multipart form.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("projectName", projectName); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fw, err := mw.CreateFormFile("file", "codebase.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(zbuf.Bytes()); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return &body, mw.FormDataContentType()
}

func TestOfficeUploadCodebaseHappyPath(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	root := t.TempDir()
	oldRoot := office.NewProjectRoot
	office.NewProjectRoot = root
	defer func() { office.NewProjectRoot = oldRoot }()

	body, contentType := buildCodebaseUpload(t, "myapp", map[string]string{
		"main.go":     "package main",
		"pkg/util.go": "package pkg",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/office/upload-codebase", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	ctx.OfficeUploadCodebase(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ProjectName string `json:"projectName"`
		WorkingDir  string `json:"workingDir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProjectName != "myapp" {
		t.Errorf("projectName: got %q", resp.ProjectName)
	}
	if b, err := os.ReadFile(filepath.Join(root, "myapp", "main.go")); err != nil || string(b) != "package main" {
		t.Errorf("extracted main.go: got %q err %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "myapp", "pkg", "util.go")); err != nil {
		t.Errorf("extracted nested file missing: %v", err)
	}
}

func TestOfficeUploadCodebaseConflict(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	root := t.TempDir()
	oldRoot := office.NewProjectRoot
	office.NewProjectRoot = root
	defer func() { office.NewProjectRoot = oldRoot }()

	// Pre-populate the target so the upload is refused.
	existing := filepath.Join(root, "taken")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, contentType := buildCodebaseUpload(t, "taken", map[string]string{"a.txt": "a"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/upload-codebase", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	ctx.OfficeUploadCodebase(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeUploadCodebaseInvalidName(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	body, contentType := buildCodebaseUpload(t, "../evil", map[string]string{"a.txt": "a"})
	req := httptest.NewRequest(http.MethodPost, "/api/office/upload-codebase", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	ctx.OfficeUploadCodebase(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeUploadCodebaseNotMultipart(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest(http.MethodPost, "/api/office/upload-codebase", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ctx.OfficeUploadCodebase(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
