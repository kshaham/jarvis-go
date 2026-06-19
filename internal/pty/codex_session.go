package pty

// CodexSession runs an agent on OpenAI's Codex CLI as an alternative backend
// to Claude Code. Unlike the Claude SDK (one persistent subprocess streaming
// all turns), `codex exec` is ONE-SHOT per turn: each prompt spawns a fresh
// `codex exec [resume <thread_id>] --json …` whose stdout is a newline-
// delimited JSON event stream. A single worker goroutine serializes turns off
// a prompt queue; the first turn captures the thread_id, subsequent turns
// resume it. Events map onto the same pty.Event* types the SDK session emits,
// so the manager / SSE / dashboard layers are backend-agnostic.
//
// The config (model, provider/base_url+key, MCP, sandbox) lives in a per-agent
// CODEX_HOME/config.toml written by internal/codexcfg; this driver only passes
// CODEX_HOME + provider env and parses the event stream.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/safego"
)

// CodexCostFunc, when set, computes the USD cost of a Codex turn from its
// token usage. The server wires this to state.ComputeCost at startup; left nil
// (e.g. in tests) the emitted EventTokens.Cost is 0. Kept as a package var to
// avoid an internal/pty → internal/state import dependency.
var CodexCostFunc func(model string, inputTokens, outputTokens int64) float64

// codexPromptQueue bounds the per-session pending-prompt buffer.
const codexPromptQueue = 64

// codexStopWaitTimeout bounds how long Stop() waits for the worker goroutine to
// exit before logging a warning and returning anyway. A package var (not a
// const) so tests can shrink it to force the teardown-timeout branch without a
// 5-second wall-clock wait. Production never reassigns it.
var codexStopWaitTimeout = 5 * time.Second

// codexStdoutPipe / codexStderrPipe are seams over (*exec.Cmd).StdoutPipe /
// StderrPipe. The standard-library calls only fail when the corresponding
// stream is already set or the process has already started — neither of which
// runTurn does — so the error branches are otherwise unreachable. Tests
// override these to return an error and exercise the failTurn paths. Production
// uses the direct method calls (behavior-preserving).
var (
	codexStdoutPipe = func(cmd *exec.Cmd) (io.ReadCloser, error) { return cmd.StdoutPipe() }
	codexStderrPipe = func(cmd *exec.Cmd) (io.ReadCloser, error) { return cmd.StderrPipe() }
)

// CodexSession is the ModeCodexExec backend. Field layout mirrors SDKSession
// where the manager/handlers read it, so both satisfy the same method set.
type CodexSession struct {
	mu sync.RWMutex

	ID         string
	WorkingDir string
	CreatedAt  time.Time
	Mode       SessionMode
	name       string

	status         SessionStatus
	isWorking      bool
	lastActivityAt time.Time
	isStopped      bool
	// threadID is the Codex session/thread id (from thread.started); used to
	// `codex exec resume <threadID>` on turns after the first.
	threadID string

	totalInputTokens   int64
	totalOutputTokens  int64
	totalCostUSD       float64
	maxTurnsPerSession int
	turnCount          int
	turnCapFired       bool

	terminalBuffer *BufferAccumulator
	textOutput     *BufferAccumulator

	events     chan SessionEvent
	done       chan struct{}
	cancelFunc context.CancelFunc
	ctx        context.Context
	eventsOnce sync.Once
	doneOnce   sync.Once
	wg         sync.WaitGroup
	prompts    chan string

	droppedEvents atomic.Uint64

	// Config
	providerEnv  map[string]string
	codexHome    string
	codexBin     string
	model        string
	kickstart    string
	subscription bool // ChatGPT/Codex OAuth (auth.json) instead of an API key

	outputObservers   map[int]func(string)
	outputObserverSeq int
	observerMu        sync.Mutex
	activeTools       map[string]bool
}

// NewCodexSession constructs (does not start) a Codex-backed session.
func NewCodexSession(config SessionConfig) *CodexSession {
	id := config.ID
	if id == "" {
		id = uuid.New().String()
	}
	bin := config.CodexBinaryPath
	if bin == "" {
		bin = "codex"
	}
	return &CodexSession{
		ID:                 id,
		WorkingDir:         config.WorkingDir,
		CreatedAt:          time.Now(),
		Mode:               ModeCodexExec,
		name:               config.Name,
		status:             StatusStopped,
		terminalBuffer:     NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo),
		textOutput:         NewBufferAccumulator(MaxTextOutputSize, TrimTextTo),
		events:             make(chan SessionEvent, eventChannelSize),
		done:               make(chan struct{}),
		prompts:            make(chan string, codexPromptQueue),
		providerEnv:        config.ProviderEnv,
		codexHome:          config.CodexHome,
		codexBin:           bin,
		model:              config.Model,
		kickstart:          config.SystemPrompt,
		subscription:       config.CodexSubscription,
		maxTurnsPerSession: config.MaxTurnsPerSession,
		outputObservers:    make(map[int]func(string)),
		activeTools:        make(map[string]bool),
	}
}

