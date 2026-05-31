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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shaharia-lab/claude-agent-sdk-go/claude"

	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/safego"
)

// sdkStopWaitTimeout bounds how long Stop() waits for eventConsumer to exit
// before logging a warning and returning anyway. Declared as a package var
// (not a const) so tests can shrink it to force the teardown-timeout branch
// without a 5-second wall-clock wait. Production never reassigns it.
var sdkStopWaitTimeout = 5 * time.Second

// sdkSessionSend is the seam used to deliver the kickstart turn to the
// persistent SDK session. Production points at (*claude.Session).Send; tests
// override it to force the kickstart-send error branch in Start() without
// having to provoke a flaky broken-pipe on the subprocess's stdin (small
// writes are buffered by the kernel even after the child exits, so a real
// EPIPE is not reliably reproducible). Behavior-preserving: the default is a
// direct method call.
var sdkSessionSend = func(session *claude.Session, msg string) error {
	return session.Send(msg)
}

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

	// R1.3 additions: per-session tuning forwarded to the SDK on
	// Start(). Empty / zero values mean "let the SDK default apply".
	systemPromptFile                   string
	appendSystemPromptFile             string
	effort                             string
	fallbackModel                      string
	excludeDynamicSystemPromptSections bool
	strictMcpConfig                    bool

	// R3.5 additions: fork-from-resume support for snapshot restore.
	// resumeSessionID asks the SDK for --resume <id>; forkSession adds
	// --fork-session so the resumed transcript branches into a new id.
	resumeSessionID string
	forkSession     bool

	// R3.6: optional GitHub PR number forwarded via --from-pr <n>.
	// Zero leaves the flag off (default).
	fromPRNumber int

	// R4.1: opt-in browser-automation tooling forwarded via --chrome.
	chromeEnabled bool

	// R4.3: opt-in channel subscriptions forwarded via --channels.
	channels []string

	// claudeBinaryPath overrides the `claude` executable the SDK spawns.
	// Empty resolves via PATH (production default). Tests point this at a
	// stub binary (cmd/fakeclaude or a stream-json emitter) so Start() and
	// the event consumer can be exercised without a real Anthropic session.
	claudeBinaryPath string

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

		systemPromptFile:                   config.SystemPromptFile,
		appendSystemPromptFile:             config.AppendSystemPromptFile,
		effort:                             config.Effort,
		fallbackModel:                      config.FallbackModel,
		excludeDynamicSystemPromptSections: config.ExcludeDynamicSystemPromptSections,
		strictMcpConfig:                    config.StrictMcpConfig,
		resumeSessionID:                    config.ResumeSessionID,
		forkSession:                        config.ForkSession,
		fromPRNumber:                       config.FromPRNumber,
		chromeEnabled:                      config.ChromeEnabled,
		channels:                           sanitizeChannels(config.Channels, "sdk:"+id),
		claudeBinaryPath:                   config.ClaudeBinaryPath,
		outputObservers:                    make(map[int]func(string)),
		activeTools:                        make(map[string]bool),
		toolTracker:                        newToolEventTracker(),
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
	// Override the `claude` executable when configured. Empty leaves the
	// SDK's PATH lookup intact (production default); tests point this at a
	// stub stream-json emitter so Start()/eventConsumer run without a real
	// Anthropic session.
	if s.claudeBinaryPath != "" {
		opts = append(opts, claude.WithClaudeExecutable(s.claudeBinaryPath))
	}
	// R31 · Stable cross-agent prefix goes into the system prompt so
	// Anthropic's ephemeral cache can discount it on subsequent calls.
	// Only attach when non-empty so SDK default behavior is preserved
	// for callers that don't opt in.
	if s.appendSystemPrompt != "" {
		opts = append(opts, claude.WithAppendSystemPrompt(s.appendSystemPrompt))
	}

	// R1.3 · File-backed system prompts. When a file is set we still
	// pass the inline variant too (the SDK enforces precedence
	// internally and the file path wins). This double-attach is safe
	// and keeps the in-process kickstart path functional during the
	// rollout.
	// IMPORTANT: WithExtraArgs serialises each key VERBATIM (no `--` prepend —
	// see claude-agent-sdk-go options.go), so every key MUST already include
	// its leading `--`. Bare keys (e.g. "exclude-dynamic-system-prompt-sections")
	// arrive as positional args; since claude's `--mcp-config` is variadic
	// (>= ~2.1.156) it then swallows the bare token as a second MCP config path,
	// producing "Invalid MCP configuration: MCP config file not found: <token>"
	// and killing the session at startup. The `--` prefix makes them flags so
	// --mcp-config stops consuming at the next one.
	extraArgs := map[string]string{}
	if s.systemPromptFile != "" {
		extraArgs["--system-prompt-file"] = s.systemPromptFile
	}
	if s.appendSystemPromptFile != "" {
		extraArgs["--append-system-prompt-file"] = s.appendSystemPromptFile
	}
	if s.excludeDynamicSystemPromptSections {
		// Boolean flag (empty value) -> bare `--exclude-dynamic-system-prompt-sections`.
		extraArgs["--exclude-dynamic-system-prompt-sections"] = ""
	}
	// R3.6 · --from-pr loads PR context into the system prompt. Pushed
	// through WithExtraArgs because the SDK doesn't expose a typed
	// option yet.
	if s.fromPRNumber > 0 {
		extraArgs["--from-pr"] = strconv.Itoa(s.fromPRNumber)
	}
	// R4.1 · --chrome turns on Claude's built-in browser-automation
	// tools. Pushed through WithExtraArgs (bare flag, no value).
	if s.chromeEnabled {
		extraArgs["--chrome"] = ""
	}
	// R4.3 · --channels subscribes to MCP channel notifications.
	//
	// P7.2.8 fix: the CLI expects a repeated `--channels <spec>` flag,
	// one per subscription. The SDK's WithExtraArgs is a single-valued
	// map (`map[string]string`) so we can only forward ONE channel via
	// the SDK path. Previously we comma-joined — that's the wrong
	// grammar; the CLI would have silently passed the comma-joined
	// value as a single spec, never matching a real channel.
	//
	// Pragmatic resolution: forward the first channel; log a clear
	// WARN if there are more so operators know to switch
	// officeSessionMode=hooks (PTY) where the repeated-flag pattern
	// works. Single-channel SDK teams continue to function unchanged.
	if len(s.channels) > 0 {
		extraArgs["--channels"] = s.channels[0]
		if len(s.channels) > 1 {
			slog.Warn("SDK session can only subscribe to 1 channel via WithExtraArgs; forwarding first only; set officeSessionMode=hooks for multi-channel",
				"session", s.ID,
				"forwarded", s.channels[0],
				"dropped", s.channels[1:])
		}
	}
	if len(extraArgs) > 0 {
		opts = append(opts, claude.WithExtraArgs(extraArgs))
	}

	// R1.3 · Per-session reasoning effort. Only attach when the
	// caller picks a tier; empty defers to the SDK + env defaults.
	if effortLevel := normaliseEffort(s.effort); effortLevel != "" {
		opts = append(opts, claude.WithEffort(effortLevel))
	}

	// R1.3 · Fallback model — the SDK retries an overloaded primary
	// against this model before failing the turn. Empty disables.
	// Dollar-cap budget is intentionally NOT wired: jarvis runs
	// against subscription-billed providers (Claude subscription,
	// Z.AI Coding Plan) where per-turn USD limits are meaningless.
	if s.fallbackModel != "" {
		opts = append(opts, claude.WithFallbackModel(s.fallbackModel))
	}

	// R1.3 · Strict MCP config: ignores user/project MCP registrations
	// so the office MCP surface is exactly what jarvis passed in.
	if s.strictMcpConfig {
		opts = append(opts, claude.WithStrictMcpConfig())
	}

	// R3.5 · Resume + fork. Snapshot-restore captures each member's
	// final ClaudeSessionID; when the operator picks "restore as fork"
	// we replay that ID through --resume and add --fork-session so
	// Claude branches the transcript into a new session instead of
	// hijacking the original (which may still be running elsewhere).
	if s.resumeSessionID != "" {
		opts = append(opts, claude.WithSessionIDToResume(s.resumeSessionID))
		if s.forkSession {
			opts = append(opts, claude.WithForkSession())
		}
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

	// Capture subprocess stderr for diagnostics (API errors, auth issues).
	// Pipe through logscrub.All before logging — Claude's stderr on 401
	// includes `Authorization: Bearer ...` headers verbatim that would
	// otherwise land in slog (and any downstream log shipper) in
	// cleartext. Scrubbing here is the single chokepoint.
	//
	// SCOPE LIMIT (P12.3): only the parent Claude subprocess's stderr
	// passes through this callback. MCP child processes (context7,
	// sequential-thinking, codegraph, codegraphcontext, jarvis-mcp)
	// emit their own stderr directly to the host and bypass this
	// scrubber. claude.WithStderr in the SDK does not expose a per-MCP
	// stderr hook (verified against claude-agent-sdk-go v0.3.1).
	// Operators concerned about MCP-server credential leakage should
	// tail those child processes directly. See docs/ECC.md "Operating
	// notes → MCP server stderr is not captured" for the rationale.
	opts = append(opts, claude.WithStderr(func(line string) {
		scrubbed := logscrub.All(line)
		slog.Debug("SDK subprocess stderr", "session", s.ID, "line", scrubbed)
		if strings.Contains(scrubbed, "error") || strings.Contains(scrubbed, "Error") || strings.Contains(scrubbed, "unauthorized") || strings.Contains(scrubbed, "rate") {
			slog.Warn("SDK subprocess error", "session", s.ID, "stderr", scrubbed)
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
	// safego wraps so a panic in event parsing (malformed SDK
	// frame, JSON decode race) doesn't crash the whole daemon.
	consumerReady := make(chan struct{})
	s.wg.Add(1)
	safego.Go("pty.sdk.eventConsumer", func() { s.eventConsumer(sessionCtx, consumerReady) })
	<-consumerReady

	// Send kickstart as the first turn.
	if s.systemPrompt != "" {
		s.sendMu.Lock()
		err := sdkSessionSend(session, s.systemPrompt)
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
		s.processEvent(event)
	}
}

// processEvent handles a single SDK event from the persistent session's
// stream. Extracted from eventConsumer's range body so the per-event
// switch (token accounting, status transitions, event emission, tool
// pairing) is testable with synthetic claude.Event values without a
// live subprocess. Behavior-preserving: eventConsumer calls this once
// per ranged event.
func (s *SDKSession) processEvent(event claude.Event) {
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
			clear(s.activeTools)
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
				// Emit EventExit so the death flows through the forwarder's
				// normal EventExit path (HandleAgentDeath + cleanup). The
				// subsequent go s.Stop() sets isStopped, so the worker's defer
				// won't emit a second EventExit. Without this the events
				// channel would close with no EventExit and the office agent
				// would leak (zombie counting against maxSessions).
				s.emitEvent(EventExit{ExitCode: 0})
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
// Tokens returns input, output, and total token counts accumulated across
// this SDK session's turns. Mirrors Session.Tokens() so the token-stats
// handler can aggregate SDK sessions alongside PTY and Codex sessions.
func (s *SDKSession) Tokens() (input, output, total int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalInputTokens, s.totalOutputTokens, s.totalInputTokens + s.totalOutputTokens
}

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
// Clones into a []func(string) slice (same pattern as session.go) so
// the hot path allocates a slice header rather than a full map.
func (s *SDKSession) notifyOutputObservers(data string) {
	s.observerMu.Lock()
	observers := make([]func(string), 0, len(s.outputObservers))
	for _, fn := range s.outputObservers {
		observers = append(observers, fn)
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
	case <-time.After(sdkStopWaitTimeout):
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

// ClaudeSessionID returns the Claude-side session identifier reported
// by the SDK (populated from system_init / result events). Empty until
// the first SDK event arrives. R3.5 callers use this to persist a
// resume-point inside team snapshots.
func (s *SDKSession) ClaudeSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.claudeSessionID
}

// SetClaudeSessionIDForTest seeds the underlying claudeSessionID so
// tests can exercise code paths (snapshot fork-restore, attach
// command lookup) without driving a real SDK handshake. Production
// callers must NOT use this — the ID is normally written by the
// event consumer when system_init / result events arrive. Lives in
// the production file so the test helper is visible to consumers
// in other packages.
func (s *SDKSession) SetClaudeSessionIDForTest(id string) {
	s.mu.Lock()
	s.claudeSessionID = id
	s.mu.Unlock()
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
// normaliseEffort maps the operator-facing string (low|medium|high|max,
// case-insensitive) to the SDK's EffortLevel typed alias. Returns ""
// for unrecognised values so the caller skips the option entirely
// (letting SDK defaults apply). The "xhigh" CLI value documented for
// some models maps to EffortMax in the SDK.
func normaliseEffort(in string) claude.EffortLevel {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "low":
		return claude.EffortLow
	case "medium", "med":
		return claude.EffortMedium
	case "high":
		return claude.EffortHigh
	case "xhigh", "max", "maximum":
		return claude.EffortMax
	default:
		return ""
	}
}

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
