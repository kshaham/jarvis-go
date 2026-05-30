package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// newInvariantsCtx builds an isolated Context for invariants handler tests.
func newInvariantsCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
}

// jsonBody serialises v into a *bytes.Reader for use in httptest requests.
func jsonBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// decodeMap decodes the JSON response body into a map.
func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return out
}

// invariantsFromResp extracts the "invariants" slice from a decoded response map.
func invariantsFromResp(t *testing.T, m map[string]interface{}) []string {
	t.Helper()
	raw, ok := m["invariants"]
	if !ok {
		t.Fatalf("response missing 'invariants' key: %v", m)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("'invariants' is not an array: %T %v", raw, raw)
	}
	out := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("invariant[%d] is not a string: %T", i, v)
		}
		out[i] = s
	}
	return out
}

// TestInvariantsHandlersRoundTrip exercises the four invariants handlers.
func TestInvariantsHandlersRoundTrip(t *testing.T) {
	ctx := newInvariantsCtx(t)

	// --- PUT replaces the full list ---
	putReq := httptest.NewRequest(http.MethodPut, "/api/office/project/invariants",
		jsonBody(t, map[string]interface{}{"invariants": []string{"r1", "r2"}}))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	ctx.OfficePutProjectInvariants(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT invariants: want 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	invs := invariantsFromResp(t, decodeMap(t, putRec))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("PUT response invariants = %v, want [r1 r2]", invs)
	}

	// --- GET returns the same list ---
	getRec := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET invariants: want 200, got %d", getRec.Code)
	}
	invs = invariantsFromResp(t, decodeMap(t, getRec))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("GET invariants = %v, want [r1 r2]", invs)
	}

	// --- POST adds a third entry ---
	postReq := httptest.NewRequest(http.MethodPost, "/api/office/project/invariants",
		jsonBody(t, map[string]interface{}{"text": "r3"}))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	ctx.OfficePostProjectInvariant(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST invariant: want 200, got %d: %s", postRec.Code, postRec.Body.String())
	}
	invs = invariantsFromResp(t, decodeMap(t, postRec))
	if len(invs) != 3 {
		t.Fatalf("after POST: want 3 invariants, got %d: %v", len(invs), invs)
	}
	if !strings.Contains(strings.Join(invs, ","), "r3") {
		t.Fatalf("after POST: r3 not in list: %v", invs)
	}

	// --- GET confirms 3 entries ---
	getRec2 := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec2, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	invs = invariantsFromResp(t, decodeMap(t, getRec2))
	if len(invs) != 3 {
		t.Fatalf("GET after POST: want 3, got %d", len(invs))
	}

	// --- DELETE removes index 2 (r3), leaving r1, r2 ---
	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "2")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	delRec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE invariant: want 200, got %d: %s", delRec.Code, delRec.Body.String())
	}
	invs = invariantsFromResp(t, decodeMap(t, delRec))
	if len(invs) != 2 {
		t.Fatalf("after DELETE: want 2 invariants, got %d: %v", len(invs), invs)
	}
	if invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("after DELETE: want [r1 r2], got %v", invs)
	}

	// --- Final GET confirms state ---
	getRec3 := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec3, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	invs = invariantsFromResp(t, decodeMap(t, getRec3))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("final GET: want [r1 r2], got %v", invs)
	}
}

// TestOfficeDeleteProjectInvariant_OutOfRange checks 400 for bad index.
func TestOfficeDeleteProjectInvariant_OutOfRange(t *testing.T) {
	ctx := newInvariantsCtx(t)

	// DELETE with index 5 on empty list => 400
	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/5", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "5")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(rec, delReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range index, got %d", rec.Code)
	}
}

// TestOfficeDeleteProjectInvariant_ParseError checks 400 for non-numeric index.
func TestOfficeDeleteProjectInvariant_ParseError(t *testing.T) {
	ctx := newInvariantsCtx(t)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/notanumber", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "notanumber")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(rec, delReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric index, got %d", rec.Code)
	}
}
