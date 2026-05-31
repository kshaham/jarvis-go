package handler

// Branch fills for GetSubagent (agent-found arm) and GetSessionSubagents
// (the matched-agent arm where an agent's transcript path lives under the
// session's working dir).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
)

func TestGetSubagent_Found(t *testing.T) {
	w := watcherWithTranscript(t, "found-1", `{"type":"user"}`+"\n")
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(t.TempDir() + "/s.json")}

	req := withURLParam(httptest.NewRequest("GET", "/api/subagents/found-1", nil), "agentId", "found-1")
	rec := httptest.NewRecorder()
	ctx.GetSubagent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for found agent, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	agent, _ := resp["agent"].(map[string]interface{})
	if agent["agentId"] != "found-1" {
		t.Errorf("agent.agentId=%v, want found-1", agent["agentId"])
	}
}

func TestGetSessionSubagents_MatchesByWorkingDir(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())

	// projectsDir layout: <projectsDir>/<proj>/<sess>/subagents/agent-<id>.jsonl
	// The session's working dir is set to the same <proj>/<sess> prefix so the
	// agent's FilePath contains it and the match branch fires.
	projectsDir := t.TempDir()
	sessDir := filepath.Join(projectsDir, "projX", "sessY")
	subDir := filepath.Join(sessDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fp := filepath.Join(subDir, "agent-match-1.jsonl")
	if err := os.WriteFile(fp, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	watcher := subagent.NewWatcher(context.Background(), projectsDir)
	watcher.Start()
	t.Cleanup(watcher.Stop)
	// Wait for the scan to discover the agent with its FilePath.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := watcher.GetAgent("match-1"); a != nil && a.FilePath != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx := &Context{
		SessionManager:  pty.NewSessionManager(10),
		StateStore:      state.NewStore(t.TempDir() + "/s.json"),
		SubagentWatcher: watcher,
	}
	// A real session rooted at sessDir so its WorkingDir is contained in the
	// agent's FilePath.
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: sessDir, Mode: pty.ModeShell, Name: "subs",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+sess.ID+"/subagents", nil), "id", sess.ID)
	rec := httptest.NewRecorder()
	ctx.GetSessionSubagents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	subs, _ := resp["subagents"].([]interface{})
	if len(subs) != 1 {
		t.Fatalf("expected 1 matched subagent, got %d: %v", len(subs), subs)
	}
}
