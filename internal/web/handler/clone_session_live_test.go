package handler

// Happy-path coverage for CloneSession: clone a live shell session into
// multiple parallel workers and assert the clones are spawned + registered.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloneSession_HappyMultiple(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	srcID, _ := liveSession(t, ctx)

	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+srcID+"/clone?count=2", nil), "id", srcID)
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clone: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	clones, _ := resp["sessions"].([]interface{})
	if len(clones) != 2 {
		t.Fatalf("expected 2 clones, got %d", len(clones))
	}
	// Stop each clone so its read-loop goroutine doesn't leak.
	for _, c := range clones {
		cm, _ := c.(map[string]interface{})
		id, _ := cm["id"].(string)
		if id == "" {
			t.Error("clone missing id")
			continue
		}
		// Clone name should derive from the source.
		if name, _ := cm["name"].(string); name == "" {
			t.Error("clone missing name")
		}
		cloneID := id
		t.Cleanup(func() {
			_ = ctx.SessionManager.StopSession(context.Background(), cloneID)
			ctx.cleanupSessionResources(cloneID)
		})
	}
}

func TestCloneSession_DefaultCountOne(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	srcID, _ := liveSession(t, ctx)

	rec := httptest.NewRecorder()
	// No count param -> defaults to 1. Also exercises an out-of-range count
	// being ignored in favor of the default.
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+srcID+"/clone?count=999", nil), "id", srcID)
	ctx.CloneSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clone: expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	clones, _ := resp["sessions"].([]interface{})
	if len(clones) != 1 {
		t.Fatalf("out-of-range count should fall back to 1 clone, got %d", len(clones))
	}
	cm, _ := clones[0].(map[string]interface{})
	id, _ := cm["id"].(string)
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		ctx.cleanupSessionResources(id)
	})
}
