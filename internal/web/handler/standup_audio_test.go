package handler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/tts"
)

type fakeSynth struct{}

func (fakeSynth) CondenseScript(_ context.Context, _ string, _ int) (tts.Script, error) {
	return tts.Script{
		Intro: "AI-generated recap.",
		Segments: []tts.ScriptSegment{
			{SessionID: "a", Text: "Alice did work."},
		},
	}, nil
}

func (fakeSynth) CreateSpeech(_ context.Context, _ tts.SpeechRequest) ([]byte, error) {
	return []byte{0xFF, 0xFB, 0x00, 0x01}, nil
}

func TestAudioJobEmitsProgressAndWritesFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Persist a standup "s1" with one Alice status_report.
	ps := office.PersistedStandup{
		ID:           "s1",
		Participants: []string{"a"},
		Transcript: []office.StandupMessage{
			{
				SessionID:   "a",
				SpeakerName: "Alice",
				Type:        "status_report",
				Content:     "Shipped the parser.",
			},
		},
		CompletedAt: 1000,
	}

	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	events := map[string]int{}
	broadcast := func(name string, _ interface{}) {
		mu.Lock()
		events[name]++
		mu.Unlock()
	}

	mgr := NewAudioJobManager(
		context.Background(),
		broadcast,
		func() (Synthesizer, error) { return fakeSynth{}, nil },
		func(_ string, _ int) string { return "marin" },
	)
	if _, err := mgr.Start("s1"); err != nil {
		t.Fatal(err)
	}

	// Poll up to 2s for the ready event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ready := events["office:standupAudioReady"]
		mu.Unlock()
		if ready > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if events["office:standupAudioError"] != 0 {
		t.Fatalf("unexpected error events: %v", events)
	}
	if events["office:standupAudioProgress"] == 0 {
		t.Errorf("expected progress events; got events=%v", events)
	}
	if events["office:standupAudioReady"] == 0 {
		t.Fatalf("timed out waiting for ready event; events=%v", events)
	}
	fi, statErr := os.Stat(office.StandupAudioPath("s1"))
	if statErr != nil || fi.Size() == 0 {
		t.Fatalf("audio file missing or empty: %v", statErr)
	}
}

// TestStartReturnsErrNoAPIKey asserts that when newSynth returns tts.ErrNoAPIKey,
// Start() propagates the error synchronously without consuming a semaphore slot,
// marking a job running, or broadcasting any events (Fix 4).
func TestStartReturnsErrNoAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Seed a persisted standup so the id-resolution path succeeds.
	ps := office.PersistedStandup{
		ID:           "s-nokey",
		Participants: []string{"a"},
		Transcript: []office.StandupMessage{
			{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: "Did work."},
		},
		CompletedAt: 1000,
	}
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s-nokey.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	events := map[string]int{}
	broadcast := func(name string, _ interface{}) {
		mu.Lock()
		events[name]++
		mu.Unlock()
	}

	mgr := NewAudioJobManager(
		context.Background(),
		broadcast,
		func() (Synthesizer, error) { return nil, tts.ErrNoAPIKey },
		func(_ string, _ int) string { return "marin" },
	)

	_, startErr := mgr.Start("s-nokey")
	if startErr == nil {
		t.Fatal("expected an error from Start, got nil")
	}
	if !errors.Is(startErr, tts.ErrNoAPIKey) {
		t.Fatalf("expected tts.ErrNoAPIKey, got %v", startErr)
	}

	// Give any goroutine (there should be none) a moment to broadcast events.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 {
		t.Fatalf("Start() must not broadcast any events when key is missing; got %v", events)
	}

	// Semaphore must not have been consumed: we should be able to acquire all
	// maxConcurrentAudioJobs slots without blocking.
	for i := 0; i < maxConcurrentAudioJobs; i++ {
		select {
		case mgr.sem <- struct{}{}:
		default:
			t.Fatalf("sem slot %d was already consumed; Start() leaked a slot", i)
		}
	}
	// Drain what we just pushed so the test is clean.
	for i := 0; i < maxConcurrentAudioJobs; i++ {
		<-mgr.sem
	}
}
