package handler

// Coverage for QuerySessions filtering / sorting / pagination across live
// shell sessions, plus the spawn_remote error-wrapper helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestQuerySessions_FilterSortPaginate_Live(t *testing.T) {
	ctx := newSessionLiveCtx(t)

	// Create three named live sessions.
	make3 := func(name string) string {
		s, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
			WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: name,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), s.ID) })
		return s.ID
	}
	make3("alpha")
	make3("beta")
	make3("gamma")

	// Search filter (name contains "alph") + name sort asc.
	q := func(query string) map[string]interface{} {
		req := httptest.NewRequest("GET", "/api/sessions/query?"+query, nil)
		rec := httptest.NewRecorder()
		ctx.QuerySessions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q: expected 200, got %d", query, rec.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	// Search narrows to one.
	respSearch := q("search=alph&sort=name&order=asc")
	if respSearch["total"] != float64(1) {
		t.Errorf("search total=%v, want 1", respSearch["total"])
	}

	// No filter returns all three; pagination with limit=2 -> hasMore true.
	respPage := q("limit=2&offset=0&sort=name&order=desc")
	if respPage["total"] != float64(3) {
		t.Errorf("total=%v, want 3", respPage["total"])
	}
	if respPage["hasMore"] != true {
		t.Errorf("hasMore=%v, want true with limit 2 of 3", respPage["hasMore"])
	}
	sessions, _ := respPage["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Errorf("page size=%d, want 2", len(sessions))
	}

	// Offset past the end yields an empty page.
	respPast := q("limit=10&offset=99")
	emptyPage, _ := respPast["sessions"].([]interface{})
	if len(emptyPage) != 0 {
		t.Errorf("offset past end should be empty, got %d", len(emptyPage))
	}

	// Status filter that matches nothing -> total 0.
	respStatus := q("status=definitely-not-a-status")
	if respStatus["total"] != float64(0) {
		t.Errorf("bogus status total=%v, want 0", respStatus["total"])
	}
}

// --- spawn_remote error wrapper helpers (pure) ---

func TestSpawnRemoteCLIError_ErrorAndUnwrap(t *testing.T) {
	base := errors.New("boom")

	// With stderr.
	e := &spawnRemoteCLIError{Err: base, Stderr: "stack trace"}
	msg := e.Error()
	if !strings.Contains(msg, "boom") || !strings.Contains(msg, "stderr=stack trace") {
		t.Errorf("error string missing parts: %q", msg)
	}

	// Without stderr.
	e2 := &spawnRemoteCLIError{Err: base}
	if strings.Contains(e2.Error(), "stderr=") {
		t.Errorf("no-stderr error should omit stderr clause: %q", e2.Error())
	}

	// Unwrap exposes the base error for errors.Is.
	if !errors.Is(e, base) {
		t.Error("errors.Is should find the wrapped base error via Unwrap")
	}
}

func TestTruncateLog(t *testing.T) {
	short := "short log"
	if got := truncateLog(short); got != short {
		t.Errorf("short log should pass through, got %q", got)
	}
	long := strings.Repeat("x", 3000)
	got := truncateLog(long)
	if len(got) >= len(long) || !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("long log not truncated: len=%d", len(got))
	}
}
