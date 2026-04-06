package respawn

// Default prompts for the respawn cycle steps.
const (
	// DefaultUpdatePrompt is sent at the start of each respawn cycle.
	DefaultUpdatePrompt = "update all the docs and CLAUDE.md"

	// ClearCommand is sent to reset Claude's context.
	ClearCommand = "/clear"

	// InitCommand is sent to re-initialize Claude with CLAUDE.md context.
	InitCommand = "/init"
)

// SessionWriter is the interface for writing input to a Claude session.
// Implementations typically use screen -X stuff or PTY write to deliver text.
type SessionWriter interface {
	// WriteToSession sends text to the specified session. The text should
	// include any necessary line terminators (e.g., "\r" for Enter).
	// Returns true if the write succeeded.
	WriteToSession(sessionID string, text string) bool
}

// SetSessionWriter configures the writer used to send prompts to the session.
// Must be called before Start() for the respawn cycle to function.
func (rc *Controller) SetSessionWriter(w SessionWriter) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.writer = w
}

// sendToSession writes input to the session with retry logic.
// Returns true if the send succeeded.
func (rc *Controller) sendToSession(input string, stepName string) bool {
	rc.mu.Lock()
	writer := rc.writer
	sessionID := rc.sessionID
	rc.mu.Unlock()

	if writer == nil {
		rc.emitEvent(ErrorEvent{Err: ErrNoSessionWriter})
		return false
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if writer.WriteToSession(sessionID, input) {
			return true
		}
		rc.logAction("error", stepName+" send failed (attempt "+itoa(attempt)+"/"+itoa(maxRetries)+")")
	}

	rc.logAction("error", "all send attempts failed: "+stepName)
	rc.emitEvent(ErrorEvent{Err: ErrSendFailed})
	return false
}

// itoa converts a small int to a string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf) - 1
	for n > 0 {
		buf[pos] = byte('0' + n%10)
		pos--
		n /= 10
	}
	return string(buf[pos+1:])
}
