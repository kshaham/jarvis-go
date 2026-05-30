package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/tts"
	"github.com/kshaham/jarvis/internal/web/request"
)

// maxAudioBytes caps the concatenated MP3 size before writing to disk.
// A 50 MiB standup audio file is already unrealistically large; anything
// bigger is most likely a bug or a malicious payload.
const maxAudioBytes = 50 << 20

// maxConcurrentAudioJobs bounds how many synthesis jobs run at once. Each job
// fans out paid OpenAI chat + speech calls, so without a cap an authenticated
// caller looping over every historical standup could spawn unbounded goroutines
// and billing streams.
const maxConcurrentAudioJobs = 3

// ErrTooManyAudioJobs is returned by Start when the concurrency cap is reached.
var ErrTooManyAudioJobs = errors.New("too many audio jobs in progress")

// Synthesizer is the subset of *tts.Client used by AudioJobManager.
// Exported so server.go (package web) can name it in the newSynth closure.
type Synthesizer interface {
	CondenseScript(ctx context.Context, transcript string, budget int) (tts.Script, error)
	CreateSpeech(ctx context.Context, req tts.SpeechRequest) ([]byte, error)
}

const defaultAudioWordBudget = 280

// AudioJobManager coalesces concurrent requests for the same standup audio
// and runs synthesis in a background goroutine.
type AudioJobManager struct {
	baseCtx    context.Context
	mu         sync.Mutex
	running    map[string]bool
	sem        chan struct{}  // counting semaphore bounding concurrent jobs
	wg         sync.WaitGroup // tracks in-flight run() goroutines for shutdown drain
	broadcast  func(string, interface{})
	newSynth   func() (Synthesizer, error)
	voiceFor   func(sessionID string, index int) string
	wordBudget int
}

// NewAudioJobManager constructs a manager. ctx is the server root context;
// pass nil to default to context.Background(). broadcast must be safe for
// concurrent calls (the SSE broker is).
func NewAudioJobManager(
	ctx context.Context,
	broadcast func(string, interface{}),
	newSynth func() (Synthesizer, error),
	voiceFor func(string, int) string,
) *AudioJobManager {
	if ctx == nil {
		ctx = context.Background()
	}
	sweepStaleAudioTmp() // clear *.mp3.tmp orphaned by a prior hard exit mid-write
	return &AudioJobManager{
		baseCtx:    ctx,
		running:    make(map[string]bool),
		sem:        make(chan struct{}, maxConcurrentAudioJobs),
		broadcast:  broadcast,
		newSynth:   newSynth,
		voiceFor:   voiceFor,
		wordBudget: defaultAudioWordBudget,
	}
}

// Wait blocks until all in-flight audio jobs finish. Called at server shutdown
// (after the listeners drain and the root context is cancelled, so jobs exit
// promptly) to avoid leaking a job goroutine mid-write and orphaning a .tmp.
func (m *AudioJobManager) Wait() {
	m.wg.Wait()
}

// sweepStaleAudioTmp removes any *.mp3.tmp left behind when a prior process was
// killed (e.g. SIGKILL) between WriteFile and Rename. Best-effort; ignores errors.
func sweepStaleAudioTmp() {
	matches, err := filepath.Glob(filepath.Join(office.StandupDir(), "*.mp3.tmp"))
	if err != nil {
		return
	}
	for _, p := range matches {
		os.Remove(p) //nolint:errcheck
	}
}

