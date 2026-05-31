package handler

// Happy-path coverage for the core session.go handlers using a real shell
// session created THROUGH the CreateSession HTTP handler (which also spins up
// forwardSessionEvents). Exercises get/input/terminal/delete and the bulk
// operations against live sessions. The working dir is allowlisted via
// settings so ResolveWorkingDir accepts the test's temp dir.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/integrations"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
)

func newSessionLiveCtx(t *testing.T) *Context {
	t.Helper()
	ctx := newSessionExtCtx(t)
	// forwardSessionEvents dereferences ctx.OfficeManager.GetAgent -- wire one
	// up so the goroutine doesn't nil-panic on the shell's output events.
	ctx.OfficeManager = office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	// Give the forwarder goroutines a beat to drain before TempDir cleanup.
	t.Cleanup(func() { time.Sleep(100 * time.Millisecond) })
	return ctx
}

// allowDir marks dir as an allowed working directory so CreateSession's
// ResolveWorkingDir accepts it.
func allowDir(ctx *Context, dir string) {
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{dir},
	})
}

func TestCreateSession_HappyThroughHandler(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	allowDir(ctx, dir)

	rec := postJSON(t, ctx.CreateSession, "/api/sessions", map[string]interface{}{
		"workingDir": dir, "mode": "shell", "name": "happy",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sess, _ := resp["session"].(map[string]interface{})
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("expected a session id in the create response")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		ctx.cleanupSessionResources(id)
	})

	// The session must be registered in the manager.
	if _, ok := ctx.SessionManager.GetSession(id); !ok {
		t.Fatal("created session not registered in manager")
	}
	// A respawn controller was installed.
	ctx.RespawnMu.RLock()
	_, hasRC := ctx.RespawnControllers[id]
	ctx.RespawnMu.RUnlock()
	if !hasRC {
		t.Error("expected a respawn controller for the new session")
	}

	// GetSession returns tokens + the session struct.
	recGet := httptest.NewRecorder()
	ctx.GetSession(recGet, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id, nil), "id", id))
	if recGet.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", recGet.Code)
	}
	var getResp map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &getResp)
	if _, ok := getResp["tokens"]; !ok {
		t.Error("expected tokens block in GetSession response")
	}

	// ListSessions includes it.
	recList := httptest.NewRecorder()
	ctx.ListSessions(recList, httptest.NewRequest("GET", "/api/sessions", nil))
	var arr []map[string]interface{}
	_ = json.Unmarshal(recList.Body.Bytes(), &arr)
	found := false
	for _, s := range arr {
		if s["id"] == id {
			found = true
		}
	}
	if !found {
		t.Error("created session not present in ListSessions")
	}

	// SessionInput writes to the shell.
	recIn := httptest.NewRecorder()
	inReq := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+id+"/input", strings.NewReader(`{"input":"echo hi\n"}`)), "id", id)
	inReq.Header.Set("Content-Type", "application/json")
	ctx.SessionInput(recIn, inReq)
	if recIn.Code != http.StatusOK {
		t.Fatalf("input: expected 200, got %d", recIn.Code)
	}

	// GetSessionTerminal returns the buffer.
	recTerm := httptest.NewRecorder()
	ctx.GetSessionTerminal(recTerm, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/terminal", nil), "id", id))
	if recTerm.Code != http.StatusOK {
		t.Fatalf("terminal: expected 200, got %d", recTerm.Code)
	}
	var termResp map[string]interface{}
	_ = json.Unmarshal(recTerm.Body.Bytes(), &termResp)
	if _, ok := termResp["terminalBuffer"]; !ok {
		t.Error("expected terminalBuffer in terminal response")
	}

	// GetSessionTerminal with a tail param.
	recTail := httptest.NewRecorder()
	ctx.GetSessionTerminal(recTail, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/terminal?tail=10", nil), "id", id))
	if recTail.Code != http.StatusOK {
		t.Fatalf("terminal tail: expected 200, got %d", recTail.Code)
	}

	// DeleteSession stops + removes it.
	recDel := httptest.NewRecorder()
	ctx.DeleteSession(recDel, withURLParam(httptest.NewRequest("DELETE", "/api/sessions/"+id, nil), "id", id))
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", recDel.Code, recDel.Body.String())
	}
	if _, ok := ctx.SessionManager.GetSession(id); ok {
		t.Error("session should be gone after delete")
	}
}