// Start launches the turn-worker goroutine and enqueues the kickstart.
func (s *CodexSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.status != StatusStopped {
		s.mu.Unlock()
		return fmt.Errorf("session already started")
	}
	sctx, cancel := context.WithCancel(ctx)
	s.ctx = sctx
	s.cancelFunc = cancel
	s.status = StatusIdle
	s.lastActivityAt = time.Now()
	s.isStopped = false
	s.mu.Unlock()

	s.wg.Add(1)
	safego.Go("pty.codex.worker", func() { s.worker(sctx) })

	if s.kickstart != "" {
		s.SendPrompt(sctx, s.kickstart)
		slog.Info("Codex session kickstart queued", "session", s.ID, "prompt_len", len(s.kickstart))
	}
	return nil
}

// worker serializes turns: one `codex exec` per dequeued prompt. Closes the
// events channel on exit (mirrors SDKSession.eventConsumer).
func (s *CodexSession) worker(ctx context.Context) {
	defer s.wg.Done()
	defer func() {
		s.mu.RLock()
		stopped := s.isStopped
		s.mu.RUnlock()
		if !stopped {
			s.emitEvent(EventExit{ExitCode: 0})
		}
		s.eventsOnce.Do(func() { close(s.events) })
		slog.Info("Codex session worker ended", "session", s.ID)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case prompt := <-s.prompts:
			s.runTurn(ctx, prompt)
		}
	}
}

// codexExecArgs builds the argv (after the binary) for one `codex exec` turn.
// resume is the captured thread id ("" on the first turn). workingDir is the
// agent cwd; prompt is the turn input.
//
// Flag sets differ between the first turn (`codex exec`) and resume turns
// (`codex exec resume`) — both verified against codex-cli 0.135.0:
//
//   - NEITHER accepts `--ask-for-approval` (interactive-only). Approval is
//     driven by `approval_policy` in the per-agent CODEX_HOME/config.toml
//     (codexcfg writes "never"). Passing it hard-errors "unexpected argument".
//   - `codex exec` accepts -s/--sandbox <mode> and -C/--cd <dir>.
//   - `codex exec resume` does NOT accept -s/--sandbox or -C/--cd — sandbox
//     comes from config.toml's sandbox_mode and cwd from the process working
//     dir (cmd.Dir). Passing --sandbox to a resume turn hard-errors, which
//     previously failed every turn after the first.
//   - Both accept --json and --skip-git-repo-check.
//
// The sandbox is NOT passed as a flag: it comes from config.toml's
// sandbox_mode (codexcfg writes it) on BOTH turn shapes, so the flag can't
// override the configured policy and the first/resume turns stay consistent.
// cwd is set via cmd.Dir; -C is passed on the first turn for clarity.
//
// Keep aligned with `codex exec --help` / `codex exec resume --help`; the
// regression test in codex_session_test.go pins both shapes.
// killProcessGroup is the cmd.Cancel implementation for a codex turn: it
// SIGKILLs the WHOLE process group (-pid) so the Node launcher AND the platform
// binary it spawned both die, instead of orphaning the child. Extracted from the
// inline cmd.Cancel closure so both arms are unit-testable: the runtime only
// ever invokes Cancel after Start sets cmd.Process, so the nil guard is
// otherwise unreachable.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func codexExecArgs(resume, workingDir, prompt string) []string {
	if resume != "" {
		// Resume the captured thread (preserves transcript + prompt cache).
		return []string{"exec", "resume", resume, "--json", "--skip-git-repo-check", prompt}
	}
	return []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"-C", workingDir,
		prompt,
	}
}

