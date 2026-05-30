package handler

import (
	"context"
	"encoding/json"
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
