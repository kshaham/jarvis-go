package pty

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	vtpkg "github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/safego"
)

// vtScreenCols / vtScreenRows match the PTY winsize used at session
// start (see pty.StartWithSize call further down). Keeping the vt
// emulator the same dimensions means cursor positions and wrapped
// lines render identically to what claude actually sees.
const (
	vtScreenCols = 200
	vtScreenRows = 50
)

const (
	readBufSize      = 32 * 1024 // 32KB read buffer
	readDeadline     = 500 * time.Millisecond
	eventChannelSize = 256
)

// idleDetectionDelay is how long the idle detector waits after the last
// activity before declaring a session idle. A package var (not a const) so
// tests can shrink it to provoke the timer stop/drain race in idleDetector
// deterministically; production never reassigns it.
var idleDetectionDelay = 2 * time.Second

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
	name             string
	status           SessionStatus
	pid              int
	isWorking        bool
	lastActivityAt   time.Time
	lastPromptTime   time.Time
	lastAutoResponse time.Time
	isStopped        bool

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
	sessionCtx context.Context // set in Start(); used by background goroutines that outlive a single function call
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

	// Spawn option. When true, the Claude CLI is spawned without
	// --dangerously-skip-permissions so interactive /login works.
	// Required because the CLI refuses that flag as root.
	omitSkipPermissions bool

	// claudeBinaryPath overrides the PATH-resolved `claude` binary.
	// Empty means resolve via exec.LookPath. Tests point this at
	// cmd/fakeclaude so the office integration path can run without
	// a real Claude install.
	claudeBinaryPath string

	// hooksEnabled flags ModeClaude sessions whose office spawner has
	// written a .claude/settings.local.json into WorkingDir. Today
	// this is informational only -- the binary itself doesn't behave
	// differently. Surfaced via Status() so the dashboard can render
	// the "via hooks" badge.
	hooksEnabled bool

	// R1.3 · Per-session CLI flags mirrored from SessionConfig. PTY
	// sessions get the same flag surface as SDK sessions where the
	// `claude` CLI exposes it. Empty / zero values mean "skip".
	systemPromptFile       string
	appendSystemPromptFile string
	effort                 string
	// model mirrors SessionConfig.Model. The SDK path forwards it via
	// claude.WithModel; the PTY path now forwards it via --model (without
	// this it was silently ignored, so PTY/hooks sessions ran the CLI
	// default regardless of the team's pinned model).
	model                              string
	fallbackModel                      string
	excludeDynamicSystemPromptSections bool
	strictMcpConfig                    bool
	// R3.6 · Optional GitHub PR linkage forwarded via --from-pr <n>.
	fromPRNumber int
	// R4.1 · Optional browser-automation tools forwarded via --chrome.
	chromeEnabled bool
	// R4.3 · Optional channel subscriptions forwarded as repeated
	// --channels args (one per entry).
	channels []string

	// vtEmulator is the canonical screen-state model for this PTY.
	// Every byte chunk read from the PTY is fed into it before the
	// per-chunk heuristic detectors run. SafeEmulator is the
	// concurrency-safe variant so the read loop, idle detector, and
	// dashboard snapshot calls don't need to coordinate locking.
	//
	// We keep it alongside (not replacing) the existing
	// terminalBuffer + StripANSI parsing so the v1 swap is purely
	// additive: existing code paths see no behavioural change, but
	// new code can call Snapshot() to read a faithful screen view
	// instead of doing per-chunk substring searches that miss when
	// ANSI escape sequences split across PTY reads.
	vtEmulator *vtpkg.SafeEmulator
}

