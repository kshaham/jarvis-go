package handler

// Coverage for GetSubagentTranscript's body once an agent with a real
// transcript file is registered. The watcher's initial scan (run on Start)
// discovers a JSONL file laid out at
//   <projectsDir>/<proj>/<sess>/subagents/agent-<id>.jsonl
// and backfills the agent's FilePath, so the handler reads + parses it.
// Drives the structured-parse path, the format=raw path, the unparseable-line
// fallback, and the empty-FilePath short-circuit.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
)

// watcherWithTranscript builds a started watcher whose initial scan discovers a
// transcript file for agentID containing the given JSONL lines.
func watcherWithTranscript(t *testing.T, agentID, jsonl string) *subagent.Watcher {
	t.Helper()
	projectsDir := t.TempDir()
	subDir := filepath.Join(projectsDir, "projA", "sessB", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fp := filepath.Join(subDir, "agent-"+agentID+".jsonl")
	if err := os.WriteFile(fp, []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	w := subagent.NewWatcher(context.Background(), projectsDir)
	w.Start() // runs scanForAgents() immediately, registering the agent w/ FilePath
	t.Cleanup(w.Stop)

	// The scan is synchronous inside Start's goroutine; poll briefly for the
	// agent to appear with its FilePath backfilled.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := w.GetAgent(agentID); a != nil && a.FilePath != "" {
			return w
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent %s not discovered with FilePath within window", agentID)
	return nil
}

func transcriptReq(agentID, query string) *http.Request {
	url := "/api/subagents/" + agentID + "/transcript"
	if query != "" {
		url += "?" + query
	}
	return withURLParam(httptest.NewRequest("GET", url, nil), "agentId", agentID)
}

func TestGetSubagentTranscript_StructuredParse(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"user","content":"hello"}
{"type":"assistant","content":"hi there"}
not-valid-json-line
`
	w := watcherWithTranscript(t, "abc", jsonl)
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(t.TempDir() + "/s.json")}

	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, transcriptReq("abc", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries, _ := resp["transcript"].([]interface{})
	if len(entries) != 3 {
		t.Fatalf("expected 3 transcript entries (2 parsed + 1 raw), got %d: %v", len(entries), entries)
	}
	// The unparseable line should surface as a {type:raw,text:...} entry.
	last, _ := entries[2].(map[string]interface{})
	if last["type"] != "raw" || last["text"] != "not-valid-json-line" {
		t.Errorf("expected raw fallback entry for bad JSON line, got %v", last)
	}
	if resp["raw"] == "" {
		t.Error("expected non-empty raw content")
	}
}

func TestGetSubagentTranscript_FormatRaw(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"user","content":"hello"}
`
	w := watcherWithTranscript(t, "rawid", jsonl)
	ctx := &Context{SubagentWatcher: w, StateStore: state.NewStore(t.TempDir() + "/s.json")}

	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, transcriptReq("rawid", "format=raw"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	// format=raw returns an empty structured transcript and the raw text.
	entries, _ := resp["transcript"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("format=raw should return empty structured transcript, got %d", len(entries))
	}
	raw, _ := resp["raw"].(string)
	if raw == "" {
		t.Error("format=raw should return non-empty raw text")
	}
}
