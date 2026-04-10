package transcript

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	pollInterval = 5 * time.Second
)

// Watcher monitors a Claude Code transcript JSONL file.
type Watcher struct {
	mu             sync.Mutex
	transcriptPath string
	sessionID      string
	filePos        int64
	isRunning      bool
	events         chan TranscriptEvent
	ctx            context.Context
	cancel         context.CancelFunc
	closeOnce      sync.Once
}

// NewWatcher creates a new transcript watcher.
func NewWatcher(ctx context.Context, sessionID string) *Watcher {
	childCtx, cancel := context.WithCancel(ctx)
	return &Watcher{
		sessionID: sessionID,
		events:    make(chan TranscriptEvent, 32),
		ctx:       childCtx,
		cancel:    cancel,
	}
}

// Start begins watching the transcript file.
func (w *Watcher) Start(transcriptPath string) {
	w.mu.Lock()
	w.transcriptPath = transcriptPath
	w.isRunning = true
	w.mu.Unlock()

	go w.watchLoop()
}

// Stop halts the watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	w.isRunning = false
	w.mu.Unlock()
	w.cancel()
	w.closeOnce.Do(func() { close(w.events) })
}

// Events returns the event channel.
func (w *Watcher) Events() <-chan TranscriptEvent {
	return w.events
}

func (w *Watcher) watchLoop() {
	// Poll for file existence first
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.mu.Lock()
		path := w.transcriptPath
		w.mu.Unlock()

		if _, err := os.Stat(path); err == nil {
			break // File exists, proceed to reading
		}

		select {
		case <-w.ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}

	// Read new content periodically
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.readNewContent()
		}
	}
}

func (w *Watcher) readNewContent() {
	w.mu.Lock()
	path := w.transcriptPath
	pos := w.filePos
	w.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return
	}

	// Handle file truncation/rotation
	if stat.Size() < pos {
		pos = 0
		f.Seek(0, 0)
	}

	if stat.Size() == pos {
		return
	}

	if pos > 0 {
		f.Seek(pos, 0)
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		w.processEntry(entry)
	}

	// Use the actual file position instead of accumulating len(line)+1, which
	// is wrong for \r\n line endings. Scanner reads ahead in chunks, so after
	// Scan returns false, the file position reflects everything consumed.
	newPos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		newPos = pos
	}
	w.mu.Lock()
	w.filePos = newPos
	w.mu.Unlock()
}

func (w *Watcher) processEntry(entry TranscriptEntry) {
	hookType := detectHookEvent(entry.Type)
	if hookType == "" {
		return
	}

	select {
	case w.events <- HookDetectedEvent{Hook: hookType, SessionID: w.sessionID}:
	default:
	}
}

func detectHookEvent(entryType string) HookEvent {
	switch {
	case strings.Contains(entryType, "permission_prompt"):
		return HookPermissionPrompt
	case strings.Contains(entryType, "elicitation"):
		return HookElicitation
	case strings.Contains(entryType, "idle_prompt"):
		return HookIdlePrompt
	case entryType == "stop":
		return HookStop
	default:
		return ""
	}
}
