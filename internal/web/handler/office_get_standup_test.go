package handler

// Branch fill for OfficeGetStandup's active-standup arm: register an agent,
// start a standup so GetStandup() returns non-nil, then assert the handler
// serializes the standup payload (id, participants, currentSpeakerId).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeGetStandup_ActiveStandup(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	su := ctx.OfficeManager.StartStandup([]string{"s-vex"})
	if su == nil {
		t.Fatal("StartStandup returned nil with a registered participant")
	}
	t.Cleanup(ctx.OfficeManager.StopStandup)

	req := httptest.NewRequest("GET", "/api/office/standup", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Standup map[string]interface{} `json:"standup"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Standup == nil {
		t.Fatal("expected a non-nil standup payload")
	}
	if resp.Standup["id"] != su.ID {
		t.Errorf("standup id=%v, want %s", resp.Standup["id"], su.ID)
	}
	if resp.Standup["active"] != true {
		t.Errorf("standup active=%v, want true", resp.Standup["active"])
	}
}
