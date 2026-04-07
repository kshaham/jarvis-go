package pty

// SDK-based session implementation using github.com/shaharia-lab/claude-agent-sdk-go.
// Replaces PTY screen-scraping with structured JSON streaming for agent sessions.
// Used when Mode == ModeClaudeSDK.

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
// It implements the same observable interface as the PTY Session but uses
// structured JSON streaming instead of terminal scraping.
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
	sessionID      string // Claude session ID for multi-turn

	// Token tracking
	totalInputTokens  int64
	totalOutputTokens int64
	totalCostUSD      float64

	// Buffers (for API compatibility with PTY session)
	terminalBuffer *BufferAccumulator
	textOutput     *BufferAccumulator

	// Event channels
	events     chan SessionEvent
	done       chan struct{}
	cancelFunc context.CancelFunc

	// Provider env
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
		activeTools:    make(map[string]bool),
	}
}

// Start begins the SDK session. Unlike PTY sessions, SDK sessions don't spawn
// a long-lived process. They execute queries on demand.
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

	// If we have a system prompt (kickstart), run the initial query
	if s.systemPrompt != "" {
		go s.runQuery(sessionCtx, s.systemPrompt)
	}

	return nil
}

// runQuery executes a single Claude query using the SDK and streams events.
func (s *SDKSession) runQuery(ctx context.Context, prompt string) {
	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.lastActivityAt = time.Now()
	s.mu.Unlock()

	s.emitEvent(EventWorking{})

	// Build SDK options
	opts := []claude.Option{
		claude.WithCWD(s.WorkingDir),
		claude.WithBypassPermissions(),
		claude.WithIncludePartialMessages(),
		claude.WithSettingSources(claude.SettingSourceUser),
	}

	// Add provider env vars (Z.AI models, API keys, etc.)
	if len(s.providerEnv) > 0 {
		opts = append(opts, claude.WithEnv(s.providerEnv))
	}

	// Add MCP servers
	if len(s.mcpServers) > 0 {
		opts = append(opts, claude.WithMcpServers(s.mcpServers))
	}

	// Resume session for multi-turn conversations
	s.mu.RLock()
	resumeID := s.sessionID
	s.mu.RUnlock()
	if resumeID != "" {
		opts = append(opts, claude.WithSessionIDToResume(resumeID))
	}

	// Add system prompt for initial queries
	if s.systemPrompt != "" && resumeID == "" {
		opts = append(opts, claude.WithAppendSystemPrompt(s.systemPrompt))
	}

	slog.Info("SDK session query started", "session", s.ID, "prompt_len", len(prompt))

	stream, err := claude.Query(ctx, prompt, opts...)
	if err != nil {
		slog.Error("SDK session query failed", "session", s.ID, "error", err)
		s.appendOutput(fmt.Sprintf("[SDK Error] %v\n", err))
		s.mu.Lock()
		s.isWorking = false
		s.status = StatusIdle
		s.mu.Unlock()
		s.emitEvent(EventIdle{})
		return
	}

	// Process streaming events
	for event := range stream.Events() {
		s.mu.Lock()
		s.lastActivityAt = time.Now()
		s.mu.Unlock()

		switch event.Type {
		case claude.TypeSystem:
			if event.System != nil {
				switch event.System.Subtype {
				case claude.SubtypeInit:
					s.mu.Lock()
					s.sessionID = event.System.SessionID
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
					// Track thinking as working state
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
				// Log assistant text output
				if text := event.Assistant.Text(); text != "" {
					s.appendOutput(text)
				}
			}

		case claude.TypeRateLimitEvent:
			slog.Warn("SDK session rate limited", "session", s.ID)
			s.appendOutput("[Rate Limited] Waiting for API quota...\n")

		case claude.TypeResult:
			if event.Result != nil {
				s.mu.Lock()
				s.totalInputTokens += int64(event.Result.Usage.InputTokens)
				s.totalOutputTokens += int64(event.Result.Usage.OutputTokens)
				s.totalCostUSD += event.Result.TotalCostUSD
				s.sessionID = event.Result.SessionID
				s.isWorking = false
				s.status = StatusIdle
				s.activeTools = make(map[string]bool)
				s.mu.Unlock()

				s.appendOutput(fmt.Sprintf("\n[Complete] Turns: %d | Cost: $%.4f | Tokens: in=%d out=%d\n",
					event.Result.NumTurns, event.Result.TotalCostUSD,
					event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens))

				s.emitEvent(EventTokens{
					Input:  int64(event.Result.Usage.InputTokens),
					Output: int64(event.Result.Usage.OutputTokens),
				})
				s.emitEvent(EventIdle{})

				if event.Result.IsError {
					slog.Error("SDK session result error", "session", s.ID, "subtype", event.Result.Subtype)
				} else {
					slog.Info("SDK session complete", "session", s.ID,
						"turns", event.Result.NumTurns,
						"cost", event.Result.TotalCostUSD,
						"tokens_in", event.Result.Usage.InputTokens,
						"tokens_out", event.Result.Usage.OutputTokens)
				}
			}
		}
	}
}

// SendPrompt sends a follow-up prompt to this SDK session (multi-turn).
// This is the SDK equivalent of WriteInkPrompt -- but reliable.
func (s *SDKSession) SendPrompt(ctx context.Context, prompt string) {
	go s.runQuery(ctx, prompt)
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
func (s *SDKSession) emitEvent(event SessionEvent) {
	select {
	case s.events <- event:
	default:
		// Channel full, drop event
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

// --- Interface methods matching PTY Session ---

func (s *SDKSession) Events() <-chan SessionEvent { return s.events }

func (s *SDKSession) Stop() {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return
	}
	s.isStopped = true
	s.status = StatusStopped
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.mu.Unlock()

	close(s.done)
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
	return s.sessionID
}

func (s *SDKSession) GetTotalCostUSD() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalCostUSD
}

// WriteToPTY is a no-op for SDK sessions. Use SendPrompt instead.
func (s *SDKSession) WriteToPTY(input string) bool {
	// For backward compatibility, treat as a prompt submission
	if strings.TrimSpace(input) != "" && input != "\r" {
		s.SendPrompt(context.Background(), strings.TrimRight(input, "\r\n"))
		return true
	}
	return true
}

// WriteInkPrompt sends a prompt via the SDK. No chunking, no CR hacks needed.
func (s *SDKSession) WriteInkPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	s.SendPrompt(context.Background(), prompt)
	return true
}

// WriteViaScreen sends a prompt via the SDK (Screen wrapper not used).
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
