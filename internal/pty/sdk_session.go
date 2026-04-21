package pty

// SDK-based session implementation using github.com/shaharia-lab/claude-agent-sdk-go.
// Uses claude.NewSession() for persistent multi-turn sessions where the subprocess
// stays alive across turns. Standup/brainstorm/replenishment prompts are delivered
// immediately via Session.Send() without waiting for the current turn to complete.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shaharia-lab/claude-agent-sdk-go/claude"
)

// SDKSession wraps the Claude Agent SDK for programmatic agent interaction.
// Uses a persistent claude.Session for multi-turn conversations.
type SDKSession struct {
	mu sync.RWMutex

	// Identity
	ID         string
	WorkingDir string
	CreatedAt  time.Time
	Mode       SessionMode
	name       string

	// State
	status          SessionStatus
	isWorking       bool
	lastActivityAt  time.Time
	isStopped       bool
	claudeSessionID string

	// Token tracking
	totalInputTokens   int64
	totalOutputTokens  int64
	totalCostUSD       float64
	maxTurnsPerSession int  // per-session turn-count cap; 0 = unlimited
	turnCapFired       bool // so EventTurnCapReached only fires once

	// Buffers
	terminalBuffer *BufferAccumulator
	textOutput     *BufferAccumulator

	// Persistent SDK session (lives for the session lifetime)
	persistentSession *claude.Session
	sendMu            sync.Mutex // serializes all Send() calls

	// Event channels
	events     chan SessionEvent
	done       chan struct{}
	cancelFunc context.CancelFunc
	eventsOnce sync.Once
	doneOnce   sync.Once
	wg         sync.WaitGroup

	// R19 · Event-drop observability. Counts how many times emitEvent
	// hit the non-blocking default branch because the 256-slot events
	// buffer was full. Exposed on the member payload so the dashboard
	// can show a pill + tooltip; escalates from debug-log to a WARN at
	// 1st drop and every 100th thereafter so operators see repeated
	// backlog without log spam.
	droppedEvents atomic.Uint64

	// R21 · Tool invocation tracker — pairs tool_use content blocks
	// (from assistant messages) with their tool_result (from user
	// messages) so we can emit a single EventToolInvocation per
	// completed pair. Drives office-level tool-loop detection.
	toolTracker *toolEventTracker

	// Provider env and config
	providerEnv  map[string]string
	systemPrompt string
	mcpServers   map[string]any
	// model is the specific Claude model ID to force via claude.WithModel.
	// Empty = let ANTHROPIC_DEFAULT_*_MODEL env vars / CLI default pick.
	model string
	// appendSystemPrompt is the stable (cacheable) boilerplate passed
	// to claude.WithAppendSystemPrompt. Anthropic's ephemeral cache
	// auto-breakpoints here; content must be byte-for-byte identical
	// across sessions to share the cache.
	appendSystemPrompt string

	// Output observers
	outputObservers   map[int]func(string)
	outputObserverSeq int
	observerMu        sync.Mutex

	// Active tool tracking
	activeTools map[string]bool
}

// NewSDKSession creates a new SDK-based session.
func NewSDKSession(config SessionConfig) *SDKSession {
	id := config.ID
	if id == "" {
		id = uuid.New().String()
	}

	return &SDKSession{
		ID:                 id,
		WorkingDir:         config.WorkingDir,
		CreatedAt:          time.Now(),
		Mode:               ModeClaudeSDK,
		name:               config.Name,
		status:             StatusStopped,
		terminalBuffer:     NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo),
		textOutput:         NewBufferAccumulator(MaxTextOutputSize, TrimTextTo),
		events:             make(chan SessionEvent, eventChannelSize),
		done:               make(chan struct{}),
		providerEnv:        config.ProviderEnv,
		systemPrompt:       config.SystemPrompt,
		mcpServers:         config.McpServers,
		model:              config.Model,
		appendSystemPrompt: config.AppendSystemPrompt,
		maxTurnsPerSession: config.MaxTurnsPerSession,
		outputObservers:    make(map[int]func(string)),
		activeTools:        make(map[string]bool),
		toolTracker:        newToolEventTracker(),
	}
}

