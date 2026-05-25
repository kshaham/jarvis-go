package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claudeman/jarvis-go/internal/office"
)

func statuslineGet(t *testing.T, ctx *Context, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/office/agent/statusline?"+query, nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentStatusLine(rec, req)
	return rec
}

func TestOfficeAgentStatusLine_NoSessionIDReturnsPlaceholder(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := statuslineGet(t, ctx, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "(no session)") {
		t.Errorf("missing placeholder: %q", got)
	}
}

func TestOfficeAgentStatusLine_NoManagerReturnsPlaceholder(t *testing.T) {
	ctx := &Context{}
	rec := statuslineGet(t, ctx, "sessionId=sess-0001")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "manager offline") {
		t.Errorf("missing placeholder: %q", got)
	}
}

func TestOfficeAgentStatusLine_UnknownAgentReturnsPlaceholder(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := statuslineGet(t, ctx, "sessionId=ghost-0001")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "not registered") {
		t.Errorf("missing placeholder: %q", got)
	}
}

func TestOfficeAgentStatusLine_RegisteredAgentRendersName(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-vexA001", "vex", &office.TeamMemberConfig{
		ID: "vex", Name: "Vex", Role: "scout", Tier: "strategic",
	})
	rec := statuslineGet(t, ctx, "sessionId=sess-vexA001")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Vex") {
		t.Errorf("name missing from statusline: %q", body)
	}
	if !strings.Contains(body, "[office]") {
		t.Errorf("missing [office] prefix: %q", body)
	}
	if !strings.Contains(body, "scout") {
		t.Errorf("role missing: %q", body)
	}
	if !strings.Contains(body, "lead") {
		// Tier 'strategic' should add the "lead" suffix.
		t.Errorf("strategic-tier should mark agent as lead: %q", body)
	}
}

func TestOfficeAgentStatusLine_ContentTypePlainText(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := statuslineGet(t, ctx, "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type=%q want text/plain", ct)
	}
}

func TestTruncateRune(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdefgh", 3, "abc…"},
		{"日本語テスト", 2, "日本…"},
		{"", 5, ""},
		{"any", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := truncateRune(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("truncateRune(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}
