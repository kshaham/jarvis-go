package pty

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
)

const (
	readBufSize        = 32 * 1024 // 32KB read buffer
	readDeadline       = 500 * time.Millisecond
	idleDetectionDelay = 2 * time.Second
	eventChannelSize   = 256
)

// workingIndicators is a package-level slice to avoid allocation per call.
var workingIndicators = []string{
	"Thinking", "Writing", "Reading", "Running",
	"\u280B", "\u2819", "\u2839", "\u2838", "\u283C", "\u2834", "\u2826", "\u2827",
}

// Session wraps a PTY process running Claude CLI or a shell.
type Session struct {
	mu sync.RWMutex
	wg sync.WaitGroup

	// Identity (immutable after construction)
	ID         string
	WorkingDir string
	CreatedAt  time.Time
	Mode       SessionMode

	// Mutable state (guarded by mu)
	name           string
	status         SessionStatus
	pid            int
	isWorking      bool
	lastActivityAt time.Time
	lastPromptTime   time.Time
	lastAutoResponse time.Time
	isStopped      bool

	// Token tracking
	totalInputTokens  int64
	totalOutputTokens int64

	// Buffers
	terminalBuffer *BufferAccumulator
	textOutput     *BufferAccumulator

	// PTY handle
	ptyFile *os.File
	cmd     *exec.Cmd

	// Screen integration
	screenManager ScreenManagerInterface
	useScreen     bool

	// Event channels
	events     chan SessionEvent
	done       chan struct{}
	cancelFunc context.CancelFunc
	idleReset  chan struct{}

	// Provider env
	providerEnv map[string]string
	niceConfig  NiceConfig

	// Debounce for heavy processing
	pendingData   strings.Builder
	debounceTimer *time.Timer

	// Output observers (fan-out for Telegram streaming, etc.)
	outputObservers   map[int]func(string)
	outputObserverSeq int
	observerMu        sync.Mutex

	// Active tool tracking
	activeTools map[string]bool
}

// NewSession creates a new session (does not start it).
func NewSession(config SessionConfig) *Session {
	id := config.ID
	if id == "" {
		id = uuid.New().String()
	}

	return &Session{
		ID:             id,
		WorkingDir:     config.WorkingDir,
		CreatedAt:      time.Now(),
		Mode:           config.Mode,
		name:           config.Name,
		status:         StatusStopped,
		terminalBuffer: NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo),
		textOutput:     NewBufferAccumulator(MaxTextOutputSize, TrimTextTo),
		screenManager:  config.ScreenManager,
		useScreen:      config.UseScreen,
		events:         make(chan SessionEvent, eventChannelSize),
		done:           make(chan struct{}),
		idleReset:      make(chan struct{}, 1),
		providerEnv:    config.ProviderEnv,
		niceConfig:     config.NiceConfig,
	}
}

// Start spawns the PTY process and begins the read loop.
func (s *Session) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.status != StatusStopped {
		s.mu.Unlock()
		return fmt.Errorf("session already started")
	}
	s.status = StatusBusy
	s.lastActivityAt = time.Now()
	s.mu.Unlock()

	sessionCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancelFunc = cancel
	s.mu.Unlock()

	var cmd *exec.Cmd
	switch s.Mode {
	case ModeClaude:
		// Resolve claude binary path to avoid symlink race during deploys.
		claudePath, lookErr := exec.LookPath("claude")
		if lookErr != nil {
			claudePath = "claude" // fallback to PATH resolution
		}
		cmd = exec.CommandContext(sessionCtx, claudePath, "--dangerously-skip-permissions")
	case ModeShell:
		cmd = exec.CommandContext(sessionCtx, "bash", "-l", "-i")
	default:
		cmd = exec.CommandContext(sessionCtx, "bash", "-l", "-i")
	}

	cmd.Dir = s.WorkingDir
	cmd.Env = append(os.Environ(), s.buildEnv()...)
	// Ensure TERM is set for interactive shells
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 200, Rows: 50})
	if err != nil {
		cancel()
		s.mu.Lock()
		s.status = StatusError
		s.mu.Unlock()
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	s.mu.Lock()
	s.ptyFile = ptmx
	s.cmd = cmd
	if cmd.Process != nil {
		s.pid = cmd.Process.Pid
	}
	s.mu.Unlock()

	// Track both goroutines so we can close channels safely after both exit.
	s.wg.Add(2)

	// Start the read loop goroutine
	go s.readLoop(sessionCtx)
	// Start idle detector goroutine
	go s.idleDetector(sessionCtx)

	// Cleanup goroutine: close channels only after both goroutines finish.
	go func() {
		s.wg.Wait()
		close(s.done)
		close(s.events)
	}()

	return nil
}