// Start creates a persistent claude.Session and begins consuming events.
func (s *SDKSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.status != StatusStopped {
		s.mu.Unlock()
		return fmt.Errorf("session already started")
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	s.status = StatusIdle
	s.lastActivityAt = time.Now()
	s.isStopped = false
	s.mu.Unlock()

	// Build SDK options for persistent session.
	// Per-member model override wins (e.g. claude-opus-4-7 picked in
	// the spawn modal). Empty string falls back to the SDK's own
	// default, which also respects ANTHROPIC_DEFAULT_*_MODEL env vars.
	opts := []claude.Option{
		claude.WithModel(s.model),
		claude.WithCWD(s.WorkingDir),
		claude.WithBypassPermissions(),
		claude.WithIncludePartialMessages(),
		claude.WithSettingSources(claude.SettingSourceUser),
	}
	// R31 · Stable cross-agent prefix goes into the system prompt so
	// Anthropic's ephemeral cache can discount it on subsequent calls.
	// Only attach when non-empty so SDK default behavior is preserved
	// for callers that don't opt in.
	if s.appendSystemPrompt != "" {
		opts = append(opts, claude.WithAppendSystemPrompt(s.appendSystemPrompt))
	}

	if len(s.providerEnv) > 0 {
		opts = append(opts, claude.WithEnv(s.providerEnv))
		// Log env keys (not values) for auth diagnostics
		keys := make([]string, 0, len(s.providerEnv))
		for k := range s.providerEnv {
			keys = append(keys, k)
		}
		slog.Info("SDK session env vars", "session", s.ID, "keys", keys)
	} else {
		slog.Warn("SDK session has no provider env vars", "session", s.ID)
	}

	// Capture subprocess stderr for diagnostics (API errors, auth issues)
	opts = append(opts, claude.WithStderr(func(line string) {
		slog.Debug("SDK subprocess stderr", "session", s.ID, "line", line)
		if strings.Contains(line, "error") || strings.Contains(line, "Error") || strings.Contains(line, "unauthorized") || strings.Contains(line, "rate") {
			slog.Warn("SDK subprocess error", "session", s.ID, "stderr", line)
		}
	}))

	if len(s.mcpServers) > 0 {
		opts = append(opts, claude.WithMcpServers(s.mcpServers))
	}

	// Create persistent session -- subprocess stays alive across turns.
	session, err := claude.NewSession(sessionCtx, opts...)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create persistent session: %w", err)
	}
	s.persistentSession = session

	// Start event consumer goroutine FIRST, wait for it to be ready
	// before sending the kickstart to avoid dropping early events.
	consumerReady := make(chan struct{})
	s.wg.Add(1)
	go s.eventConsumer(sessionCtx, consumerReady)
	<-consumerReady

	// Send kickstart as the first turn.
	if s.systemPrompt != "" {
		s.sendMu.Lock()
		err := session.Send(s.systemPrompt)
		s.sendMu.Unlock()
		if err != nil {
			slog.Error("Failed to send kickstart", "session", s.ID, "error", err)
			cancel()
			return fmt.Errorf("failed to send kickstart: %w", err)
		}
		s.mu.Lock()
		s.isWorking = true
		s.status = StatusBusy
		s.mu.Unlock()
		s.emitEvent(EventWorking{})
		slog.Info("SDK session kickstart sent", "session", s.ID, "prompt_len", len(s.systemPrompt))
	}

	return nil
}

