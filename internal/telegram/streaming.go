package telegram

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// ansiRegex matches ANSI escape sequences for stripping from terminal output.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// inkControlRegex matches Ink-specific control sequences (cursor movement, line clearing).
var inkControlRegex = regexp.MustCompile(`\x1b\[\?25[hl]|\x1b\[\d*[ABCDJK]|\x1b\[2K|\x1b\[\d+;\d+H|\r`)

const (
	streamFlushSize    = 2000 // flush when buffer exceeds this many bytes
	streamFlushDelay   = 500 * time.Millisecond
	streamMaxMessage   = 3900 // leave room for Telegram's 4096 limit
	streamTruncateNote = "\n[...truncated]"
)

// OutputStreamer buffers session terminal output and flushes to Telegram.
type OutputStreamer struct {
	mu      sync.Mutex
	buffers map[string]*outputBuffer // chatID -> buffer
	queue   *MessageQueue
}

type outputBuffer struct {
	data       strings.Builder
	lastAppend time.Time
	dirty      atomic.Bool
	chatID     string
	timer      *time.Timer // managed carefully to avoid races
}

// NewOutputStreamer creates a new output streamer.
func NewOutputStreamer(queue *MessageQueue) *OutputStreamer {
	return &OutputStreamer{
		buffers: make(map[string]*outputBuffer),
		queue:   queue,
	}
}

// HandleOutput processes terminal output for a linked chat.
// Called from the session output observer callback.
func (s *OutputStreamer) HandleOutput(chatID, data string) {
	cleaned := stripTerminalArtifacts(data)
	if cleaned == "" {
		return
	}

	s.mu.Lock()
	buf, exists := s.buffers[chatID]
	if !exists {
		buf = &outputBuffer{chatID: chatID}
		s.buffers[chatID] = buf
	}
	buf.data.WriteString(cleaned)
	buf.lastAppend = time.Now()
	buf.dirty.Store(true)
	currentSize := buf.data.Len()
	s.mu.Unlock()

	// Flush immediately if buffer is large enough.
	if currentSize >= streamFlushSize {
		s.flushBuffer(chatID)
		return
	}

	// Schedule a delayed flush using time.AfterFunc. The callback flushes only
	// if the live buffer for chatID is STILL this exact object -- if RemoveChat
	// deleted it and HandleOutput recreated a new buffer for the same chatID
	// while the timer was in flight, an unconditional flush would prematurely
	// flush the new buffer mid-fill. Identity is checked under the lock.
	s.mu.Lock()
	if buf.timer != nil {
		buf.timer.Stop()
	}
	captured := buf
	buf.timer = time.AfterFunc(streamFlushDelay, func() {
		s.flushBufferMatching(chatID, captured)
	})
	s.mu.Unlock()
}

// FlushAll forces all buffers to flush immediately.
func (s *OutputStreamer) FlushAll() {
	s.mu.Lock()
	chatIDs := make([]string, 0, len(s.buffers))
	for chatID := range s.buffers {
		chatIDs = append(chatIDs, chatID)
	}
	s.mu.Unlock()

	for _, chatID := range chatIDs {
		s.flushBuffer(chatID)
	}
}

// FlushChat forces a specific chat's buffer to flush.
func (s *OutputStreamer) FlushChat(chatID string) {
	s.flushBuffer(chatID)
}

// RemoveChat removes and flushes a chat's buffer.
func (s *OutputStreamer) RemoveChat(chatID string) {
	s.flushBuffer(chatID)

	s.mu.Lock()
	if buf, exists := s.buffers[chatID]; exists {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		delete(s.buffers, chatID)
	}
	s.mu.Unlock()
}

func (s *OutputStreamer) flushBuffer(chatID string) {
	s.flushBufferMatching(chatID, nil)
}

// flushBufferMatching flushes chatID's buffer. When expected is non-nil it only
// flushes if the live buffer is still that exact object (timer-path identity
// guard); nil means flush whatever is current (FlushAll/FlushChat/RemoveChat).
func (s *OutputStreamer) flushBufferMatching(chatID string, expected *outputBuffer) {
	s.mu.Lock()
	buf, exists := s.buffers[chatID]
	if !exists || buf.data.Len() == 0 || (expected != nil && buf != expected) {
		s.mu.Unlock()
		return
	}

	text := buf.data.String()
	buf.data.Reset()
	buf.dirty.Store(false)
	if buf.timer != nil {
		buf.timer.Stop()
		buf.timer = nil
	}
	s.mu.Unlock()

	// Truncate if necessary, backing up to a rune boundary so we never split a
	// multi-byte rune (the truncated text is HTML-escaped below; a half rune
	// would corrupt the <pre> payload).
	if len(text) > streamMaxMessage {
		limit := streamMaxMessage - len(streamTruncateNote)
		for limit > 0 && !utf8.RuneStart(text[limit]) {
			limit--
		}
		text = text[:limit] + streamTruncateNote
	}

	// Wrap in <pre> tags for monospace rendering.
	formatted := "<pre>" + escapeHTML(text) + "</pre>"

	s.queue.Enqueue(chatID, formatted, nil)
}

// stripTerminalArtifacts removes ANSI codes, Ink control sequences, and other noise.
func stripTerminalArtifacts(s string) string {
	s = ansiRegex.ReplaceAllString(s, "")
	s = inkControlRegex.ReplaceAllString(s, "")
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// Collapse multiple blank lines
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// escapeHTML escapes HTML special characters for Telegram messages.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