// Stop gracefully stops the session.
func (s *Session) Stop() error {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return nil
	}
	s.isStopped = true
	s.status = StatusStopped

	// Stop debounce timer if it exists
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.debounceTimer = nil
	}

	cancelFn := s.cancelFunc
	ptyF := s.ptyFile
	cmd := s.cmd
	s.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}

	if ptyF != nil {
		_ = ptyF.Close()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	// Wait for both goroutines to finish (done is closed by cleanup goroutine)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}

	return nil
}

// Events returns the read-only event channel.
func (s *Session) Events() <-chan SessionEvent {
	return s.events
}

// AddOutputObserver registers a callback that receives terminal output data.
// Returns an unsubscribe function. Thread-safe; can be called from any goroutine.
func (s *Session) AddOutputObserver(cb func(string)) func() {
	s.observerMu.Lock()
	if s.outputObservers == nil {
		s.outputObservers = make(map[int]func(string))
	}
	s.outputObserverSeq++
	id := s.outputObserverSeq
	s.outputObservers[id] = cb
	s.observerMu.Unlock()

	return func() {
		s.observerMu.Lock()
		delete(s.outputObservers, id)
		s.observerMu.Unlock()
	}
}

// notifyOutputObservers fans out terminal data to all registered observers.
func (s *Session) notifyOutputObservers(data string) {
	s.observerMu.Lock()
	observers := make([]func(string), 0, len(s.outputObservers))
	for _, cb := range s.outputObservers {
		observers = append(observers, cb)
	}
	s.observerMu.Unlock()

	for _, cb := range observers {
		cb(data)
	}
}

// Done returns a channel that closes when the session stops.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// inkChunkSize is the max bytes per PTY write for Ink compatibility.
// Ink's input buffer can't handle very large writes; the Node.js version
// uses 700-byte chunks via GNU Screen's stuff command.
const inkChunkSize = 700

// WriteViaScreen sends input through the screen session, or directly to PTY.
// For normal user input, writes directly. For programmatic large writes to
// Claude/Ink sessions, use WriteInkPrompt() which chunks text and sends Enter separately.
func (s *Session) WriteViaScreen(input string) bool {
	if s.screenManager != nil {
		return s.screenManager.SendInput(s.ID, input)
	}
	// Direct PTY write -- pass through as-is for user input
	return s.WriteToPTY(input)
}