// eventConsumer ranges over the persistent session's Events() channel.
// Processes events from ALL turns (kickstart, standup, brainstorm, etc.).
// Only exits when the session is closed.
func (s *SDKSession) eventConsumer(ctx context.Context, ready chan<- struct{}) {
	// wg.Done must be the LAST deferred call (runs last) so Stop() only
	// unblocks after all cleanup (channel close, log) has completed.
	defer s.wg.Done()

	// Signal that we're about to start ranging (consumer is ready).
	close(ready)

	defer func() {
		// Only emit EventExit if Stop() hasn't already run (avoid double cleanup).
		s.mu.RLock()
		stopped := s.isStopped
		s.mu.RUnlock()
		if !stopped {
			s.emitEvent(EventExit{ExitCode: 0})
		}
		s.eventsOnce.Do(func() { close(s.events) })
		slog.Info("SDK session event consumer ended", "session", s.ID)
	}()

	for event := range s.persistentSession.Events() {
		s.mu.Lock()
		s.lastActivityAt = time.Now()
		s.mu.Unlock()

		switch event.Type {
		case claude.TypeSystem:
			if event.System != nil {
				switch event.System.Subtype {
				case claude.SubtypeInit:
					s.mu.Lock()
					s.claudeSessionID = event.System.SessionID
					s.mu.Unlock()
					s.appendOutput(fmt.Sprintf("[Session %s] Model: %s\n", event.System.SessionID, event.System.Model))
					slog.Info("SDK session initialized", "session", s.ID, "claudeSession", event.System.SessionID, "model", event.System.Model)
				case "error":
					s.appendOutput(fmt.Sprintf("[Error] %s\n", event.System.Message))
					slog.Error("SDK session error", "session", s.ID, "message", event.System.Message)
				}
			}

		case claude.TypeStreamEvent:
			if event.StreamEvent != nil && event.StreamEvent.Event.Delta != nil {
				delta := event.StreamEvent.Event.Delta
				switch delta.Type {
				case "text_delta":
					s.appendOutput(delta.Text)
				case "thinking_delta":
					s.mu.Lock()
					if !s.isWorking {
						s.isWorking = true
						s.status = StatusBusy
						s.mu.Unlock()
						s.emitEvent(EventWorking{})
					} else {
						s.mu.Unlock()
					}
				}
			}

		case claude.TypeAssistant:
			if event.Assistant != nil {
				if text := event.Assistant.Text(); text != "" {
					s.appendOutput(text)
				}
			}
			// R21 · Stash any tool_use content blocks from Raw for
			// pairing with their tool_result arrivals (which come
			// as user-shaped messages the SDK doesn't type).
			if len(event.Raw) > 0 {
				s.toolTracker.observeAssistant(event.Raw)
			}

		case claude.TypeRateLimitEvent:
			slog.Warn("SDK session rate limited", "session", s.ID)
			s.appendOutput("[Rate Limited] Waiting for API quota...\n")

		case claude.TypeResult:
			// Turn complete. Session stays alive for the next Send().
			if event.Result != nil {
				s.mu.Lock()
				s.totalInputTokens += int64(event.Result.Usage.InputTokens)
				s.totalOutputTokens += int64(event.Result.Usage.OutputTokens)
				s.totalCostUSD += event.Result.TotalCostUSD
				s.claudeSessionID = event.Result.SessionID
				s.isWorking = false
				s.status = StatusIdle
				s.activeTools = make(map[string]bool)
				currentCost := s.totalCostUSD
				// Best-effort model name: pick the key with the largest usage
				// out of ModelUsages. Multi-model turns are rare; in the common
				// single-model case this returns the only entry.
				var model string
				var bestTokens int64
				for m, u := range event.Result.ModelUsages {
					total := int64(u.InputTokens) + int64(u.OutputTokens)
					if total >= bestTokens {
						bestTokens = total
						model = m
					}
				}
				turnCap := s.maxTurnsPerSession
				turnCapAlreadyFired := s.turnCapFired
				s.mu.Unlock()

				s.appendOutput(fmt.Sprintf("\n[Turn Complete] Turns: %d | Cost: $%.4f | Tokens: in=%d out=%d\n",
					event.Result.NumTurns, event.Result.TotalCostUSD,
					event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens))

				s.emitEvent(EventTokens{
					Input:  int64(event.Result.Usage.InputTokens),
					Output: int64(event.Result.Usage.OutputTokens),
					Total:  int64(event.Result.Usage.InputTokens) + int64(event.Result.Usage.OutputTokens),
					Cost:   event.Result.TotalCostUSD,
					Model:  model,
				})
				s.emitEvent(EventIdle{})

				// R24 · Per-session cost cap removed. Operator uses
				// subscription plans (Z.AI Coding, Claude sub) where
				// dollar-reported cost is always $0 and irrelevant.
				// Turn cap below remains as the primary runaway
				// defense — it's quota-agnostic and fires on usage,
				// not billing.
				//
				// Per-session turn-count cap enforcement. Guards with
				// turnCapFired so we emit exactly once even if the
				// goroutine Stop() takes a moment to wind the session
				// down.
				if turnCap > 0 && !turnCapAlreadyFired && event.Result.NumTurns >= turnCap {
					s.mu.Lock()
					s.turnCapFired = true
					s.mu.Unlock()
					s.emitEvent(EventTurnCapReached{
						Turns: event.Result.NumTurns,
						Cap:   turnCap,
						Cost:  currentCost,
					})
					slog.Warn("SDK session hit turn cap; stopping",
						"session", s.ID, "turns", event.Result.NumTurns, "cap", turnCap, "cost", currentCost)
					go s.Stop()
				}

				if event.Result.IsError {
					// Include errors slice + result text so rate-limit /
					// quota / 529 / credit failures are diagnosable from
					// the log alone. Subtype can be misleading ("success"
					// despite IsError=true) on some provider errors.
					stopReason := ""
					if event.Result.StopReason != nil {
						stopReason = *event.Result.StopReason
					}
					resultText := event.Result.Result
					if len(resultText) > 500 {
						resultText = resultText[:500] + "..."
					}
					slog.Error("SDK session turn error",
						"session", s.ID,
						"subtype", event.Result.Subtype,
						"stopReason", stopReason,
						"errors", event.Result.Errors,
						"numTurns", event.Result.NumTurns,
						"durationMS", event.Result.DurationMS,
						"result", resultText)
					// Surface to the web layer so the operator sees the
					// failure in the dashboard instead of the agent
					// silently falling idle after an expensive botched run.
					s.emitEvent(EventTurnError{
						Subtype:    event.Result.Subtype,
						StopReason: stopReason,
						Result:     resultText,
						NumTurns:   event.Result.NumTurns,
					})
				} else {
					slog.Info("SDK session turn complete", "session", s.ID,
						"turns", event.Result.NumTurns,
						"cost", event.Result.TotalCostUSD)
				}
				// R21 · Tool pairings that didn't match within this
				// turn are stale — clear them so next turn starts
				// with a clean map.
				s.toolTracker.reset()
			}

		default:
			// R21 · User-shaped messages carrying tool_result content
			// blocks arrive with an unknown MessageType in v0.3.1
			// (the SDK only types assistant/result/system/stream).
			// Peek at the Raw envelope's role field — if it's "user",
			// pair tool_result blocks against pending tool_use entries
			// and emit one EventToolInvocation per matched pair.
			if len(event.Raw) > 0 && looksLikeUserMessage(event.Raw) {
				for _, inv := range s.toolTracker.observeUser(event.Raw) {
					s.emitEvent(EventToolInvocation{
						Name:       inv.Name,
						ToolUseID:  inv.ToolUseID,
						InputHash:  inv.InputHash,
						ResultHash: inv.ResultHash,
						IsError:    inv.IsError,
					})
				}
			}
		}
	}
}

