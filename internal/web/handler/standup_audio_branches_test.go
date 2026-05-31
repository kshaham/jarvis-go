package handler

// Branch fills for OfficePostStandupAudio's error mapping: the newSynth
// factory returning tts.ErrNoAPIKey -> 400, a generic synth error -> 500, and
// an invalid standup id -> the wrapped "invalid standup id" error surfaced as
// 500 (it isn't one of the sentinel errors).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/tts"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func audioCtxWithSynthErr(t *testing.T, synthErr error) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	broker := sse.NewBroker(100, 10)
	jobs := NewAudioJobManager(
		nil,
		broker.Broadcast,
		func() (Synthesizer, error) { return nil, synthErr },
		func(_ string, _ int) string { return "alloy" },
	)
	t.Cleanup(jobs.Wait)
	return &Context{SSEBroker: broker, AudioJobs: jobs}
}

func TestOfficePostStandupAudio_NoAPIKey400(t *testing.T) {
	ctx := audioCtxWithSynthErr(t, tts.ErrNoAPIKey)
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, standupAudioReq("POST", "sx"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no API key, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostStandupAudio_GenericSynthError500(t *testing.T) {
	ctx := audioCtxWithSynthErr(t, errors.New("tts backend exploded"))
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, standupAudioReq("POST", "sx"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on generic synth error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficePostStandupAudio_InvalidStandupID500(t *testing.T) {
	// An unsafe id fails safeName inside Start -> a plain error (not a sentinel)
	// -> the generic 500 branch.
	ctx := audioCtxWithSynthErr(t, nil)
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, standupAudioReq("POST", "../evil"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for invalid standup id, got %d: %s", rec.Code, rec.Body.String())
	}
}