// WriteInkPrompt sends a large prompt to a Claude/Ink session by chunking
// the text and sending Enter separately. This is needed because Ink's input
// buffer can't handle large writes, and text+Enter must be separate PTY writes.
func (s *Session) WriteInkPrompt(prompt string) bool {
	prompt = strings.TrimRight(prompt, " \t\r\n")
	if prompt == "" {
		return false
	}

	// Send text in chunks to avoid overflowing Ink's input buffer
	for i := 0; i < len(prompt); i += inkChunkSize {
		end := i + inkChunkSize
		if end > len(prompt) {
			end = len(prompt)
		}
		if !s.WriteToPTY(prompt[i:end]) {
			return false
		}
		// Small delay between chunks so Ink can process each one
		if end < len(prompt) {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Delay before sending Enter so Ink finishes processing all pasted text.
	// Longer prompts need more time -- scale delay by prompt length.
	baseDelay := 2 * time.Second
	if len(prompt) > 2000 {
		baseDelay = 3 * time.Second
	}
	time.Sleep(baseDelay)

	// Send CR multiple times with delays as reliability measure.
	// Ink sometimes doesn't respond to the first CR if it's still processing.
	for attempt := 0; attempt < 3; attempt++ {
		s.WriteToPTY("\r")
		time.Sleep(800 * time.Millisecond)
	}
	return true
}

// WriteToPTY writes directly to the PTY file descriptor.
func (s *Session) WriteToPTY(input string) bool {
	s.mu.RLock()
	ptyFile := s.ptyFile
	s.mu.RUnlock()
	if ptyFile == nil {
		return false
	}
	_, err := ptyFile.Write([]byte(input))
	return err == nil
}

// Resize changes the PTY window size.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.RLock()
	ptyFile := s.ptyFile
	s.mu.RUnlock()
	if ptyFile == nil {
		return fmt.Errorf("no PTY file")
	}
	return pty.Setsize(ptyFile, &pty.Winsize{Cols: cols, Rows: rows})
}

// --- State getters ---

// Status returns the current session status.
func (s *Session) Status() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// IsWorking returns whether the session is actively working.
func (s *Session) IsWorking() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isWorking
}

// Name returns the session name.
func (s *Session) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SetName updates the session name.
func (s *Session) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// PID returns the process ID of the PTY child.
func (s *Session) PID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pid
}

// Tokens returns input, output, and total token counts.
func (s *Session) Tokens() (input, output, total int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalInputTokens, s.totalOutputTokens, s.totalInputTokens + s.totalOutputTokens
}

// TerminalBuffer returns the accumulated terminal buffer content.
func (s *Session) TerminalBuffer() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.terminalBuffer.Value()
}

// TextOutput returns the accumulated text output content.
func (s *Session) TextOutput() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.textOutput.Value()
}

// GetActiveTools returns the list of currently active (started but not completed) tools.
func (s *Session) GetActiveTools() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeTools == nil {
		return []string{}
	}
	tools := make([]string, 0, len(s.activeTools))
	for name := range s.activeTools {
		tools = append(tools, name)
	}
	return tools
}

// --- Internal ---

func (s *Session) readLoop(ctx context.Context) {
	defer s.wg.Done()

	// PTY file descriptors don't support SetReadDeadline (they're not net.Conn).
	// Use a goroutine to do blocking reads, and select on ctx.Done() for cancellation.
	type readResult struct {
		data []byte
		err  error
	}
	readCh := make(chan readResult, 1)

	for {
		s.mu.RLock()
		ptyFile := s.ptyFile
		s.mu.RUnlock()
		if ptyFile == nil {
			s.reapProcess()
			return
		}

		// Start blocking read in a goroutine with its own buffer to avoid data races.
		go func(f *os.File) {
			buf := make([]byte, readBufSize)
			n, err := f.Read(buf)
			if err != nil {
				readCh <- readResult{nil, err}
			} else {
				readCh <- readResult{buf[:n], nil}
			}
		}(ptyFile)

		// Wait for either read completion or context cancellation
		select {
		case <-ctx.Done():
			s.reapProcess()
			return
		case result := <-readCh:
			if result.err != nil {
				// PTY closed (process exited)
				s.reapProcess()
				s.handleExit()
				return
			}
			s.processOutput(string(result.data))
		}
	}
}

// reapProcess calls cmd.Wait() to reap the child process and populate ProcessState.
func (s *Session) reapProcess() {
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
}

