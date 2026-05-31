package handler

// Round-2 branch fills for AudioJobManager.run -- the synthesis pipeline that
// runs in a background goroutine. Each test drives ONE error/edge arm to a
// terminal SSE event (Error/Ready) and asserts the broadcast message, so the
// branches are exercised HONESTLY through real synth-failure injection and real
// filesystem faults (read-only standup dir, oversize output).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/tts"
)

// configurableSynth lets each test shape the CondenseScript result + error and
// the CreateSpeech bytes + error independently.
type configurableSynth struct {
	script     tts.Script
	condErr    error
	speech     []byte
	speechErr  error
	speechFunc func(req tts.SpeechRequest) ([]byte, error)
}

func (s configurableSynth) CondenseScript(_ context.Context, _ string, _ int) (tts.Script, error) {
	return s.script, s.condErr
}

func (s configurableSynth) CreateSpeech(_ context.Context, req tts.SpeechRequest) ([]byte, error) {
	if s.speechFunc != nil {
		return s.speechFunc(req)
	}
	return s.speech, s.speechErr
}

// audioEventRec captures broadcast events + the last message per event name.
type audioEventRec struct {
	mu     sync.Mutex
	counts map[string]int
	last   map[string]map[string]interface{}
}

func newAudioEventRec() *audioEventRec {
	return &audioEventRec{counts: map[string]int{}, last: map[string]map[string]interface{}{}}
}

func (e *audioEventRec) broadcast(name string, data interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counts[name]++
	if m, ok := data.(map[string]interface{}); ok {
		e.last[name] = m
	}
}

func (e *audioEventRec) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counts[name]
}

func (e *audioEventRec) message(name string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if m, ok := e.last[name]; ok {
		if s, ok := m["message"].(string); ok {
			return s
		}
	}
	return ""
}

// seedPersistedStandup writes a persisted standup with the given transcript to
// the (HOME-isolated) standup dir.
func seedPersistedStandup(t *testing.T, id string, transcript []office.StandupMessage) {
	t.Helper()
	ps := office.PersistedStandup{ID: id, Participants: []string{"a"}, Transcript: transcript, CompletedAt: 1000}
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal standup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}
}

// runAudioJobAndWait starts a job and blocks until a terminal event
// (Ready or Error) is observed, or the deadline elapses.
func runAudioJobAndWait(t *testing.T, rec *audioEventRec, synth Synthesizer, id string) {
	t.Helper()
	mgr := NewAudioJobManager(
		context.Background(),
		rec.broadcast,
		func() (Synthesizer, error) { return synth, nil },
		func(_ string, _ int) string { return "marin" },
	)
	if _, err := mgr.Start(id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count("office:standupAudioReady") > 0 || rec.count("office:standupAudioError") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mgr.Wait()
}

func standardTranscript() []office.StandupMessage {
	return []office.StandupMessage{
		{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: "Shipped the parser."},
	}
}

// L178: LoadPersistedStandup error -> "failed to load standup".
func TestRun_LoadStandupError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Seed a standup so Start's safeName passes, then corrupt the JSON so
	// LoadPersistedStandup fails inside run.
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newAudioEventRec()
	runAudioJobAndWait(t, rec, configurableSynth{}, "corrupt")
	if rec.count("office:standupAudioError") == 0 {
		t.Fatalf("expected an error event for corrupt standup; events=%v", rec.counts)
	}
	if !strings.Contains(rec.message("office:standupAudioError"), "failed to load standup") {
		t.Errorf("error message=%q, want 'failed to load standup'", rec.message("office:standupAudioError"))
	}
}

// L197: no speakable content -> empty inputs -> "no speakable content".
func TestRun_NoSpeakableContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A transcript whose only message has empty content -> BuildSpeechInputs yields nothing.
	seedPersistedStandup(t, "empty-content", []office.StandupMessage{
		{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: ""},
	})
	rec := newAudioEventRec()
	runAudioJobAndWait(t, rec, configurableSynth{}, "empty-content")
	if !strings.Contains(rec.message("office:standupAudioError"), "no speakable content") {
		t.Errorf("expected 'no speakable content' error, got %q (events=%v)", rec.message("office:standupAudioError"), rec.counts)
	}
}

// L214: CondenseScript error -> "script condensation failed".
func TestRun_CondenseError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "cond-err", standardTranscript())
	rec := newAudioEventRec()
	runAudioJobAndWait(t, rec, configurableSynth{condErr: errors.New("LLM down")}, "cond-err")
	if !strings.Contains(rec.message("office:standupAudioError"), "script condensation failed") {
		t.Errorf("expected 'script condensation failed', got %q", rec.message("office:standupAudioError"))
	}
}

// L271: condense returns empty intro + all-empty segments -> "no speakable
// content after condensing". Also exercises the seg.Text=="" continue (L249).
func TestRun_NoSpeakableAfterCondensing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "empty-script", standardTranscript())
	rec := newAudioEventRec()
	synth := configurableSynth{script: tts.Script{
		Intro:    "",
		Segments: []tts.ScriptSegment{{SessionID: "a", Text: ""}}, // empty -> skipped
	}}
	runAudioJobAndWait(t, rec, synth, "empty-script")
	if !strings.Contains(rec.message("office:standupAudioError"), "no speakable content after condensing") {
		t.Errorf("expected 'no speakable content after condensing', got %q", rec.message("office:standupAudioError"))
	}
}

