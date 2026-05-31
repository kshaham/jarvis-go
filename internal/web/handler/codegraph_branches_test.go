package handler

// Branch fills for buildCodeGraph / OfficePostCodeGraphImpact: the
// session-present arm where a live session's working dir roots the analysis.

import (
	"context"
	"net/http"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestOfficeGetCodeGraph_WithLiveSession(t *testing.T) {
	ctx := newTimelineContext(t)
	wd := t.TempDir()
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: wd, Mode: pty.ModeShell, Name: "cg",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	rec := getJSON(t, ctx.OfficeGetCodeGraph, "/api/office/codegraph")
	if rec["available"] != true {
		t.Errorf("expected available=true with a live session, got %v", rec["available"])
	}
}

func TestOfficePostCodeGraphImpact_WithLiveSession(t *testing.T) {
	ctx := newTimelineContext(t)
	wd := t.TempDir()
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: wd, Mode: pty.ModeShell, Name: "cg-impact",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	rec := postJSON(t, ctx.OfficePostCodeGraphImpact, "/api/office/codegraph/impact", map[string]interface{}{
		"files": []string{"main.go", "handler.go"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if _, ok := resp["files"]; !ok {
		t.Errorf("expected files key in impact response, got %v", resp)
	}
}
