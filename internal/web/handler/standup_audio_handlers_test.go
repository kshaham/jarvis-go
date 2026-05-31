package handler

// Coverage for the OfficePostStandupAudio / OfficeGetStandupAudio HTTP
// handlers. The AudioJobManager is built with the existing fakeSynth so a
// real standup generates an MP3 without hitting OpenAI; the error branches
// (no standup, invalid ID, audio-not-generated) are also exercised.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newAudioCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	broker := sse.NewBroker(100, 10)
	jobs := NewAudioJobManager(
		nil,
		broker.Broadcast,
		func() (Synthesizer, error) { return fakeSynth{}, nil },
		func(_ string, _ int) string { return "alloy" },
	)
	t.Cleanup(jobs.Wait)
	return &Context{SSEBroker: broker, AudioJobs: jobs}
}

func standupAudioReq(method, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("standupId", id)
	req := httptest.NewRequest(method, "/api/office/standups/"+id+"/audio", nil)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestOfficePostStandupAudio_LatestNoStandup404(t *testing.T) {
	ctx := newAudioCtx(t)
	// "latest" is resolved synchronously; with no completed standup on disk
	// the resolver returns os.ErrNotExist -> 404.
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, standupAudioReq("POST", "latest"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for latest with no standup, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostStandupAudio_HappyAccepted(t *testing.T) {
	ctx := newAudioCtx(t)

	// Persist a completed standup so Start can find it (write the JSON file
	// directly into the standup dir, the same shape the manager produces).
	ps := office.PersistedStandup{
		ID:           "sx",
		Participants: []string{"a"},
		Transcript: []office.StandupMessage{
			{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: "Shipped it."},
		},
		CompletedAt: 1000,
	}
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}
	data, _ := json.Marshal(ps)
	if err := os.WriteFile(filepath.Join(dir, "sx.json"), data, 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, standupAudioReq("POST", "sx"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	// Drain the job before the test's TempDir cleanup.
	ctx.AudioJobs.Wait()
}

func TestOfficeGetStandupAudio_InvalidID400(t *testing.T) {
	ctx := newAudioCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupAudio(rec, standupAudioReq("GET", "../etc"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", rec.Code)
	}
}

func TestOfficeGetStandupAudio_NotGenerated404(t *testing.T) {
	ctx := newAudioCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupAudio(rec, standupAudioReq("GET", "neverdone"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when audio not generated, got %d", rec.Code)
	}
}

func TestOfficeGetStandupAudio_LatestNoneFound404(t *testing.T) {
	ctx := newAudioCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupAudio(rec, standupAudioReq("GET", "latest"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for latest with no completed standup, got %d", rec.Code)
	}
}