// NewSession creates a new session (does not start it).
func NewSession(config SessionConfig) *Session {
	id := config.ID
	if id == "" {
		id = uuid.New().String()
	}

	return &Session{
		ID:                  id,
		WorkingDir:          config.WorkingDir,
		CreatedAt:           time.Now(),
		Mode:                config.Mode,
		name:                config.Name,
		status:              StatusStopped,
		terminalBuffer:      NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo),
		textOutput:          NewBufferAccumulator(MaxTextOutputSize, TrimTextTo),
		screenManager:       config.ScreenManager,
		useScreen:           config.UseScreen,
		events:              make(chan SessionEvent, eventChannelSize),
		done:                make(chan struct{}),
		idleReset:           make(chan struct{}, 1),
		providerEnv:         config.ProviderEnv,
		niceConfig:          config.NiceConfig,
		omitSkipPermissions: config.OmitSkipPermissions,
		claudeBinaryPath:    config.ClaudeBinaryPath,
		hooksEnabled:        config.HooksEnabled,
		vtEmulator:          vtpkg.NewSafeEmulator(vtScreenCols, vtScreenRows),

		systemPromptFile:                   config.SystemPromptFile,
		appendSystemPromptFile:             config.AppendSystemPromptFile,
		effort:                             config.Effort,
		model:                              config.Model,
		fallbackModel:                      config.FallbackModel,
		excludeDynamicSystemPromptSections: config.ExcludeDynamicSystemPromptSections,
		strictMcpConfig:                    config.StrictMcpConfig,
		fromPRNumber:                       config.FromPRNumber,
		chromeEnabled:                      config.ChromeEnabled,
		channels:                           sanitizeChannels(config.Channels, "pty:"+id),
	}
}

// Snapshot returns the current rendered screen as plain text. Reads
// from the SafeEmulator so it's race-free against the read loop.
// Empty string means the emulator has no buffered content yet (fresh
// session that hasn't produced output). Useful for the dashboard's
// stable-screen view and for any future detector that wants to
// reason about the full screen state instead of a single PTY chunk.
func (s *Session) Snapshot() string {
	if s.vtEmulator == nil {
		return ""
	}
	return s.vtEmulator.String()
}

// HooksEnabled returns whether this session was spawned with Claude
// Code hooks wired to the daemon. Read by the office payload builder
// so the per-agent card can render a "via hooks" badge.
func (s *Session) HooksEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hooksEnabled
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
	s.sessionCtx = sessionCtx
	s.mu.Unlock()

	var cmd *exec.Cmd
	switch s.Mode {
	case ModeClaude:
		// Resolve claude binary path to avoid symlink race during deploys.
		// Tests can override via SessionConfig.ClaudeBinaryPath (used by
		// cmd/fakeclaude in the office integration suite).
		var claudePath string
		if s.claudeBinaryPath != "" {
			claudePath = s.claudeBinaryPath
		} else {
			var lookErr error
			claudePath, lookErr = exec.LookPath("claude")
			if lookErr != nil {
				claudePath = "claude" // fallback to PATH resolution
			}
		}
		// Auth sessions (omitSkipPermissions=true) need the flag
		// dropped -- Claude CLI refuses --dangerously-skip-permissions
		// when running as root and exits immediately, which kills the
		// /login flow. Regular "new Claude session" button users keep
		// the flag so tool-call prompts don't interrupt the loop.
		claudeArgs := []string{}
		if !s.omitSkipPermissions {
			claudeArgs = append(claudeArgs, "--dangerously-skip-permissions")
		}
		// R1.3 · forward the same per-session knobs as the SDK path.
		// All conditional so an unconfigured session matches the
		// pre-R1.3 invocation byte-for-byte.
		if s.systemPromptFile != "" {
			claudeArgs = append(claudeArgs, "--system-prompt-file", s.systemPromptFile)
		}
		if s.appendSystemPromptFile != "" {
			claudeArgs = append(claudeArgs, "--append-system-prompt-file", s.appendSystemPromptFile)
		}
		if s.excludeDynamicSystemPromptSections {
			claudeArgs = append(claudeArgs, "--exclude-dynamic-system-prompt-sections")
		}
		if effort := normaliseEffortString(s.effort); effort != "" {
			claudeArgs = append(claudeArgs, "--effort", effort)
		}
		// Forward the pinned model (e.g. claude-opus-4-8 for strategic-tier
		// leads). Previously the PTY path omitted --model, so hooks/standard
		// sessions silently ran the CLI default — only the SDK path honored
		// SessionConfig.Model.
		if s.model != "" {
			claudeArgs = append(claudeArgs, "--model", s.model)
		}
		if s.fallbackModel != "" {
			claudeArgs = append(claudeArgs, "--fallback-model", s.fallbackModel)
		}
		if s.strictMcpConfig {
			claudeArgs = append(claudeArgs, "--strict-mcp-config")
		}
		// R3.6 · --from-pr <n> loads PR diff + description into the
		// system prompt so the agent has the PR context on its first
		// turn. Zero leaves the flag off so non-PR teams are unchanged.
		if s.fromPRNumber > 0 {
			claudeArgs = append(claudeArgs, "--from-pr", strconv.Itoa(s.fromPRNumber))
		}
		// R4.1 · --chrome enables browser-automation tools (navigate,
		// click, screenshot, evaluate). Opt-in per team because it
		// widens the agent's reach beyond the worktree.
		if s.chromeEnabled {
			claudeArgs = append(claudeArgs, "--chrome")
		}
		// R4.3 · --channels subscribes to MCP channel notifications.
		// Each entry becomes its own --channels arg so the CLI sees
		// a flat list (unlike the SDK path, which has to comma-join
		// because WithExtraArgs is single-valued).
		for _, ch := range s.channels {
			if ch != "" {
				claudeArgs = append(claudeArgs, "--channels", ch)
			}
		}
		cmd = exec.CommandContext(sessionCtx, claudePath, claudeArgs...)
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

	// safego: per-session loops live for the entire session lifetime.
	// A panic in either readLoop (PTY read on a stale fd, syscall
	// edge case) or idleDetector (timer race during Stop) would
	// crash the whole jarvis daemon without the wrapper.
	safego.Go("pty.readLoop", func() { s.readLoop(sessionCtx) })
	safego.Go("pty.idleDetector", func() { s.idleDetector(sessionCtx) })

	// Cleanup goroutine: signal shutdown via s.done after both tracked
	// goroutines finish. We deliberately do NOT close(s.events): the
	// debounce AfterFunc and the auto-respond goroutines are UNTRACKED
	// senders (not in s.wg), so wg.Wait() does not guarantee they have
	// stopped sending. Closing a channel with untracked concurrent
	// senders is a data race (flagged by -race even though emitEvent
	// recover()s the panic). Consumers must terminate on s.done instead
	// of ranging to completion -- see forwardSessionEvents and Events().
	safego.Go("pty.session.cleanup", func() {
		s.wg.Wait()
		close(s.done)
	})

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
	cmd := s.cmd
	s.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}

	// Close the PTY fd and nil the pointer while holding the write lock.
	// Closing the fd triggers internal/poll.(*FD).destroy(), which races
	// any concurrent reader of the fd -- specifically Resize's pty.Setsize
	// (which reads ptyFile.Fd()). Resize now takes s.mu.RLock across its
	// whole Setsize call, so doing the Close under s.mu.Lock here makes the
	// two mutually exclusive: Resize either runs entirely before this close
	// (fd still valid) or sees ptyFile == nil afterwards and bails. Close()
	// on a PTY master is a quick syscall that unblocks in-flight reads via
	// the runtime poller -- it does not wait on the reader goroutine, so
	// holding the lock across it cannot deadlock. Ordering (cancel first,
	// then close) is preserved from the pre-fix code.
	s.mu.Lock()
	if s.ptyFile != nil {
		_ = s.ptyFile.Close()
		s.ptyFile = nil
	}
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	// Wait for both goroutines to finish (done is closed by cleanup goroutine)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}

	// Clear output observers as a backstop for callers that forgot to
	// call the unsubscribe function returned from AddOutputObserver.
	s.observerMu.Lock()
	s.outputObservers = nil
	s.observerMu.Unlock()

	return nil
}

