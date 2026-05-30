package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/office"
)

// ---------------------------------------------------------------------------
// currentSpeakerID unit tests (no Context needed)
// ---------------------------------------------------------------------------

func TestCurrentSpeakerID_NilState(t *testing.T) {
	if got := currentSpeakerID(nil); got != "" {
		t.Errorf("currentSpeakerID(nil) = %q, want empty", got)
	}
}

func TestCurrentSpeakerID_InactiveStandup(t *testing.T) {
	su := &office.StandupState{
		Active:       false,
		Participants: []string{"alice", "bob"},
		CurrentIndex: 0,
	}
	if got := currentSpeakerID(su); got != "" {
		t.Errorf("currentSpeakerID(inactive) = %q, want empty", got)
	}
}

func TestCurrentSpeakerID_InRange(t *testing.T) {
	su := &office.StandupState{
		Active:       true,
		Participants: []string{"alice", "bob"},
		CurrentIndex: 1,
	}
	if got := currentSpeakerID(su); got != "bob" {
		t.Errorf("currentSpeakerID(index=1) = %q, want bob", got)
	}
}

func TestCurrentSpeakerID_OutOfRange(t *testing.T) {
	su := &office.StandupState{
		Active:       true,
		Participants: []string{"alice", "bob"},
		CurrentIndex: 5,
	}
	if got := currentSpeakerID(su); got != "" {
		t.Errorf("currentSpeakerID(out-of-range) = %q, want empty", got)
	}
}