func (s *Session) processOutput(rawData string) {
	data := FilterFocusSequences(rawData)
	data = FilterCtrlL(data)
	if data == "" {
		return
	}

	s.mu.Lock()
	s.terminalBuffer.Append(data)
	s.lastActivityAt = time.Now()
	s.mu.Unlock()

	// Emit raw terminal event (immediate, for real-time display)
	s.emitEvent(EventTerminal{Data: data})
	s.emitEvent(EventOutput{Data: data})
	s.notifyOutputObservers(data)

	// Detect idle/working state (immediate, latency-sensitive)
	clean := StripANSI(data)
	s.detectWorkingState(data, clean)

	// Auto-respond to Claude Code interactive selection menus.
	// When Claude presents a numbered choice (❯ 1. ...), agents get stuck.
	// Auto-select option 1 to keep agents moving autonomously.
	s.detectAndAutoRespondMenu(clean)

	// Detect unsent pasted text at the prompt and send CR to submit it.
	if s.Mode == ModeClaude && strings.Contains(clean, "[Pasted text") {
		s.mu.Lock()
		lastAuto := s.lastAutoResponse
		s.mu.Unlock()
		if lastAuto.IsZero() || time.Since(lastAuto) > 3*time.Second {
			s.mu.Lock()
			s.lastAutoResponse = time.Now()
			ptyFile := s.ptyFile
			s.mu.Unlock()
			if ptyFile != nil {
				go func() {
					time.Sleep(1 * time.Second)
					ptyFile.Write([]byte("\r"))
					slog.Info("Auto-submitted pasted text", "session", s.ID)
				}()
			}
		}
	}

	// Track active tool invocations
	s.trackToolInvocations(clean)

	// Debounced: token parsing (100ms batch)
	s.mu.Lock()
	s.pendingData.WriteString(clean)
	if s.debounceTimer == nil {
		s.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
			s.mu.Lock()
			pending := s.pendingData.String()
			s.pendingData.Reset()
			s.debounceTimer = nil
			s.mu.Unlock()

			if pending != "" {
				s.parseTokensFromStatusLine(pending)
			}
		})
	}
	s.mu.Unlock()
}

// detectAndAutoRespondMenu detects Claude Code's Ink interactive selection menus
// and auto-selects option 1 to prevent agents from getting stuck.
// Menus have patterns like "❯ 1. Title" with numbered options below.
func (s *Session) detectAndAutoRespondMenu(cleanData string) {
	// Only auto-respond in Claude mode (not shell sessions)
	if s.Mode != ModeClaude {
		return
	}

	// Debounce: don't auto-respond more than once per 5 seconds
	s.mu.Lock()
	lastAuto := s.lastAutoResponse
	s.mu.Unlock()
	if !lastAuto.IsZero() && time.Since(lastAuto) < 5*time.Second {
		return
	}

	// Detect selection menu patterns:
	// - "❯ 1." at start of a line (Ink's selection arrow)
	// - "Pick task" or "Which" question followed by numbered items
	// - "Type something" as an option (Claude Code's free-text fallback)
	hasSelector := strings.Contains(cleanData, "❯") || strings.Contains(cleanData, ">")
	hasNumbered := strings.Contains(cleanData, "1.") && strings.Contains(cleanData, "2.")
	hasTypeSomething := strings.Contains(cleanData, "Type something")
	hasPickTask := strings.Contains(cleanData, "Pick task") || strings.Contains(cleanData, "Which task") || strings.Contains(cleanData, "Which option")
	hasChatAbout := strings.Contains(cleanData, "Chat about this")

	if hasSelector && hasNumbered && (hasTypeSomething || hasPickTask || hasChatAbout) {
		s.mu.Lock()
		s.lastAutoResponse = time.Now()
		ptyFile := s.ptyFile
		s.mu.Unlock()

		if ptyFile != nil {
			// Send "1" + Enter to select the first option
			go func() {
				time.Sleep(500 * time.Millisecond) // brief delay for Ink to be ready
				ptyFile.Write([]byte("1"))
				time.Sleep(200 * time.Millisecond)
				ptyFile.Write([]byte("\r"))
				slog.Info("Auto-selected menu option 1", "session", s.ID)
			}()
		}
	}
}

