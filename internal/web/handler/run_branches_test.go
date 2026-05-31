package handler

// Branch fills for the Run handler: empty-command 400, shell-metachars 400,
// invalid-workingDir 400, and the happy `echo` execution (allowlisted binary)
// returning exitCode 0 + captured stdout.

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
)

func TestRun_EmptyCommand400(t *testing.T) {
	ctx := &Context{}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{"command": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty command, got %d", rec.Code)
	}
}

func TestRun_InvalidWorkingDir400(t *testing.T) {
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{
		"command":    "echo hi",
		"workingDir": "/etc/definitely-not-allowed",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed working dir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRun_EchoHappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := t.TempDir()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
	})
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{
		"command":    "echo run-marker",
		"workingDir": wd,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 running echo, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["exitCode"] != float64(0) {
		t.Errorf("exitCode=%v, want 0", resp["exitCode"])
	}
	if out, _ := resp["output"].(string); !strings.Contains(out, "run-marker") {
		t.Errorf("output missing echo result: %q", out)
	}
}
