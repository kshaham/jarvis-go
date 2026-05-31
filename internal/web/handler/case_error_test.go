package handler

// Error-branch coverage for CreateCase / DeleteCase. An invalid case name
// (one IsValidCaseName rejects) makes CaseManager.Create / Delete return an
// error, driving the 500 paths the happy-path tests don't reach.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestCreateCase_ManagerError500(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	// "../escape" is non-empty (passes the handler's name!="" guard) but
	// IsValidCaseName rejects the ".." form, so Create returns an error.
	rec := postJSON(t, ctx.CreateCase, "/api/cases", map[string]interface{}{"name": "../escape"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for invalid case name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteCase_ManagerError500(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/cases/..%2Fevil", nil), "name", "../evil")
	rec := httptest.NewRecorder()
	ctx.DeleteCase(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for invalid case name on delete, got %d: %s", rec.Code, rec.Body.String())
	}
}