// runTurn spawns one `codex exec` invocation and consumes its JSONL stream.
func (s *CodexSession) runTurn(ctx context.Context, prompt string) {
	s.setWorking()

	s.mu.RLock()
	resume := s.threadID
	s.mu.RUnlock()
	args := codexExecArgs(resume, s.WorkingDir, prompt)

	cmd := exec.CommandContext(ctx, s.codexBin, args...)
	cmd.Dir = s.WorkingDir
	cmd.Env = s.buildEnv()
	cmd.Stdin = nil // /dev/null — codex exec hangs reading an open stdin
	// Run the turn in its own process group and kill the WHOLE group on
	// ctx-cancel. The `codex` binary is a Node launcher that spawns the
	// platform binary as a child; the default CommandContext kill only reaps
	// the launcher and orphans that child, which keeps running (and spending)
	// after Stop()/shutdown-team. Setpgid + kill(-pgid) reaps both.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcessGroup(cmd) }

	stdout, err := codexStdoutPipe(cmd)
	if err != nil {
		s.failTurn(fmt.Sprintf("codex stdout pipe: %v", err))
		return
	}
	stderr, err := codexStderrPipe(cmd)
	if err != nil {
		s.failTurn(fmt.Sprintf("codex stderr pipe: %v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		s.failTurn(fmt.Sprintf("codex start (is the `codex` CLI installed?): %v", err))
		return
	}

	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	safego.Go("pty.codex.stderr", func() {
		defer stderrWG.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := logscrub.All(sc.Text())
			lower := strings.ToLower(line)
			if strings.Contains(lower, "error") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "rate") {
				slog.Warn("Codex subprocess error", "session", s.ID, "stderr", line)
			} else {
				slog.Debug("Codex subprocess stderr", "session", s.ID, "line", line)
			}
		}
		// Drain any remaining stderr if the scanner stopped early (e.g. a line
		// exceeded the 1MB token cap -> ErrTooLong). Without this the child can
		// block once the OS stderr pipe buffer fills, wedging cmd.Wait() — the
		// stdout path does the same drain on its scan error.
		if sc.Err() != nil {
			_, _ = io.Copy(io.Discard, stderr)
		}
	})

	sawCompleted := s.consumeJSONL(stdout)
	stderrWG.Wait()
	waitErr := cmd.Wait()

	// Turn finished — back to idle and let the dashboard settle.
	s.setIdle()
	s.emitEvent(EventIdle{})

	if waitErr != nil && !sawCompleted {
		// Process died without a clean turn.completed (e.g. auth failure,
		// crash). Surface it instead of silently falling idle.
		s.emitEvent(EventTurnError{Subtype: "exec-failed", Result: waitErr.Error()})
		slog.Error("Codex turn failed", "session", s.ID, "error", waitErr)
	}

	s.mu.Lock()
	s.turnCount++
	turns := s.turnCount
	cap := s.maxTurnsPerSession
	fired := s.turnCapFired
	cost := s.totalCostUSD
	s.mu.Unlock()
	if cap > 0 && !fired && turns >= cap {
		s.mu.Lock()
		s.turnCapFired = true
		s.mu.Unlock()
		s.emitEvent(EventTurnCapReached{Turns: turns, Cap: cap, Cost: cost})
		slog.Warn("Codex session hit turn cap; stopping", "session", s.ID, "turns", turns, "cap", cap)
		// Emit EventExit so the death flows through the forwarder's normal
		// EventExit path (HandleAgentDeath + cleanup). go s.Stop() below sets
		// isStopped, so the worker's defer won't emit a second EventExit.
		// Tagged with ExitReasonTurnCap so the office forwarder can exempt
		// this legitimate rollover from the respawn-cap budget — critical on
		// Codex, where turnCount accumulates across the whole session.
		s.emitEvent(EventExit{ExitCode: 0, Reason: ExitReasonTurnCap})
		go s.Stop()
	}
}

// codexEvent is one line of the `codex exec --json` JSONL stream.
type codexEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Message  string          `json:"message"` // "error" events
	Item     *codexItem      `json:"item"`
	Usage    *codexUsage     `json:"usage"`
	Error    *codexErrDetail `json:"error"` // "turn.failed"
}

type codexItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // agent_message|reasoning|command_execution|file_change|mcp_tool_call|web_search|plan_update
	Text    string `json:"text"`
	Command string `json:"command"`
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	Status  string `json:"status"`
}

type codexUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type codexErrDetail struct {
	Message string `json:"message"`
}

