package handler

// Round-4 fills for a cluster of small single-arm gaps:
//   - OfficePostStandupAudio: the ErrTooManyAudioJobs -> 429 arm (348-351),
//     driven by saturating the audio-job semaphore with blocking fake synth jobs.
//   - GetSessionSubagents: the SubagentWatcher == nil -> empty-200 arm (475-481).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOfficePostStandupAudio_TooManyJobs429 saturates the audio-job semaphore
// (cap = maxConcurrentAudioJobs) with blocking jobs, then posts one more so the
// handler maps ErrTooManyAudioJobs to 429.
func TestOfficePostStandupAudio_TooManyJobs429(t *testing.T) {
	synth := newBlockingSynth(maxConcurrentAudioJobs + 2)
	mgr := newTestManager(t, synth)

	// Saturate every slot with a distinct in-flight standup. Each blocks inside
	// CondenseScript holding its semaphore slot until the test ends.
	for i := 0; i < maxConcurrentAudioJobs; i++ {
		id := "saturate-" + string(rune('a'+i))
		writeStandupFixture(t, id)
		if _, err := mgr.Start(id); err != nil {
			t.Fatalf("priming Start(%s): %v", id, err)
		}
	}
	// Wait until all primer jobs are genuinely in flight (holding their slots).
	waitForStarted(t, synth, maxConcurrentAudioJobs)
	t.Cleanup(func() { close(synth.release) })

	ctx := &Context{AudioJobs: mgr}

	// One more distinct standup -> no slot -> ErrTooManyAudioJobs -> 429.
	writeStandupFixture(t, "overflow")
	req := withURLParam(httptest.NewRequest(http.MethodPost, "/api/office/standups/overflow/audio", nil), "standupId", "overflow")
	rec := httptest.NewRecorder()
	ctx.OfficePostStandupAudio(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when the audio-job cap is reached, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetSessionSubagents_WatcherNil drives the SubagentWatcher == nil arm: a
// found session but no watcher returns an empty subagents list with 200.
func TestGetSessionSubagents_WatcherNil(t *testing.T) {
	ctx := newSessionExtCtx(t)
	ctx.SubagentWatcher = nil // force the nil-watcher early return
	id, _ := liveSession(t, ctx)

	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/subagents", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionSubagents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	subs, _ := resp["subagents"].([]interface{})
	if len(subs) != 0 {
		t.Errorf("expected empty subagents with a nil watcher, got %d", len(subs))
	}
}