func TestSessionInputAndShell_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)

	// RunSession / SessionInteractive / SessionShell all write to the shell.
	for _, h := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"run", ctx.RunSession},
		{"interactive", ctx.SessionInteractive},
		{"shell", ctx.SessionShell},
		{"trim", ctx.TrimSessionContext},
	} {
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+id+"/x", strings.NewReader(`{"input":"true"}`)), "id", id)
		req.Header.Set("Content-Type", "application/json")
		h.handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", h.name, rec.Code)
		}
	}
}

func TestGetSessionActiveTools_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionActiveTools(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/active-tools", nil), "id", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("active-tools: expected 200, got %d", rec.Code)
	}
}

func TestGetSessionIntegrations_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	ctx.IntegrationMonitor = integrations.NewMonitor()
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionIntegrations(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/integrations", nil), "id", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("integrations: expected 200, got %d", rec.Code)
	}
}

func TestSessionStatusEndpoints_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	// build/test/hmr status read from settings; they only need a valid session.
	for _, h := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"build-status", ctx.GetSessionBuildStatus},
		{"test-status", ctx.GetSessionTestStatus},
		{"hmr-status", ctx.GetSessionHMRStatus},
	} {
		rec := httptest.NewRecorder()
		h.handler(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/"+h.name, nil), "id", id))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", h.name, rec.Code)
		}
	}
}

func TestBulkOps_LiveHappy(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id1, _ := liveSession(t, ctx)
	id2, _ := liveSession(t, ctx)

	// Bulk rename both.
	recR := postJSON(t, ctx.BulkRenameSessions, "/api/sessions/bulk-rename", map[string]interface{}{
		"renames": map[string]string{id1: "alpha", id2: "beta"},
	})
	if recR.Code != http.StatusOK {
		t.Fatalf("bulk rename: expected 200, got %d", recR.Code)
	}
	respR := decodeMap(t, recR)
	renamed, _ := respR["renamed"].([]interface{})
	if len(renamed) != 2 {
		t.Errorf("expected 2 renamed, got %d", len(renamed))
	}

	// Bulk color both.
	recC := postJSON(t, ctx.BulkUpdateSessionColors, "/api/sessions/bulk-color", map[string]interface{}{
		"colors": map[string]string{id1: "red", id2: "green"},
	})
	if recC.Code != http.StatusOK {
		t.Fatalf("bulk color: expected 200, got %d", recC.Code)
	}
	respC := decodeMap(t, recC)
	updated, _ := respC["updated"].([]interface{})
	if len(updated) != 2 {
		t.Errorf("expected 2 updated colors, got %d", len(updated))
	}

	// Bulk delete both.
	recD := postJSON(t, ctx.BulkDeleteSessions, "/api/sessions/bulk-delete", map[string]interface{}{
		"sessionIds": []string{id1, id2},
	})
	if recD.Code != http.StatusOK {
		t.Fatalf("bulk delete: expected 200, got %d", recD.Code)
	}
	respD := decodeMap(t, recD)
	deleted, _ := respD["deleted"].([]interface{})
	if len(deleted) != 2 {
		t.Errorf("expected 2 deleted, got %d", len(deleted))
	}
}

// sessionToJSON should reflect a live session's fields.
func TestSessionToJSON_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	sess, _ := ctx.SessionManager.GetSession(id)
	j := sessionToJSON(sess)
	if j.ID != id {
		t.Errorf("id=%q, want %q", j.ID, id)
	}
	if j.WorkingDir != wd {
		t.Errorf("workingDir=%q, want %q", j.WorkingDir, wd)
	}
	if j.Mode != string(pty.ModeShell) {
		t.Errorf("mode=%q, want %q", j.Mode, pty.ModeShell)
	}
}