func (s *Session) detectWorkingState(rawData, cleanData string) {
	// Check for prompt character (idle indicator)
	if strings.Contains(rawData, "\u276f") || strings.Contains(rawData, "\u2771") {
		// Signal idle detector
		select {
		case s.idleReset <- struct{}{}:
		default:
		}
	}

	// Check for working indicators (package-level slice to avoid per-call allocation)
	for _, indicator := range workingIndicators {
		if strings.Contains(cleanData, indicator) {
			s.mu.Lock()
			if !s.isWorking {
				s.isWorking = true
				s.status = StatusBusy
				s.mu.Unlock()
				s.emitEvent(EventWorking{})
			} else {
				s.mu.Unlock()
			}
			return
		}
	}
}

func (s *Session) idleDetector(ctx context.Context) {
	defer s.wg.Done()

	timer := time.NewTimer(idleDetectionDelay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.idleReset:
			// Safely reset: stop and drain before resetting
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleDetectionDelay)
		case <-timer.C:
			s.mu.Lock()
			wasWorking := s.isWorking
			isInitialReady := s.status == StatusBusy && !s.isWorking
			if wasWorking || isInitialReady {
				s.isWorking = false
				s.status = StatusIdle
				s.lastPromptTime = time.Now()
				s.mu.Unlock()
				s.emitEvent(EventIdle{})
			} else {
				s.mu.Unlock()
			}
		}
	}
}

func (s *Session) trackToolInvocations(cleanData string) {
	invocations := ParseToolInvocations(cleanData)
	if len(invocations) == 0 {
		return
	}

	s.mu.Lock()
	if s.activeTools == nil {
		s.activeTools = make(map[string]bool)
	}
	for _, inv := range invocations {
		switch inv.Status {
		case "started":
			s.activeTools[inv.Name] = true
		case "completed":
			delete(s.activeTools, inv.Name)
		}
	}
	s.mu.Unlock()
}

func (s *Session) parseTokensFromStatusLine(cleanData string) {
	tokenCount, ok := ParseTokenCount(cleanData)
	if !ok {
		return
	}

	s.mu.Lock()
	currentTotal := s.totalInputTokens + s.totalOutputTokens
	if tokenCount > currentTotal {
		delta := tokenCount - currentTotal
		if delta > MaxDeltaPerUpdate {
			s.mu.Unlock()
			return
		}
		// Estimate 60/40 input/output split
		inputDelta := int64(float64(delta) * 0.6)
		outputDelta := delta - inputDelta
		s.totalInputTokens += inputDelta
		s.totalOutputTokens += outputDelta
	}
	input := s.totalInputTokens
	output := s.totalOutputTokens
	total := input + output
	s.mu.Unlock()

	// Emit token update event outside the lock
	if tokenCount > currentTotal {
		s.emitEvent(EventTokens{Input: input, Output: output, Total: total})
	}
}

func (s *Session) handleExit() {
	exitCode := -1
	if s.cmd != nil && s.cmd.ProcessState != nil {
		exitCode = s.cmd.ProcessState.ExitCode()
	}

	s.mu.Lock()
	s.status = StatusStopped
	s.isWorking = false
	s.pid = 0
	s.mu.Unlock()

	s.emitEvent(EventExit{ExitCode: exitCode})
}

func (s *Session) emitEvent(event SessionEvent) {
	select {
	case s.events <- event:
	default:
		// Channel full, drop event (backpressure)
	}
}

func (s *Session) buildEnv() []string {
	env := []string{
		fmt.Sprintf("JARVIS_SESSION_ID=%s", s.ID),
	}
	for k, v := range s.providerEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}