// looksLikeUserMessage peeks at the raw envelope and returns true if
// the top-level "type" field is "user". Structured check (per R21
// code review) avoids the false-positive class where an assistant
// message's text content happens to contain the literal substring
// `"role":"user"` — common when agents discuss JSON schemas or
// role-based access control. Parsing just the envelope type field is
// cheap (the CLI emits a type on every line).
func looksLikeUserMessage(raw []byte) bool {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return false
	}
	return env.Type == "user"
}

// SendPrompt sends a message to the persistent session. Non-blocking.
// The agent receives it immediately, even if a previous turn is still running.
func (s *SDKSession) SendPrompt(ctx context.Context, prompt string) {
	s.mu.RLock()
	stopped := s.isStopped
	s.mu.RUnlock()
	if stopped {
		return
	}

	if s.persistentSession == nil {
		slog.Warn("SDK session not initialized, dropping prompt", "session", s.ID)
		return
	}

	s.sendMu.Lock()
	err := s.persistentSession.Send(prompt)
	s.sendMu.Unlock()
	if err != nil {
		slog.Error("Failed to send prompt to SDK session", "session", s.ID, "error", err)
		return
	}

	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.lastActivityAt = time.Now()
	s.mu.Unlock()
	s.emitEvent(EventWorking{})
	slog.Info("SDK session prompt sent", "session", s.ID, "prompt_len", len(prompt))
}

// appendOutput writes text to buffers and notifies observers.
func (s *SDKSession) appendOutput(text string) {
	s.mu.Lock()
	s.terminalBuffer.Append(text)
	s.textOutput.Append(text)
	s.mu.Unlock()

	s.emitEvent(EventTerminal{Data: text})
	s.emitEvent(EventOutput{Data: text})
	s.notifyOutputObservers(text)
}

// emitEvent sends an event to the channel without blocking.
// Uses isStopped guard + recover for bulletproof closed-channel safety.
func (s *SDKSession) emitEvent(event SessionEvent) {
	s.mu.RLock()
	stopped := s.isStopped
	s.mu.RUnlock()
	if stopped {
		return
	}
	defer func() { recover() }()
	select {
	case s.events <- event:
	default:
		total := s.droppedEvents.Add(1)
		slog.Debug("SDK session event dropped (channel full)", "session", s.ID, "event", fmt.Sprintf("%T", event), "totalDropped", total)
		// R19 · WARN at first drop so operators notice immediately, and
		// every 100th after that to surface sustained backlog without
		// log spam. The per-drop debug line stays for deep traces.
		if total == 1 || total%100 == 0 {
			slog.Warn("SDK session event backlog — events dropped",
				"session", s.ID, "totalDropped", total, "event", fmt.Sprintf("%T", event))
		}
	}
}

