package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/library"
)

// newKnowledgeContext builds a handler Context with both the
// library root and HOME pointed at temp dirs. Sharing
// newTimelineContext's stub session store + broker keeps the
// handler test surface uniform.
func newKnowledgeContext(t *testing.T) *Context {
	t.Helper()
	// Temp HOME so shared-skills writes land under the tempdir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Independent library root (don't use HOME-derived path so the
	// test isn't tied to library.Dir's HOME lookup quirks).
	root := t.TempDir()
	for _, sub := range []string{"experiences", "workflows", "standups", "experiments"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)

	ctx := newTimelineContext(t)
	return ctx
}

func TestGetKnowledgeStats_EmptyLibraryReturnsZeroCounts(t *testing.T) {
	ctx := newKnowledgeContext(t)
	req := httptest.NewRequest("GET", "/api/knowledge/stats", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"experiences", "workflows", "standups", "experiments"} {
		if v, ok := resp[k].(float64); !ok || v != 0 {
			t.Errorf("%s=%v want 0", k, resp[k])
		}
	}
	if _, ok := resp["libraryDir"]; !ok {
		t.Error("stats missing libraryDir field")
	}
}

func TestGetKnowledgeStats_CountsReflectFiles(t *testing.T) {
	ctx := newKnowledgeContext(t)
	// Seed 2 experiences + 1 workflow.
	now := time.Now()
	for i := 0; i < 2; i++ {
		_ = library.WriteExperience(library.ExperienceRecord{
			AgentConfigID:    "vex",
			TaskID:           "t-" + itoaForTest(i),
			TaskTitle:        "x",
			CompletionStatus: "completed",
			OutcomeScore:     0.9,
			CompletedAt:      now,
		})
	}
	_ = library.WriteWorkflow(library.Workflow{
		Slug:       "auth",
		Title:      "Auth",
		CreatedAt:  now,
		AvgOutcome: 0.9,
	})

	req := httptest.NewRequest("GET", "/api/knowledge/stats", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["experiences"].(float64) != 2 {
		t.Errorf("experiences=%v want 2", resp["experiences"])
	}
	if resp["workflows"].(float64) != 1 {
		t.Errorf("workflows=%v want 1", resp["workflows"])
	}
}

func TestGetKnowledgePromotions_EmptyLedger(t *testing.T) {
	ctx := newKnowledgeContext(t)
	req := httptest.NewRequest("GET", "/api/knowledge/promotions", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgePromotions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Entries []library.LedgerEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries=%d want 0", len(resp.Entries))
	}
}

func TestGetKnowledgePromotions_ReturnsLedgerEntries(t *testing.T) {
	ctx := newKnowledgeContext(t)
	ledger, _ := library.OpenLedger()
	_ = ledger.Increment("workflows/auth.md", 0.85)
	_ = ledger.Increment("workflows/auth.md", 0.90)

	req := httptest.NewRequest("GET", "/api/knowledge/promotions", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgePromotions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp struct {
		Entries []library.LedgerEntry `json:"entries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("entries=%d want 1", len(resp.Entries))
	}
	if resp.Entries[0].ReuseCount != 2 {
		t.Errorf("ReuseCount=%d want 2", resp.Entries[0].ReuseCount)
	}
}

func TestGetKnowledgeLibrary_ReadsFile(t *testing.T) {
	ctx := newKnowledgeContext(t)
	// Seed one experience.
	_ = library.WriteExperience(library.ExperienceRecord{
		AgentConfigID:    "vex",
		TaskID:           "task-1",
		TaskTitle:        "Read me back",
		CompletionStatus: "completed",
		OutcomeScore:     0.8,
		CompletedAt:      time.Now(),
		ApproachSummary:  "Did stuff.",
	})

	q := url.Values{}
	q.Set("path", "experiences/vex/task-1.md")
	req := httptest.NewRequest("GET", "/api/knowledge/library?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["content"].(string), "Read me back") {
		t.Errorf("content missing seeded title; got: %s", resp["content"])
	}
	if resp["path"].(string) != "experiences/vex/task-1.md" {
		t.Errorf("path=%v want experiences/vex/task-1.md", resp["path"])
	}
}

func TestGetKnowledgeLibrary_MissingPathReturns400(t *testing.T) {
	ctx := newKnowledgeContext(t)
	req := httptest.NewRequest("GET", "/api/knowledge/library", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

func TestGetKnowledgeLibrary_TraversalReturns403(t *testing.T) {
	ctx := newKnowledgeContext(t)
	q := url.Values{}
	q.Set("path", "../../etc/passwd")
	req := httptest.NewRequest("GET", "/api/knowledge/library?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetKnowledgeLibrary_AbsolutePathReturns403(t *testing.T) {
	ctx := newKnowledgeContext(t)
	q := url.Values{}
	q.Set("path", "/etc/passwd")
	req := httptest.NewRequest("GET", "/api/knowledge/library?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rec.Code)
	}
}

func TestGetKnowledgeLibrary_MissingFileReturns404(t *testing.T) {
	ctx := newKnowledgeContext(t)
	q := url.Values{}
	q.Set("path", "experiences/ghost/task.md")
	req := httptest.NewRequest("GET", "/api/knowledge/library?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rec.Code)
	}
}

func TestPostKnowledgeRevoke_MarksLedger(t *testing.T) {
	ctx := newKnowledgeContext(t)
	ledger, _ := library.OpenLedger()
	_ = ledger.Increment("workflows/auth.md", 0.85)

	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke", map[string]interface{}{
		"path":   "workflows/auth.md",
		"reason": "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Reload the ledger so we read fresh on-disk state.
	fresh, _ := library.OpenLedger()
	e, _ := fresh.Get("workflows/auth.md")
	if e == nil {
		t.Fatal("entry missing after revoke")
	}
	if e.RevokedAt.IsZero() {
		t.Errorf("RevokedAt not stamped: %+v", e)
	}
	if e.RevokedReason != "test" {
		t.Errorf("RevokedReason=%q want test", e.RevokedReason)
	}
}

func TestPostKnowledgeRevoke_MissingPathReturns400(t *testing.T) {
	ctx := newKnowledgeContext(t)
	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke",
		map[string]interface{}{"reason": "no path"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

func TestPostKnowledgeRevoke_DefaultsReason(t *testing.T) {
	ctx := newKnowledgeContext(t)
	ledger, _ := library.OpenLedger()
	_ = ledger.Increment("workflows/x.md", 0.8)

	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke",
		map[string]interface{}{"path": "workflows/x.md"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	fresh, _ := library.OpenLedger()
	e, _ := fresh.Get("workflows/x.md")
	if e == nil || e.RevokedReason == "" {
		t.Errorf("default reason not stamped: %+v", e)
	}
}

func TestIsWorkflowPathLedger(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"workflows/auth.md", true},
		{"workflows/", false}, // empty slug
		{"experiences/vex/task.md", false},
		{"standups/abc.md", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isWorkflowPathLedger(tc.in); got != tc.want {
			t.Errorf("isWorkflowPathLedger(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestWorkflowSlugFromLedgerPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"workflows/auth.md", "auth"},
		{"workflows/jwt-refresh.md", "jwt-refresh"},
		{"workflows/noext", "noext"},
	}
	for _, tc := range cases {
		if got := workflowSlugFromLedgerPath(tc.in); got != tc.want {
			t.Errorf("workflowSlugFromLedgerPath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// itoaForTest is a tiny stand-alone helper since this file's
// pattern doesn't share with the existing helper namespace.
func itoaForTest(i int) string {
	if i == 0 {
		return "0"
	}
	var s string
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