func TestCurrentSpeakerID_NegativeIndex(t *testing.T) {
	su := &office.StandupState{
		Active:       true,
		Participants: []string{"alice"},
		CurrentIndex: -1,
	}
	if got := currentSpeakerID(su); got != "" {
		t.Errorf("currentSpeakerID(negative) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// TestOfficeGetStandupIncludesIDAndTranscript
// Uses SetStandup to inject an active standup, then calls the handler.
// ---------------------------------------------------------------------------

func TestOfficeGetStandupIncludesIDAndTranscript(t *testing.T) {
	ctx := newSnapshotCtx(t)

	// Inject an active standup with ID and a transcript entry.
	ctx.OfficeManager.SetStandup(&office.StandupState{
		ID:           "standup-test-123",
		Active:       true,
		Participants: []string{"alice", "bob"},
		CurrentIndex: 0,
		Transcript: []office.StandupMessage{
			{
				SessionID:   "alice",
				SpeakerName: "Alice",
				Content:     "working on task X",
				Type:        "status_report",
				Timestamp:   time.Now().UnixMilli(),
			},
		},
		StartedAt: time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/office/standup", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Success bool `json:"success"`
		Standup struct {
			ID               string                  `json:"id"`
			Active           bool                    `json:"active"`
			Transcript       []office.StandupMessage `json:"transcript"`
			CurrentSpeakerID string                  `json:"currentSpeakerId"`
		} `json:"standup"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.Success {
		t.Errorf("response success=false")
	}
	if envelope.Standup.ID == "" {
		t.Errorf("standup.id is empty, want standup-test-123")
	}
	if envelope.Standup.ID != "standup-test-123" {
		t.Errorf("standup.id = %q, want standup-test-123", envelope.Standup.ID)
	}
	if len(envelope.Standup.Transcript) < 1 {
		t.Errorf("standup.transcript len=%d, want >=1", len(envelope.Standup.Transcript))
	}
	if !envelope.Standup.Active {
		t.Errorf("standup.active should be true")
	}
	// currentSpeakerId should be "alice" (index 0 in ["alice","bob"])
	if envelope.Standup.CurrentSpeakerID != "alice" {
		t.Errorf("standup.currentSpeakerId = %q, want alice", envelope.Standup.CurrentSpeakerID)
	}
}

// ---------------------------------------------------------------------------
// TestOfficeGetStandupHistoryShape
// Writes two PersistedStandup JSON files, calls the handler, and
// verifies the response shape and sort order.
// ---------------------------------------------------------------------------

func TestOfficeGetStandupHistoryShape(t *testing.T) {
	// newSnapshotCtx sets HOME to a temp dir via t.Setenv, so build the
	// context first and then derive the standups directory from HOME.
	ctx := newSnapshotCtx(t)

	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}

	// Older standup — completed first.
	older := office.PersistedStandup{
		ID:           "standup-older",
		Participants: []string{"alice"},
		StartedAt:    time.Now().Add(-2 * time.Hour).UnixMilli(),
		CompletedAt:  time.Now().Add(-1 * time.Hour).UnixMilli(),
		Transcript: []office.StandupMessage{
			{SessionID: "alice", Content: "old update", Type: "status_report"},
		},
	}
	// Newer standup — completed second.
	newer := office.PersistedStandup{
		ID:           "standup-newer",
		Participants: []string{"alice", "bob"},
		StartedAt:    time.Now().Add(-30 * time.Minute).UnixMilli(),
		CompletedAt:  time.Now().UnixMilli(),
		Transcript: []office.StandupMessage{
			{SessionID: "alice", Content: "new update", Type: "status_report"},
			{SessionID: "bob", Content: "also new", Type: "status_report"},
		},
	}

	for _, ps := range []office.PersistedStandup{older, newer} {
		data, err := json.Marshal(ps)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, ps.ID+".json"), data, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/office/standup/history", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupHistory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify key is "history" not "standups".
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if _, hasHistory := raw["history"]; !hasHistory {
		t.Errorf("response missing top-level 'history' key; got keys: %v", keysOf(raw))
	}
	if _, hasStandups := raw["standups"]; hasStandups {
		t.Errorf("response contains old 'standups' key; should use 'history'")
	}

	var envelope struct {
		History []struct {
			ID           string   `json:"id"`
			StartedAt    string   `json:"startedAt"`
			EndedAt      string   `json:"endedAt"`
			Participants []string `json:"participants"`
			MessageCount int      `json:"messageCount"`
		} `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode history: %v", err)
	}

	if len(envelope.History) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(envelope.History))
	}

	// Sorted newest-first: first entry should be "standup-newer".
	first := envelope.History[0]
	if first.ID != "standup-newer" {
		t.Errorf("first entry id=%q, want standup-newer (newest-first sort)", first.ID)
	}
	if first.StartedAt == "" {
		t.Errorf("first entry startedAt is empty")
	}
	if first.MessageCount != 2 {
		t.Errorf("first entry messageCount=%d, want 2", first.MessageCount)
	}

	// Second entry is "standup-older".
	second := envelope.History[1]
	if second.ID != "standup-older" {
		t.Errorf("second entry id=%q, want standup-older", second.ID)
	}
	if second.MessageCount != 1 {
		t.Errorf("second entry messageCount=%d, want 1", second.MessageCount)
	}
}

// keysOf returns the string keys of a map for error messages.
func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---------------------------------------------------------------------------
// TestPutMemberVoiceRejectsInvalid — bogus voice name → 400
// ---------------------------------------------------------------------------

func TestPutMemberVoiceRejectsInvalid(t *testing.T) {
	ctx := newSnapshotCtx(t)

	// Register an agent so the handler can find it.
	ctx.OfficeManager.RegisterAgent("s1", "cfg1", &office.TeamMemberConfig{ID: "cfg1", Name: "Alice"})

	body := bytes.NewBufferString(`{"voice":"bogus"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/office/member/s1/voice", body)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "s1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	ctx.OfficePutMemberVoice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid voice") {
		t.Errorf("expected 'invalid voice' in body, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// TestPutMemberVoiceSetsConfig — valid voice → 200 and Config.Voice updated
// ---------------------------------------------------------------------------

func TestPutMemberVoiceSetsConfig(t *testing.T) {
	ctx := newSnapshotCtx(t)

	ctx.OfficeManager.RegisterAgent("s1", "cfg1", &office.TeamMemberConfig{ID: "cfg1", Name: "Alice"})

	body := bytes.NewBufferString(`{"voice":"marin"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/office/member/s1/voice", body)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", "s1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	ctx.OfficePutMemberVoice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	agent := ctx.OfficeManager.GetAgent("s1")
	if agent == nil {
		t.Fatal("agent s1 not found after PUT")
	}
	if agent.Config.Voice != "marin" {
		t.Errorf("Config.Voice = %q, want marin", agent.Config.Voice)
	}
}
