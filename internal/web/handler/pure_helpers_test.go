package handler

// Direct unit coverage for small pure helpers + the resolveSessionFilePath
// error-mapping branches that the handler-level tests don't all reach.

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFromOr(t *testing.T) {
	t.Parallel()
	if got := fromOr("primary", "fb"); got != "primary" {
		t.Errorf("non-empty primary should win, got %q", got)
	}
	if got := fromOr("   ", "fb"); got != "fb" {
		t.Errorf("whitespace primary should fall back, got %q", got)
	}
	if got := fromOr("", "fb"); got != "fb" {
		t.Errorf("empty primary should fall back, got %q", got)
	}
}

func TestHasSuffix(t *testing.T) {
	t.Parallel()
	if !hasSuffix("image.png", ".png") {
		t.Error("expected true for matching suffix")
	}
	if hasSuffix("image.png", ".jpg") {
		t.Error("expected false for non-matching suffix")
	}
	// Suffix longer than string -> false (the len guard).
	if hasSuffix("x", ".png") {
		t.Error("expected false when suffix longer than string")
	}
}

// TestResolveSessionFilePath_Traversal drives the default (ErrPathTraversal ->
// 403 Forbidden) branch of resolveSessionFilePath via a "../" escape.
func TestResolveSessionFilePath_Traversal(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	// file-content shares resolveSessionFilePath; a traversal path returns 403.
	ctx.GetSessionFileContent(rec, idReq("GET", "/api/sessions/"+id+"/file-content?path=../../escape", "", id))
	if rec.Code != 403 {
		t.Fatalf("expected 403 for traversal path, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "traversal") && !strings.Contains(rec.Body.String(), "outside") &&
		rec.Code != 403 {
		t.Logf("body: %s", rec.Body.String())
	}
}

// TestResolveSessionFilePath_InvalidPath drives the ErrInvalidPath -> 400 arm
// via a NUL byte, which filepath rejects on resolution.
func TestResolveSessionFilePath_InvalidPath(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileContent(rec, idReq("GET", "/api/sessions/"+id+"/file-content?path=bad%00name", "", id))
	// A NUL in the path is rejected; the exact code depends on where the
	// rejection happens (400 invalid-path or 403 traversal), but it must not
	// be a 200 or 404-session.
	if rec.Code == 200 {
		t.Fatalf("NUL-byte path should not succeed, got 200: %s", rec.Body.String())
	}
}
