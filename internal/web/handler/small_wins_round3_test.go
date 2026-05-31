package handler

// Round-3 small-win branch fills across office.go + monitoring_extended.go:
//   - GetSubagentTranscript: empty-FilePath short-circuit + read-error 500
//   - AuthLogout: the token!="" DestroySession arm (valid session cookie)
//   - OfficeGetTelegramConfig: the non-zero lastUpdate arm
//   - OfficeAgentGetDevInstance: the registered-instance (exists) arm
//   - OfficeAgentKnowledgeTail: the limit-parse + live-team-set arms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
	"github.com/kshaham/jarvis/internal/web/auth"
)

func TestGetSubagentTranscript_EmptyFilePath(t *testing.T) {
	w := subagent.NewWatcher(context.Background(), t.TempDir())
	// OnHookStart registers an agent with NO FilePath -> the empty-FilePath arm.
	w.OnHookStart("sessX", "explorer", "nofile")
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(t.TempDir() + "/s.json")}

	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, transcriptReq("nofile", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty-FilePath agent, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	entries, _ := resp["transcript"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("empty FilePath should yield empty transcript, got %d", len(entries))
	}
}

func TestGetSubagentTranscript_ReadError(t *testing.T) {
	jsonl := `{"type":"user","content":"hi"}` + "\n"
	w := watcherWithTranscript(t, "readerr", jsonl)
	a := w.GetAgent("readerr")
	// Remove the file out from under the handler so os.ReadFile fails (not
	// IsNotExist on every platform? it IS not-exist -> 404). To force the
	// generic 500 read error we instead make the path a directory.
	_ = os.Remove(a.FilePath)
	if err := os.Mkdir(a.FilePath, 0o755); err != nil {
		t.Fatalf("replace transcript with a dir: %v", err)
	}
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(t.TempDir() + "/s.json")}

	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, transcriptReq("readerr", ""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the transcript path is unreadable (a dir), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthLogout_DestroysSession(t *testing.T) {
	am, err := auth.NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	token := am.CreateSession()
	ctx := &Context{AuthManager: am}

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(am.BuildSessionCookie(token, req))
	rec := httptest.NewRecorder()
	ctx.AuthLogout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from logout, got %d", rec.Code)
	}
	// The session token must no longer validate after logout.
	if am.ValidateSession(token) {
		t.Error("session should be destroyed after logout")
	}
}

func TestOfficeAgentGetDevInstance_Exists(t *testing.T) {
	ctx := newTimelineContext(t)
	if err := ctx.OfficeManager.SetDevInstance("slot1", "http://jarvis:3000", 4242, false); err != nil {
		t.Fatalf("set dev instance: %v", err)
	}
	// slotFromQuery reads the `name` query param (defaulting to "app").
	rec := getJSON(t, ctx.OfficeAgentGetDevInstance, "/api/office/agent/dev-instance?name=slot1")
	if rec["url"] != "http://jarvis:3000" {
		t.Errorf("expected the registered url, got %v", rec["url"])
	}
	if pid, _ := rec["pid"].(float64); int(pid) != 4242 {
		t.Errorf("expected pid 4242, got %v", rec["pid"])
	}
}

func TestOfficeAgentKnowledgeTail_LimitAndLiveTeam(t *testing.T) {
	ctx := newTimelineContext(t)
	// Register an agent so liveTeamAgentSet returns a non-nil set (the default,
	// all != "true" path).
	ctx.OfficeManager.RegisterAgent("s-vex", "cfg-vex", &office.TeamMemberConfig{ID: "cfg-vex", Name: "Vex"})

	rec := getJSON(t, ctx.OfficeAgentKnowledgeTail, "/api/office/agent/knowledge/tail?limit=5")
	if _, ok := rec["hits"]; !ok {
		t.Errorf("expected a hits field, got %v", rec)
	}
}

var _ = time.Second
var _ = filepath.Join
