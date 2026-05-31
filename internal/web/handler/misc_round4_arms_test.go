package handler

// Round-4 fill: UpdateScheduledTask's bad-JSON 400 arm (misc.go:711-714).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateScheduledTask_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := newScheduledCtx()
	req := withURLParam(httptest.NewRequest("PUT", "/api/scheduled/some-id", strings.NewReader("{not json")), "id", "some-id")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateScheduledTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}