// Start queues or coalesces an audio generation job for the given standup ID.
// "latest" is resolved to the most-recent completed standup ID. Returns the
// resolved ID on success.
//
// The OpenAI key is validated synchronously before the semaphore is acquired,
// so a missing key returns tts.ErrNoAPIKey immediately without consuming a
// concurrency slot or launching a goroutine.
func (m *AudioJobManager) Start(standupID string) (string, error) {
	id := standupID
	if id == "latest" {
		var err error
		id, err = office.LatestCompletedStandupID()
		if err != nil {
			return "", err
		}
	}
	if !safeName(id) {
		return "", errors.New("invalid standup id")
	}

	// Validate the synthesizer (and therefore the API key) before touching the
	// semaphore or marking the job running. This makes the ErrNoAPIKey path in
	// OfficePostStandupAudio reachable and keeps the slot count accurate.
	synth, err := m.newSynth()
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	if m.running[id] {
		m.mu.Unlock()
		return id, nil // already generating this one; coalesce
	}
	// Acquire a concurrency slot (non-blocking) before marking running, so the
	// total number of simultaneous synthesis jobs is bounded.
	select {
	case m.sem <- struct{}{}:
	default:
		m.mu.Unlock()
		return "", ErrTooManyAudioJobs
	}
	m.running[id] = true
	m.wg.Add(1)
	m.mu.Unlock()

	go m.run(id, synth)
	return id, nil
}

// run executes the full synthesis pipeline for standupID using the already-
// validated synth. The synth is passed in from Start so that the API-key check
// happens synchronously before the semaphore is acquired (Fix 4).
func (m *AudioJobManager) run(id string, synth Synthesizer) {
	ctx, cancel := context.WithCancel(m.baseCtx)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			tmpPath := office.StandupAudioPath(id) + ".tmp"
			os.Remove(tmpPath) //nolint:errcheck
			m.broadcast("office:standupAudioError", map[string]interface{}{
				"id":      id,
				"message": fmt.Sprintf("audio generation panicked: %v", r),
			})
		}
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
		<-m.sem // release the concurrency slot
		m.wg.Done()
	}()

	m.broadcast("office:standupAudioProgress", map[string]interface{}{
		"id":    id,
		"stage": "summarizing",
	})

	ps, err := office.LoadPersistedStandup(id)
	if err != nil {
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": "failed to load standup: " + err.Error(),
		})
		return
	}

	entries := make([]tts.TranscriptEntry, 0, len(ps.Transcript))
	for _, msg := range ps.Transcript {
		entries = append(entries, tts.TranscriptEntry{
			SessionID:   msg.SessionID,
			SpeakerName: msg.SpeakerName,
			Type:        msg.Type,
			Content:     msg.Content,
		})
	}

	inputs := tts.BuildSpeechInputs(entries, m.wordBudget)
	if len(inputs) == 0 {
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": "no speakable content",
		})
		return
	}

	// Build transcript string for the LLM.
	lines := make([]string, 0, len(inputs))
	for _, inp := range inputs {
		lines = append(lines, inp.SpeakerName+" ("+inp.SessionID+"): "+inp.Text)
	}
	transcriptStr := strings.Join(lines, "\n")

	// Job context derives from the server root context, so an in-flight job is cancelled on shutdown (each OpenAI call is also bounded by the client's 90s timeout).
	script, err := synth.CondenseScript(ctx, transcriptStr, m.wordBudget)
	if err != nil {
		// Scrub: the tts client redacts at source, but this event is fanned to
		// all SSE clients, Telegram, and the replay buffer -- defense in depth.
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": logscrub.All("script condensation failed: " + err.Error()),
		})
		return
	}

	// Build ordered list of (voice, text, label) segments.
	type segment struct {
		voice string
		text  string
		label string
	}
	var segs []segment

	// Pre-build lookup maps from the original transcript order (inputs) so that:
	//   - speaker name lookup is O(1) per segment instead of O(N) (Fix 6d)
	//   - the stable voice index is derived from original-transcript order, not
	//     the LLM's segment order, giving each speaker a consistent voice.
	sessionName := make(map[string]string, len(inputs))
	sessionIdx := make(map[string]int, len(inputs))
	for _, inp := range inputs {
		if _, seen := sessionIdx[inp.SessionID]; !seen {
			sessionIdx[inp.SessionID] = len(sessionIdx)
			sessionName[inp.SessionID] = inp.SpeakerName
		}
	}

	if script.Intro != "" {
		segs = append(segs, segment{voice: "cedar", text: script.Intro, label: "intro"})
	}
	for _, seg := range script.Segments {
		if seg.Text == "" {
			continue
		}
		idx, known := sessionIdx[seg.SessionID]
		if !known {
			// Segment references a session not in the original inputs; assign a
			// new index at the tail so it still gets a deterministic voice.
			idx = len(sessionIdx)
			sessionIdx[seg.SessionID] = idx
		}
		voice := m.voiceFor(seg.SessionID, idx)
		label := seg.SessionID
		if name, ok := sessionName[seg.SessionID]; ok {
			label = name
		}
		segs = append(segs, segment{voice: voice, text: seg.Text, label: label})
	}

	// The condense step can return valid JSON with an empty intro and all-empty
	// segment texts. Without this guard the voicing loop runs zero times,
	// ConcatMP3 returns a 0-byte slice, and a 0-byte file would be written and
	// advertised as "ready" -- the client would serve an empty audio/mpeg.
	if len(segs) == 0 {
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": "no speakable content after condensing",
		})
		return
	}

	mp3Parts := make([][]byte, 0, len(segs))
	for _, s := range segs {
		m.broadcast("office:standupAudioProgress", map[string]interface{}{
			"id":     id,
			"stage":  "voicing",
			"detail": s.label,
		})
		mp3, err := synth.CreateSpeech(ctx, tts.SpeechRequest{
			Voice: s.voice,
			Input: s.text,
		})
		if err != nil {
			m.broadcast("office:standupAudioError", map[string]interface{}{
				"id":      id,
				"message": logscrub.All(fmt.Sprintf("speech synthesis failed for %s: %v", s.label, err)),
			})
			return
		}
		mp3Parts = append(mp3Parts, mp3)
	}

	data := tts.ConcatMP3(mp3Parts)

	// Fix 6c: guard against unreasonably large audio output before touching disk.
	if len(data) > maxAudioBytes {
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": fmt.Sprintf("audio output too large (%d bytes); refusing to write", len(data)),
		})
		return
	}

	audioPath := office.StandupAudioPath(id)
	tmpPath := audioPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": "failed to write audio file: " + err.Error(),
		})
		return
	}
	if err := os.Rename(tmpPath, audioPath); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		m.broadcast("office:standupAudioError", map[string]interface{}{
			"id":      id,
			"message": "failed to finalize audio file: " + err.Error(),
		})
		return
	}

	m.broadcast("office:standupAudioReady", map[string]interface{}{
		"id":  id,
		"url": "/api/office/standup/" + id + "/audio",
	})
}

