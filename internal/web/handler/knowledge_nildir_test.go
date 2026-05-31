package handler

// Branch fills for the knowledge handlers when the library directory is
// unavailable (library.Dir() == ""). This drives:
//   - GetKnowledgePromotions: OpenLedger() returns (nil,nil) -> empty entries
//   - GetKnowledgeLibrary: root == "" -> 503
//   - PostKnowledgeRevoke: ledger == nil -> 503 library unavailable

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

func nilLibraryCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	restore := library.SetDirForTest("")
	t.Cleanup(restore)
	return newTimelineContext(t)
}

func TestGetKnowledgePromotions_NilLedgerEmptyEntries(t *testing.T) {
	ctx := nilLibraryCtx(t)
	req := httptest.NewRequest("GET", "/api/knowledge/promotions", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgePromotions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil ledger, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("expected empty entries with nil ledger, got %d", len(entries))
	}
}

func TestGetKnowledgeLibrary_NoRoot503(t *testing.T) {
	ctx := nilLibraryCtx(t)
	req := httptest.NewRequest("GET", "/api/knowledge/library?path=foo.md", nil)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when library dir unavailable, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostKnowledgeRevoke_NilLedger503(t *testing.T) {
	ctx := nilLibraryCtx(t)
	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke", map[string]interface{}{
		"path": "workflows/auth.md", "reason": "stale",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ledger unavailable, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostKnowledgeRevoke_MissingPath400(t *testing.T) {
	ctx := nilLibraryCtx(t)
	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke", map[string]interface{}{
		"reason": "no path",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing path, got %d: %s", rec.Code, rec.Body.String())
	}
}
