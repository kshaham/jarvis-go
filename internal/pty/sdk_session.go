package pty

// SDK-based session implementation using github.com/shaharia-lab/claude-agent-sdk-go.
// Uses claude.NewSession() for persistent multi-turn sessions where the subprocess
// stays alive across turns. Standup/brainstorm/replenishment prompts are delivered
// immediately via Session.Send() without waiting for the current turn to complete.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	status         SessionStatus
	isWorking      bool
	lastActivityAt time.Time
	isStopped      bool
	claudeSessionID string

	// Token tracking
	totalInputTokens  int64
	totalOutputTokens int64
	totalCostUSD      float64

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

	// Provider env and config
	providerEnv  map[string]string
	systemPrompt string
	mcpServers   map[string]any

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
		ID:             id,
		WorkingDir:     config.WorkingDir,
		CreatedAt:      time.Now(),
		Mode:           ModeClaudeSDK,
		name:           config.Name,
		status:         StatusStopped,
		terminalBuffer: NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo),
		textOutput:     NewBufferAccumulator(MaxTextOutputSize, TrimTextTo),
		events:         make(chan SessionEvent, eventChannelSize),
		done:           make(chan struct{}),
		providerEnv:    config.ProviderEnv,
		systemPrompt:   config.SystemPrompt,
		mcpServers:     config.McpServers,
		outputObservers: make(map[int]func(string)),
		activeTools:     make(map[string]bool),
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
	// Empty model string lets ANTHROPIC_DEFAULT_*_MODEL env vars take effect.
	opts := []claude.Option{
		claude.WithModel(""),
		claude.WithCWD(s.WorkingDir),
		claude.WithBypassPermissions(),
		claude.WithIncludePartialMessages(),
		claude.WithSettingSources(claude.SettingSourceUser),
	}

	if len(s.providerEnv) > 0 {
		opts = append(opts, claude.WithEnv(s.providerEnv))
	}
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
				s.mu.Unlock()

				s.appendOutput(fmt.Sprintf("\n[Turn Complete] Turns: %d | Cost: $%.4f | Tokens: in=%d out=%d\n",
					event.Result.NumTurns, event.Result.TotalCostUSD,
					event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens))

				s.emitEvent(EventTokens{
					Input:  int64(event.Result.Usage.InputTokens),
					Output: int64(event.Result.Usage.OutputTokens),
					Total:  int64(event.Result.Usage.InputTokens) + int64(event.Result.Usage.OutputTokens),
				})
				s.emitEvent(EventIdle{})

				if event.Result.IsError {
					slog.Error("SDK session turn error", "session", s.ID, "subtype", event.Result.Subtype)
				} else {
					slog.Info("SDK session turn complete", "session", s.ID,
						"turns", event.Result.NumTurns,
						"cost", event.Result.TotalCostUSD)
				}
			}
		}
	}
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
	}
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

func (s *SDKSession) GetTextOutput() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.textOutput.Value()
}

func (s *SDKSession) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.claudeSessionID
}

func (s *SDKSession) GetTotalCostUSD() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalCostUSD
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
func (s *SDKSession) WriteViaScreen(input string) bool {
	input = strings.TrimRight(input, "\r\n")
	if input == "" {
		return false
	}
	s.SendPrompt(context.Background(), input)
	return true
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