// OfficePostStandupAudio starts (or coalesces) audio generation for a standup.
func (ctx *Context) OfficePostStandupAudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "standupId")
	resolved, err := ctx.AudioJobs.Start(id)
	if err != nil {
		if errors.Is(err, tts.ErrNoAPIKey) {
			request.WriteError(w, http.StatusBadRequest, "Configure your OpenAI key in Settings")
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			request.WriteError(w, http.StatusNotFound, "no completed standup found")
			return
		}
		if errors.Is(err, ErrTooManyAudioJobs) {
			request.WriteError(w, http.StatusTooManyRequests, "audio generation busy, try again shortly")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to start audio generation")
		return
	}
	request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "id": resolved})
}

// OfficeGetStandupAudio serves the cached MP3 for a standup.
func (ctx *Context) OfficeGetStandupAudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "standupId")
	if id == "latest" {
		latest, err := office.LatestCompletedStandupID()
		if err != nil {
			request.WriteError(w, http.StatusNotFound, "no completed standup found")
			return
		}
		id = latest
	}
	if !safeName(id) {
		request.WriteError(w, http.StatusBadRequest, "invalid standup ID")
		return
	}
	if _, err := os.Stat(office.StandupAudioPath(id)); err != nil {
		request.WriteError(w, http.StatusNotFound, "audio not generated yet")
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	http.ServeFile(w, r, office.StandupAudioPath(id))
}
