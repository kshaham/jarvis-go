package handler

// Round-2 batch: bad-JSON + IO-error + edge arms across auth/knowledge/office/
// template/report handlers. Real fault injection (rate-limit priming, corrupt
// auth.json, read-only reports dir, oversize/dir-target library files, corrupt
// ledger) -- no mocks.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/peers"
)

// --- AuthChangePassword edge arms ---

func TestAuthChangePassword_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtxAt(t, t.TempDir())
	_ = ctx.AuthManager.SetPassword("currentpass123")
	req := httptest.NewRequest("POST", "/api/auth/change-password", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.AuthChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestAuthChangePassword_OversizeCurrentPassword400(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtxAt(t, t.TempDir())
	_ = ctx.AuthManager.SetPassword("currentpass123")
	rec := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"currentPassword": strings.Repeat("x", 129),
		"newPassword":     "newstrongpass1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >128 char current password, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthChangePassword_RateLimited429(t *testing.T) {
	t.Parallel()
	ctx := newAuthCtxAt(t, t.TempDir())
	_ = ctx.AuthManager.SetPassword("currentpass123")
	// Prime the rate limiter for the test's client IP (httptest default).
	ip := "192.0.2.50:1234"
	for i := 0; i < 12; i++ {
		ctx.AuthManager.RecordFailedAttempt("192.0.2.50")
	}
	req := httptest.NewRequest("POST", "/api/auth/change-password",
		strings.NewReader(`{"currentPassword":"currentpass123","newPassword":"newstrongpass1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	ctx.AuthChangePassword(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when rate limited, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthChangePassword_VerifyError500(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := newAuthCtxAt(t, dir)
	_ = ctx.AuthManager.SetPassword("currentpass123")
	// Corrupt auth.json so ChangePassword's inner VerifyPassword errors.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatalf("corrupt auth.json: %v", err)
	}
	rec := postJSON(t, ctx.AuthChangePassword, "/api/auth/change-password", map[string]interface{}{
		"currentPassword": "currentpass123",
		"newPassword":     "newstrongpass1",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ChangePassword errors, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- UpdateTemplate / OfficeAgentAskAnswer / OfficeAgentPostMemorySection bad JSON ---

func TestUpdateTemplate_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := &Context{TemplateStore: nil}
	req := withURLParam(httptest.NewRequest("PUT", "/api/templates/t1", strings.NewReader("{bad")), "id", "t1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestOfficeAgentAskAnswer_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/ask/answer", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentAskAnswer(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestOfficeAgentPostMemorySection_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/memory/section", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentPostMemorySection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

// --- OfficeAgentTaskSelfAdd edge arms ---

func TestOfficeAgentTaskSelfAdd_UnknownAgentNoOp(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": "ghost-agent", "title": "task",
	})
	// Unknown agent -> ok(nil) (200, no task created).
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (no-op) for unknown agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentTaskSelfAdd_InvalidAcceptanceCriterion400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, _ := registerTwoAgents(ctx)
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId":           a,
		"title":               "task with bad criteria",
		"acceptanceCriteria":  []string{"  "}, // empty/whitespace -> ValidateAcceptanceCriterion error
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty acceptance criterion, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- OfficeAgentGetMemoryQuery nil-hits normalization ---

func TestOfficeAgentGetMemoryQuery_NilHitsNormalized(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, _ := registerTwoAgents(ctx)
	// A fresh agent with no memory -> MemoryQuery returns nil -> normalized to [].
	req := httptest.NewRequest("GET", "/api/office/agent/memory/query?sessionId="+a+"&q=anything", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemoryQuery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"hits":[]`) {
		t.Errorf("expected an empty hits array, got %s", rec.Body.String())
	}
}

// --- OfficeGenerateReport IO failure ---

func TestOfficeGenerateReport_GenerateError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	ctx := newTimelineContext(t) // isolates HOME
	home, _ := os.UserHomeDir()
	// Plant a FILE where the reports dir should be a directory so MkdirAll inside
	// GenerateWrapUpReport fails.
	officeMem := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(officeMem, 0o755); err != nil {
		t.Fatalf("mkdir office-memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(officeMem, "reports"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("plant reports file: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/office/reports/generate", nil)
	ctx.OfficeGenerateReport(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when report generation fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- PostKnowledgeRevoke bad JSON + MarkRevoked error ---

func TestPostKnowledgeRevoke_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/knowledge/revoke", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.PostKnowledgeRevoke(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestPostKnowledgeRevoke_MarkRevokedError500(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	libDir := t.TempDir()
	restore := library.SetDirForTest(libDir)
	t.Cleanup(restore)
	// Corrupt the ledger so MarkRevoked's load() fails.
	if err := os.WriteFile(filepath.Join(libDir, ".promotion-ledger.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatalf("corrupt ledger: %v", err)
	}
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.PostKnowledgeRevoke, "/api/knowledge/revoke", map[string]interface{}{
		"path": "workflows/auth.md", "reason": "stale",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when MarkRevoked fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- GetKnowledgeLibrary read-error + truncation ---

func TestGetKnowledgeLibrary_ReadDirTarget500(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	libDir := t.TempDir()
	restore := library.SetDirForTest(libDir)
	t.Cleanup(restore)
	// The "file" we request is actually a directory -> os.ReadFile -> EISDIR
	// (not IsNotExist) -> 500.
	if err := os.MkdirAll(filepath.Join(libDir, "adir"), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	ctx := newTimelineContext(t)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, httptest.NewRequest("GET", "/api/knowledge/library?path=adir", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory as a file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetKnowledgeLibrary_TruncatesLargeFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	libDir := t.TempDir()
	restore := library.SetDirForTest(libDir)
	t.Cleanup(restore)
	// A file larger than knowledgeMaxFileBytes (1 MB) triggers truncation.
	big := make([]byte, knowledgeMaxFileBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(libDir, "big.md"), big, 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	ctx := newTimelineContext(t)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeLibrary(rec, httptest.NewRequest("GET", "/api/knowledge/library?path=big.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["truncated"] != true {
		t.Errorf("expected truncated=true for an oversize library file, got %v", resp["truncated"])
	}
	if size, _ := resp["size"].(float64); int(size) != knowledgeMaxFileBytes {
		t.Errorf("expected size capped at %d, got %v", knowledgeMaxFileBytes, resp["size"])
	}
}

// --- PeersList store error ---

func TestPeersList_UnreadCountsError500(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relayRoot := filepath.Join(dir, "relay")
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		t.Fatalf("mkdir relay root: %v", err)
	}
	// inbox planted as a file -> UnreadInboundByPeer's ensureLoaded ReadDir fails.
	if err := os.WriteFile(filepath.Join(relayRoot, "inbox"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("plant inbox file: %v", err)
	}
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(filepath.Join(dir, "identity.json"))
	svc := &peers.Service{
		Identity: identity, Registry: reg,
		Store: peers.NewStore(relayRoot), Client: peers.NewClient(),
	}
	p := &Peers{Identity: identity, Registry: reg, Relay: svc}

	rec := httptest.NewRecorder()
	p.PeersList(rec, httptest.NewRequest("GET", "/api/peers", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when unread-count read fails, got %d: %s", rec.Code, rec.Body.String())
	}
}