// L253: a segment referencing a session NOT in the original inputs -> the
// !known arm assigns a tail index. Drives the unknown-session voice path while
// still reaching Ready.
func TestRun_UnknownSessionSegmentGetsVoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "unknown-seg", standardTranscript())
	rec := newAudioEventRec()
	synth := configurableSynth{
		script: tts.Script{
			Intro: "Recap.",
			Segments: []tts.ScriptSegment{
				{SessionID: "a", Text: "Alice did work."},
				{SessionID: "ghost-session", Text: "Mystery speaker."}, // not in inputs -> !known
			},
		},
		speech: []byte{0xFF, 0xFB, 0x00, 0x01},
	}
	runAudioJobAndWait(t, rec, synth, "unknown-seg")
	if rec.count("office:standupAudioReady") == 0 {
		t.Fatalf("expected ready event; events=%v err=%q", rec.counts, rec.message("office:standupAudioError"))
	}
}

// L290: CreateSpeech error -> "speech synthesis failed".
func TestRun_SpeechError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "speech-err", standardTranscript())
	rec := newAudioEventRec()
	synth := configurableSynth{
		script:    tts.Script{Intro: "Recap.", Segments: []tts.ScriptSegment{{SessionID: "a", Text: "work"}}},
		speechErr: errors.New("tts 500"),
	}
	runAudioJobAndWait(t, rec, synth, "speech-err")
	if !strings.Contains(rec.message("office:standupAudioError"), "speech synthesis failed") {
		t.Errorf("expected 'speech synthesis failed', got %q", rec.message("office:standupAudioError"))
	}
}

// L303: oversize concatenated audio -> "audio output too large". Each speech
// segment returns a chunk just over half the cap so two segments exceed it.
func TestRun_OversizeAudio(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "too-big", standardTranscript())
	rec := newAudioEventRec()
	big := make([]byte, maxAudioBytes+1) // single segment already exceeds the cap
	synth := configurableSynth{
		script: tts.Script{Intro: "Recap.", Segments: []tts.ScriptSegment{{SessionID: "a", Text: "work"}}},
		speech: big,
	}
	runAudioJobAndWait(t, rec, synth, "too-big")
	if !strings.Contains(rec.message("office:standupAudioError"), "too large") {
		t.Errorf("expected 'too large' error, got %q", rec.message("office:standupAudioError"))
	}
}

// L313: os.WriteFile failure -> "failed to write audio file". Force it by
// making the standup dir read-only so the *.mp3.tmp write fails with EACCES.
func TestRun_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: read-only dir does not block writes")
	}
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "write-fail", standardTranscript())
	dir := office.StandupDir()
	// Make the standup dir read-only so the tmp write fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	rec := newAudioEventRec()
	synth := configurableSynth{
		script: tts.Script{Intro: "Recap.", Segments: []tts.ScriptSegment{{SessionID: "a", Text: "work"}}},
		speech: []byte{0xFF, 0xFB, 0x00, 0x01},
	}
	runAudioJobAndWait(t, rec, synth, "write-fail")
	if !strings.Contains(rec.message("office:standupAudioError"), "failed to write audio file") {
		t.Errorf("expected 'failed to write audio file', got %q (events=%v)", rec.message("office:standupAudioError"), rec.counts)
	}
}

// L320: os.Rename failure -> "failed to finalize audio file". The tmp write
// succeeds, but renaming onto a path that is a non-empty DIRECTORY fails.
func TestRun_RenameError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "rename-fail", standardTranscript())
	// Plant a non-empty directory exactly where the final .mp3 should land, so
	// os.Rename(tmp, audioPath) fails (cannot rename a file over a dir).
	audioPath := office.StandupAudioPath("rename-fail")
	if err := os.MkdirAll(filepath.Join(audioPath, "blocker"), 0o755); err != nil {
		t.Fatalf("plant blocker dir: %v", err)
	}

	rec := newAudioEventRec()
	synth := configurableSynth{
		script: tts.Script{Intro: "Recap.", Segments: []tts.ScriptSegment{{SessionID: "a", Text: "work"}}},
		speech: []byte{0xFF, 0xFB, 0x00, 0x01},
	}
	runAudioJobAndWait(t, rec, synth, "rename-fail")
	if !strings.Contains(rec.message("office:standupAudioError"), "failed to finalize audio file") {
		t.Errorf("expected 'failed to finalize audio file', got %q (events=%v)", rec.message("office:standupAudioError"), rec.counts)
	}
}

// L157: a panic mid-pipeline is recovered in the deferred cleanup, which
// removes the tmp file and broadcasts a "panicked" error. We force a panic from
// inside CreateSpeech (called from run's voicing loop).
func TestRun_PanicRecovered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedPersistedStandup(t, "panic-job", standardTranscript())
	rec := newAudioEventRec()
	synth := configurableSynth{
		script:     tts.Script{Intro: "Recap.", Segments: []tts.ScriptSegment{{SessionID: "a", Text: "work"}}},
		speechFunc: func(tts.SpeechRequest) ([]byte, error) { panic("synthetic mid-pipeline boom") },
	}
	runAudioJobAndWait(t, rec, synth, "panic-job")
	if !strings.Contains(rec.message("office:standupAudioError"), "panicked") {
		t.Errorf("expected a 'panicked' error event, got %q (events=%v)", rec.message("office:standupAudioError"), rec.counts)
	}
	// The deferred cleanup must have removed any orphaned .mp3.tmp.
	if _, err := os.Stat(office.StandupAudioPath("panic-job") + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected tmp file cleaned up after panic, stat err=%v", err)
	}
}