// Events returns the read-only event channel.
//
// NOTE: this channel is never closed. The session has untracked event
// senders (debounce AfterFunc, auto-respond goroutines) that cannot be
// waited on safely, so closing the channel would race those sends.
// Consumers MUST terminate via Done() rather than ranging to a close --
// see forwardSessionEvents.
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
//
// LOCKING: observerMu is held only while snapshotting the observer
// slice — NOT while running callbacks. This is deliberate. Observers
// include the Telegram bridge, regex matchers, and SSE forwarders;
// they can each take tens of milliseconds and may themselves acquire
// other locks (manager.mu, telegram client mu). Holding observerMu
// across callbacks would invert the lock order with several callers,
// risking deadlock under load.
//
// Contract for observer authors:
//  1. Callbacks MUST be safe to invoke concurrently with one another.
//     The snapshot below is a one-time copy; subsequent callbacks
//     happen sequentially in this goroutine, but other goroutines may
//     also be calling notifyOutputObservers at the same time.
//  2. Callbacks MUST NOT acquire s.mu or s.observerMu themselves —
//     they will deadlock if the call path back into Session re-enters
//     a method that takes either.
//  3. Callbacks SHOULD be non-blocking. If a callback needs to do
//     heavy work, it should hand the data off to its own goroutine
//     with bounded buffering.
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
//
// The read lock is held across the entire Setsize ioctl (not just the
// ptyFile read) so it cannot overlap teardown. Stop() closes the fd and
// nils ptyFile under s.mu.Lock(); without holding RLock here, Setsize
// could read a fd that internal/poll.(*FD).destroy() is concurrently
// tearing down (a data race). With the RLock held, Resize either wins
// (Setsize completes against a live fd before any close) or loses (sees
// isStopped / ptyFile == nil after the close and returns an error).
// Setsize is a quick ioctl, so holding the lock across it cannot deadlock.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.isStopped {
		return fmt.Errorf("session stopped")
	}
	if s.ptyFile == nil {
		return fmt.Errorf("no PTY file")
	}
	return pty.Setsize(s.ptyFile, &pty.Winsize{Cols: cols, Rows: rows})
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