// DroppedEvents returns the count of events silently dropped from the
// outbound channel because the buffer was full. Exposed for the member
// payload so dashboards can badge high-drop agents.
func (s *SDKSession) DroppedEvents() uint64 {
	return s.droppedEvents.Load()
}

// notifyOutputObservers calls all registered output observers.
func (s *SDKSession) notifyOutputObservers(data string) {
	s.observerMu.Lock()
	observers := make(map[int]func(string), len(s.outputObservers))
	for k, v := range s.outputObservers {
		observers[k] = v
	}
	s.observerMu.Unlock()
	for _, fn := range observers {
		fn(data)
	}
}

// --- Interface methods ---

func (s *SDKSession) Events() <-chan SessionEvent { return s.events }

func (s *SDKSession) Stop() {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return
	}
	s.isStopped = true
	s.status = StatusStopped
	cancel := s.cancelFunc
	s.mu.Unlock()

	// Cancel context -- triggers eventConsumer to exit
	if cancel != nil {
		cancel()
	}

	// Wait for eventConsumer to finish (bounded to 5s to avoid hanging).
	waitDone := make(chan struct{})
	go func() { s.wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		slog.Warn("SDK session eventConsumer did not exit within 5s", "session", s.ID)
	}

	// Clear output observers as a backstop for callers that forgot to
	// call the unsubscribe function returned from AddOutputObserver.
	s.observerMu.Lock()
	s.outputObservers = nil
	s.observerMu.Unlock()

	s.doneOnce.Do(func() { close(s.done) })
	slog.Info("SDK session stopped", "session", s.ID)
}

func (s *SDKSession) GetStatus() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *SDKSession) IsWorking() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isWorking
}

func (s *SDKSession) GetName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *SDKSession) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *SDKSession) GetTerminalBuffer() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.terminalBuffer.Value()
}

// WriteToPTY sends a prompt via the persistent session.
func (s *SDKSession) WriteToPTY(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || input == "\r" {
		return false
	}
	s.SendPrompt(context.Background(), strings.TrimRight(input, "\r\n"))
	return true
}

// WriteInkPrompt sends a prompt via the persistent session. No chunking needed.
func (s *SDKSession) WriteInkPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	s.SendPrompt(context.Background(), prompt)
	return true
}

// WriteViaScreen sends a prompt via the persistent session.
//
// PTY sessions treat this as raw terminal input so callers pass
// control sequences ("\x03" for Ctrl+C, "\r" for Enter) to interrupt
// or submit a turn. Those bytes are meaningless on an SDK session --
// the provider would just receive a 1-byte user message like "\x03"
// and reply normally. Strip ASCII control chars (C0 block, DEL)
// before dispatch; if nothing printable remains, no-op and return
// false so the caller can see the interrupt didn't take. Preserves
// \n and \t which are legitimate inside larger prompt text.
func (s *SDKSession) WriteViaScreen(input string) bool {
	cleaned := stripControlChars(input)
	cleaned = strings.TrimRight(cleaned, "\r\n")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return false
	}
	s.SendPrompt(context.Background(), cleaned)
	return true
}

// stripControlChars removes C0 control characters (0x00-0x1F except \n,
// \t) and DEL (0x7F). Used to make WriteViaScreen safe on SDK sessions
// where terminal-control sequences don't make sense as LLM input.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// AddOutputObserver registers a callback for output data.
func (s *SDKSession) AddOutputObserver(fn func(string)) func() {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	if s.outputObservers == nil {
		s.outputObservers = make(map[int]func(string))
	}
	s.outputObserverSeq++
	id := s.outputObserverSeq
	s.outputObservers[id] = fn
	return func() {
		s.observerMu.Lock()
		delete(s.outputObservers, id)
		s.observerMu.Unlock()
	}
}

// GetActiveTools returns the set of currently active tools.
func (s *SDKSession) GetActiveTools() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]string, 0, len(s.activeTools))
	for t := range s.activeTools {
		tools = append(tools, t)
	}
	return tools
}