// consumeJSONL parses the codex exec --json stream from r, emitting jarvis
// events. Returns true if a turn.completed event was seen (clean turn).
// Exported-shape (lowercase) but exercised directly by codex_session_test.go
// against captured fixtures.
func (s *CodexSession) consumeJSONL(r io.Reader) bool {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	sawCompleted := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // ignore non-JSON / partial lines
		}
		s.mu.Lock()
		s.lastActivityAt = time.Now()
		s.mu.Unlock()

		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				s.mu.Lock()
				if s.threadID == "" {
					s.threadID = ev.ThreadID
				}
				s.mu.Unlock()
			}
		case "turn.started":
			s.setWorking()
		case "item.started", "item.updated":
			if ev.Item != nil && isCodexToolItem(ev.Item.Type) {
				s.setWorking()
			}
		case "item.completed":
			if ev.Item != nil {
				switch ev.Item.Type {
				case "agent_message":
					if ev.Item.Text != "" {
						s.appendOutput(ev.Item.Text + "\n")
					}
				case "reasoning":
					s.setWorking()
				case "command_execution", "file_change", "mcp_tool_call", "web_search":
					s.emitEvent(EventToolInvocation{
						Name:      codexToolName(ev.Item),
						ToolUseID: ev.Item.ID,
						IsError:   ev.Item.Status == "failed" || ev.Item.Status == "error",
					})
				}
			}
		case "turn.completed":
			sawCompleted = true
			if ev.Usage != nil {
				in := ev.Usage.InputTokens
				out := ev.Usage.OutputTokens + ev.Usage.ReasoningOutputTokens
				var cost float64
				if CodexCostFunc != nil {
					cost = CodexCostFunc(s.model, in, out)
				}
				s.mu.Lock()
				s.totalInputTokens += in
				s.totalOutputTokens += out
				s.totalCostUSD += cost
				s.mu.Unlock()
				s.emitEvent(EventTokens{Input: in, Output: out, Total: in + out, Cost: cost, Model: s.model})
			}
		case "error":
			if ev.Message != "" {
				s.appendOutput("[Codex error] " + ev.Message + "\n")
				slog.Warn("Codex stream error", "session", s.ID, "message", ev.Message)
			}
		case "turn.failed":
			msg := ev.Message
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			s.emitEvent(EventTurnError{Subtype: "turn.failed", Result: msg})
			slog.Error("Codex turn.failed", "session", s.ID, "message", msg)
		}
	}
	// An oversized line (>4MB -> bufio.ErrTooLong) or a read error ends the
	// scan early. Drain the rest of stdout so the codex process can finish
	// writing and cmd.Wait() can reap it, instead of wedging the worker. Not
	// treated as a turn failure on its own (sawCompleted reflects what we saw).
	if err := sc.Err(); err != nil {
		slog.Warn("Codex JSONL scan ended early; draining stdout", "session", s.ID, "error", err)
		_, _ = io.Copy(io.Discard, r)
	}
	return sawCompleted
}

func isCodexToolItem(t string) bool {
	switch t {
	case "command_execution", "file_change", "mcp_tool_call", "web_search":
		return true
	default:
		return false
	}
}

func codexToolName(it *codexItem) string {
	switch it.Type {
	case "command_execution":
		return "Bash"
	case "file_change":
		return "Edit"
	case "web_search":
		return "WebSearch"
	case "mcp_tool_call":
		if it.Server != "" && it.Tool != "" {
			return "mcp__" + it.Server + "__" + it.Tool
		}
		return "mcp"
	default:
		return it.Type
	}
}

// buildEnv merges the host env with providerEnv + CODEX_HOME. In subscription
// mode it also strips OPENAI_API_KEY / CODEX_API_KEY (which the container sets
// for the shared API-key path) so the built-in `openai` provider authenticates
// with the `codex login` OAuth credentials in CODEX_HOME/auth.json rather than
// an inherited API key.
func (s *CodexSession) buildEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if s.subscription && (strings.HasPrefix(kv, "OPENAI_API_KEY=") || strings.HasPrefix(kv, "CODEX_API_KEY=")) {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range s.providerEnv {
		if s.subscription && (k == "OPENAI_API_KEY" || k == "CODEX_API_KEY") {
			continue
		}
		env = append(env, k+"="+v)
	}
	if s.codexHome != "" {
		env = append(env, "CODEX_HOME="+s.codexHome)
	}
	return env
}

// Tokens returns input, output, and total token counts accumulated across
// this Codex session's turns. Mirrors Session.Tokens() so the token-stats
// handler can aggregate Codex sessions alongside PTY and SDK sessions.
func (s *CodexSession) Tokens() (input, output, total int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalInputTokens, s.totalOutputTokens, s.totalInputTokens + s.totalOutputTokens
}

func (s *CodexSession) setWorking() {
	s.mu.Lock()
	if !s.isWorking {
		s.isWorking = true
		s.status = StatusBusy
		s.mu.Unlock()
		s.emitEvent(EventWorking{})
		return
	}
	s.mu.Unlock()
}