// readResult carries one PTY read (data) or the terminating error from the
// long-lived reader goroutine to readLoop's main select.
type readResult struct {
	data []byte
	err  error
}

// ptyReader is the body of readLoop's single long-lived reader goroutine,
// extracted to a method so the teardown races (the non-blocking error send's
// drop arm and the ctx.Done abort on a successful send) are unit-testable with a
// caller-controlled file + channel. It loops reading f and forwarding each chunk
// to readCh, exiting on read error or ctx cancellation.
func (s *Session) ptyReader(ctx context.Context, f *os.File, readCh chan readResult) {
	for {
		buf := make([]byte, readBufSize)
		n, err := f.Read(buf)
		if err != nil {
			// Use non-blocking send so we don't block forever if no one is reading.
			select {
			case readCh <- readResult{nil, err}:
			default:
			}
			return
		}
		select {
		case readCh <- readResult{buf[:n], nil}:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Session) readLoop(ctx context.Context) {
	defer s.wg.Done()

	// PTY file descriptors don't support SetReadDeadline. Use a SINGLE long-lived
	// reader goroutine to avoid spawning N goroutines (one per iteration) which
	// could leak if Stop() races with a blocking read.
	readCh := make(chan readResult, 1)

	s.mu.RLock()
	ptyFile := s.ptyFile
	s.mu.RUnlock()
	if ptyFile == nil {
		s.reapProcess()
		return
	}

	// Single reader goroutine: exits when the PTY file is closed (f.Read returns error).
	go s.ptyReader(ctx, ptyFile, readCh)

	for {
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
	s.mu.RLock()
	cmd := s.cmd
	s.mu.RUnlock()
	if cmd != nil {
		_ = cmd.Wait()
	}
}

func (s *Session) processOutput(rawData string) {
	data := FilterFocusSequences(rawData)
	data = FilterCtrlL(data)
	if data == "" {
		return
	}

	// Feed the SafeEmulator first so Snapshot() reflects the latest
	// state by the time downstream observers see this chunk. The
	// SafeEmulator handles its own locking (no need for s.mu).
	if s.vtEmulator != nil {
		// Write returns (n, err) but the emulator never errors on
		// arbitrary input -- it absorbs partial / malformed escape
		// sequences gracefully. Ignore both safely.
		_, _ = s.vtEmulator.Write([]byte(data))
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
					// R26 · A closed ptyFile panics on Write; catch
					// it so the timer-delayed auto-submit does not
					// take the session goroutine down.
					defer func() {
						if rec := recover(); rec != nil {
							slog.Warn("auto-submit goroutine recovered from panic",
								"session", s.ID, "panic", rec)
						}
					}()
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
	// Don't arm a new debounce timer once stopped: Stop() has already cancelled
	// the timer and closed (or is about to close) the events channel, so a timer
	// armed here would fire post-shutdown. (emitEvent is also guarded, but not
	// scheduling avoids the wasted timer entirely.)
	if s.isStopped {
		s.mu.Unlock()
		return
	}
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
//
// Two-layer detection:
//  1. Per-chunk substring scan on cleanData -- fast, latency-optimal
//     for the common case where the entire menu lands in one PTY read.
//  2. Snapshot fallback when the per-chunk scan misses a marker but
//     one is present -- handles ANSI splits across chunk boundaries
//     that would have left the per-chunk view incomplete.
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

	// Helper: does a haystack carry the full menu signature? Reused
	// for the per-chunk and snapshot views so we never drift between
	// them.
	hasMenuSignature := func(text string) bool {
		hasSelector := strings.Contains(text, "❯") || strings.Contains(text, ">")
		hasNumbered := strings.Contains(text, "1.") && strings.Contains(text, "2.")
		hasTypeSomething := strings.Contains(text, "Type something")
		hasPickTask := strings.Contains(text, "Pick task") || strings.Contains(text, "Which task") || strings.Contains(text, "Which option")
		hasChatAbout := strings.Contains(text, "Chat about this")
		return hasSelector && hasNumbered && (hasTypeSomething || hasPickTask || hasChatAbout)
	}

	// Per-chunk first (fast path).
	matched := hasMenuSignature(cleanData)
	// Snapshot fallback only if per-chunk missed AND something
	// menu-shaped appeared in the chunk (cheap pre-filter so we don't
	// pay for Snapshot() on every output read).
	if !matched && (strings.ContainsAny(cleanData, "❯>") || strings.Contains(cleanData, "1.")) {
		matched = hasMenuSignature(s.Snapshot())
	}

	if matched {
		s.mu.Lock()
		s.lastAutoResponse = time.Now()
		ptyFile := s.ptyFile
		sessionCtx := s.sessionCtx
		s.mu.Unlock()

		if ptyFile != nil && sessionCtx != nil {
			// Send "1" + Enter to select the first option after a brief
			// delay for Ink to settle. Honour session shutdown via
			// sessionCtx so a Stop()-then-timer-fires race doesn't
			// write to a closed ptyFile. Re-check s.ptyFile under the
			// lock before each write — a respawn could have swapped
			// the file pointer in the meantime.
			go func() {
				// Belt-and-braces: panic recovery stays in case a
				// future code path slips past the lock check below.
				defer func() {
					if rec := recover(); rec != nil {
						slog.Warn("auto-menu goroutine recovered from panic",
							"session", s.ID, "panic", rec)
					}
				}()
				if !sleepCtx(sessionCtx, 500*time.Millisecond) {
					return
				}
				if !s.writeIfPtyFileMatches(ptyFile, []byte("1")) {
					return
				}
				if !sleepCtx(sessionCtx, 200*time.Millisecond) {
					return
				}
				if !s.writeIfPtyFileMatches(ptyFile, []byte("\r")) {
					return
				}
				slog.Info("Auto-selected menu option 1", "session", s.ID)
			}()
		}
	}
}

// sleepCtx blocks for d, or returns false early if ctx is cancelled.
// True means "the sleep completed normally; proceed."
// stopAndDrainTimer stops timer and, if it had already fired (Stop reports
// false), drains the pending tick from timer.C so a subsequent Reset starts
// clean. Extracted from idleDetector so the "already fired, drain the channel"
// arm is deterministically unit-testable -- that arm only triggers when Stop
// races a fire, which a live detector hits unreliably.
func stopAndDrainTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// writeIfPtyFileMatches writes p to expected only if s.ptyFile still
// points at expected at the moment of the write. Guards against the
// "Stop() closed the file and a respawn opened a new one" race. The
// brief lock-hold around Write is acceptable: Write to a PTY is
// non-blocking on the file descriptor (the kernel buffers).
func (s *Session) writeIfPtyFileMatches(expected *os.File, p []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptyFile != expected {
		return false
	}
	_, err := s.ptyFile.Write(p)
	if err != nil {
		slog.Warn("auto-menu write failed",
			"session", s.ID, "err", err, "bytes", len(p))
		return false
	}
	return true
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
			stopAndDrainTimer(timer)
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
	// Parity with SDKSession/CodexSession emitEvent: the events channel is closed
	// by the cleanup goroutine after wg.Wait(), but untracked emitters (the
	// auto-respond goroutines and the debounce AfterFunc) can race that close.
	// Short-circuit when stopped and recover the send so a late emit after the
	// channel closes is swallowed instead of crashing the daemon with a fatal
	// send-on-closed-channel.
	s.mu.RLock()
	stopped := s.isStopped
	s.mu.RUnlock()
	if stopped {
		return
	}
	defer func() { _ = recover() }()
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

// normaliseEffortString validates an operator-supplied effort tier and
// returns the value to pass to `--effort`. Mirrors the SDK helper in
// sdk_session.go but returns a string for CLI use. "xhigh" is accepted
// for symmetry with the docs even though the SDK collapses it to "max".
func normaliseEffortString(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "low":
		return "low"
	case "medium", "med":
		return "medium"
	case "high":
		return "high"
	case "xhigh":
		return "xhigh"
	case "max", "maximum":
		return "max"
	default:
		return ""
	}
}