func (s *CodexSession) setIdle() {
	s.mu.Lock()
	s.isWorking = false
	s.status = StatusIdle
	clear(s.activeTools)
	s.mu.Unlock()
}

func (s *CodexSession) failTurn(msg string) {
	s.setIdle()
	s.emitEvent(EventTurnError{Subtype: "exec-failed", Result: msg})
	s.emitEvent(EventIdle{})
	slog.Error("Codex turn setup failed", "session", s.ID, "error", msg)
}

func (s *CodexSession) appendOutput(text string) {
	s.mu.Lock()
	s.terminalBuffer.Append(text)
	s.textOutput.Append(text)
	s.mu.Unlock()
	s.emitEvent(EventTerminal{Data: text})
	s.emitEvent(EventOutput{Data: text})
	s.notifyOutputObservers(text)
}

func (s *CodexSession) emitEvent(event SessionEvent) {
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
		if total == 1 || total%100 == 0 {
			slog.Warn("Codex session event backlog — events dropped", "session", s.ID, "totalDropped", total, "event", fmt.Sprintf("%T", event))
		}
	}
}

// notifyOutputObservers calls all registered output observers.
// Clones into a []func(string) slice (same pattern as session.go) so
// the hot path allocates a slice header rather than a full map.
func (s *CodexSession) notifyOutputObservers(data string) {
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

// --- methods mirroring SDKSession so the manager/handlers are backend-agnostic ---

func (s *CodexSession) Events() <-chan SessionEvent { return s.events }

// SendPrompt enqueues a prompt as the next turn (non-blocking). The worker
// runs turns serially; a prompt that arrives mid-turn runs after it completes.
func (s *CodexSession) SendPrompt(_ context.Context, prompt string) {
	s.mu.RLock()
	stopped := s.isStopped
	s.mu.RUnlock()
	if stopped {
		return
	}
	select {
	case s.prompts <- prompt:
	default:
		slog.Warn("Codex session prompt queue full; dropping prompt", "session", s.ID)
	}
}

func (s *CodexSession) Stop() {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return
	}
	s.isStopped = true
	s.status = StatusStopped
	cancel := s.cancelFunc
	s.mu.Unlock()

	if cancel != nil {
		cancel() // kills any in-flight `codex exec` via CommandContext
	}
	waitDone := make(chan struct{})
	go func() { s.wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(codexStopWaitTimeout):
		slog.Warn("Codex session worker did not exit within 5s", "session", s.ID)
	}
	s.observerMu.Lock()
	s.outputObservers = nil
	s.observerMu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
	// Remove the per-agent CODEX_HOME (config.toml + staged OAuth auth.json +
	// rollout logs). The worker has exited, so the codex process is dead and no
	// longer reading it. Without this, every spawn/respawn leaks disk and leaves
	// another 0600 copy of the operator's long-lived OAuth credential on disk.
	if s.codexHome != "" {
		if err := os.RemoveAll(s.codexHome); err != nil {
			slog.Warn("Codex session: failed to remove CODEX_HOME", "session", s.ID, "path", s.codexHome, "error", err)
		}
	}
	slog.Info("Codex session stopped", "session", s.ID)
}

// ClaudeSessionID returns the Codex thread id (named for method-set parity
// with SDKSession so handlers/snapshots treat both uniformly).
func (s *CodexSession) ClaudeSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threadID
}

func (s *CodexSession) SetClaudeSessionIDForTest(id string) {
	s.mu.Lock()
	s.threadID = id
	s.mu.Unlock()
}

func (s *CodexSession) GetStatus() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *CodexSession) IsWorking() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isWorking
}

func (s *CodexSession) GetName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *CodexSession) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *CodexSession) GetTerminalBuffer() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.terminalBuffer.Value()
}

func (s *CodexSession) WriteToPTY(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || input == "\r" {
		return false
	}
	s.SendPrompt(context.Background(), strings.TrimRight(input, "\r\n"))
	return true
}

func (s *CodexSession) WriteInkPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	s.SendPrompt(context.Background(), prompt)
	return true
}

func (s *CodexSession) WriteViaScreen(input string) bool {
	cleaned := strings.TrimSpace(stripControlChars(input))
	if cleaned == "" {
		return false
	}
	s.SendPrompt(context.Background(), cleaned)
	return true
}

func (s *CodexSession) DroppedEvents() uint64 { return s.droppedEvents.Load() }

func (s *CodexSession) AddOutputObserver(fn func(string)) func() {
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

func (s *CodexSession) GetActiveTools() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]string, 0, len(s.activeTools))
	for t := range s.activeTools {
		tools = append(tools, t)
	}
	return tools
}
