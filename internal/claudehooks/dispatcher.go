package claudehooks

// HTTP dispatcher for Claude Code hook events. Mounted at
//   /api/claude/hooks/{sessionId}/{event}
// in internal/web/server.go and protected by the same agent-token
// middleware as the rest of /api/office/agent/*. Uses chi router
// idioms (URLParam) but avoids importing chi directly so this package
// stays trivially testable with httptest.
//
// The dispatcher's job is to (a) authenticate the route, (b) decode
// the JSON envelope, (c) update AgentState via the AgentRegistry
// interface, and (d) write a tiny response. All of that should
// complete in well under 10ms so the spawned `claude` process never
// stalls -- HTTP hooks are blocking, per the Claude Code docs.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/claudeman/jarvis-go/internal/strs"
	"github.com/go-chi/chi/v5"
)

// AgentRegistry is the narrow office.Manager surface the dispatcher
// needs. Defining it here keeps this package independent of
// internal/office (which imports settings.go from this package, so a
// reverse import would create a cycle). The concrete *office.Manager
// satisfies this interface; tests can substitute a fake.
type AgentRegistry interface {
	// MutateAgent invokes fn with the live AgentState for sessionID
	// under the agent's mutex. Returns false if no such agent is
	// registered. The function MUST NOT block on I/O or network --
	// it runs on the hook request goroutine, which the spawned
	// claude is waiting on.
	MutateAgent(sessionID string, fn func(MutableAgent)) bool

	// HandleAgentDeath kicks the office's agent-death pathway
	// (task reassignment, dev-instance cleanup, broadcasts).
	// Mirrors the SDK forwarder's session-closed branch.
	HandleAgentDeath(sessionID string)

	// CheckForIdleAgents pre-empts the idle-replenishment cycle.
	// TeammateIdle hook lets us call this *before* the office's
	// own idle ticker fires, so brain-storm partners get matched
	// faster.
	CheckForIdleAgents()

	// Broadcast emits an SSE event to the dashboard. Same shape as
	// the broadcast func everything else in office uses.
	Broadcast(event string, data interface{})
}

// MutableAgent is the slice of AgentState methods the hook handlers
// touch. Mirroring the existing office.AgentState surface so the
// office package can satisfy this with no wrapper code (its methods
// already have the right signatures).
type MutableAgent interface {
	SetActiveTool(name string)
	SetSDKWorking(working bool)
	RecordTurnUsage(input, output int64, cost float64)
	TouchAgentAPI()
	FlushTurnText() string
	// SetWorktreePath updates the agent's bookkeeping when Claude
	// itself creates a worktree (via --worktree or a Task-tool spawn).
	// Returns the previous value so callers can detect changes for
	// logging / SSE-deduplication purposes.
	SetWorktreePath(path string) string
}

// Dispatcher routes incoming hook POSTs to per-event handlers. Holds
// the AgentRegistry it needs to mutate state plus a small flag that
// records the first event seen per session (for the "loud first
// hook" log line).
type Dispatcher struct {
	registry AgentRegistry

	// Tool-loop observer; nil if not configured. Mirrors the call site
	// in internal/pty/sdk_session.go for EventToolInvocation. Plumbed
	// as a func to avoid importing the office package directly.
	observeToolInvocation func(sessionID, toolName, resultHash string)

	// toolPolicy is consulted on PreToolUse to deny dangerous tool
	// calls even when the agent runs in bypass-permissions mode. nil
	// disables the policy (tests, minimal deployments).
	toolPolicy *ToolPolicyStore

	// subagentStart / subagentStop (R3.2) forward Subagent{Start,Stop}
	// hook events to the office's subagent watcher so the dashboard
	// sees lifecycle changes immediately instead of waiting for the
	// next filesystem-scan tick. Plumbed as callbacks rather than a
	// direct dependency so this package stays import-cycle-free from
	// internal/subagent. Both default to nil (broadcast-only
	// behaviour, matching the pre-R3.2 wiring).
	subagentStart func(sessionID, agentType, agentID string)
	subagentStop  func(sessionID, agentID string)

	mu          sync.Mutex
	firstSeen   map[string]bool // sessionID -> we've logged the first event
}

// NewDispatcher builds a Dispatcher wired to an AgentRegistry. The
// optional observeToolInvocation hook is used by PostToolUse to feed
// the tool-loop detector; pass nil if loop detection isn't wanted
// (tests, minimal deployments).
func NewDispatcher(registry AgentRegistry, observeToolInvocation func(sessionID, toolName, resultHash string)) *Dispatcher {
	return &Dispatcher{
		registry:              registry,
		observeToolInvocation: observeToolInvocation,
		firstSeen:             make(map[string]bool),
	}
}

// SetToolPolicy attaches a policy store so PreToolUse denials are
// enforced. nil disables enforcement (returns to passthrough
// behaviour). Safe to call after NewDispatcher.
func (d *Dispatcher) SetToolPolicy(policy *ToolPolicyStore) {
	d.toolPolicy = policy
}

// SetSubagentObservers attaches optional callbacks that fire on
// SubagentStart / SubagentStop hook events. Either may be nil. The
// office's subagent watcher uses this to drop most of its
// filesystem-poll latency for hooks-mode sessions.
func (d *Dispatcher) SetSubagentObservers(
	onStart func(sessionID, agentType, agentID string),
	onStop func(sessionID, agentID string),
) {
	d.subagentStart = onStart
	d.subagentStop = onStop
}

// HandleEvent is the single chi-mounted handler. Reads sessionId +
// event from the URL, decodes the body, dispatches to a per-event
// handler, and writes the response. All errors fail-open with 200 +
// empty body so a Jarvis-side bug never wedges the spawned claude
// (which would block the operator until they manually kill it).
func (d *Dispatcher) HandleEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	event := chi.URLParam(r, "event")
	if sessionID == "" || event == "" {
		// Malformed URL; return 200 anyway (fail-open) but log.
		slog.Warn("claudehooks: bad URL", "session", sessionID, "event", event)
		writeNoContent(w)
		return
	}
	// Defense-in-depth: reject sessionIDs that don't match jarvis's
	// canonical shape before any state lookup. The agent-token
	// middleware already gated this route, but a compromised token
	// shouldn't be able to probe arbitrary registry keys (`../admin`,
	// shell metacharacters, etc.). Use 400 here -- this is NOT a
	// liveness path; a malformed sessionId is a bug, not a race.
	if !strs.IsValidSessionID(sessionID) {
		slog.Warn("claudehooks: rejecting malformed sessionId",
			"len", len(sessionID), "event", event)
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	// Fast-path: touch the agent's liveness counter and log the
	// first event we see for this session. If the agent isn't
	// registered, we still return 200 so the spawned claude doesn't
	// stall -- this happens during the brief window between spawn
	// and RegisterAgent landing.
	registered := d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.TouchAgentAPI()
	})
	d.maybeLogFirstEvent(sessionID, event, registered)
	if !registered {
		writeNoContent(w)
		return
	}

	// Decode + dispatch. Per-event handlers each get the raw body
	// to decode against their specific envelope type.
	body, err := readLimited(r, 1<<20) // 1 MB matches decodeBody in handler/office.go
	if err != nil {
		slog.Warn("claudehooks: read body", "session", sessionID, "event", event, "err", err)
		writeNoContent(w)
		return
	}

	resp, err := d.dispatch(sessionID, event, body)
	if err != nil {
		slog.Warn("claudehooks: dispatch", "session", sessionID, "event", event, "err", err)
		writeNoContent(w)
		return
	}
	if resp == nil {
		writeNoContent(w)
		return
	}
	writeJSON(w, resp)
}

// dispatch is the per-event switch. Returns a non-nil HookResponse
// only for events that need to influence Claude's flow; everything
// else returns nil (which becomes a 204).
func (d *Dispatcher) dispatch(sessionID, event string, body []byte) (*HookResponse, error) {
	switch event {
	case "SessionStart":
		var ev SessionStartEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode SessionStart: %w", err)
		}
		return d.onSessionStart(sessionID, &ev), nil

	case "UserPromptSubmit":
		var ev UserPromptSubmitEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode UserPromptSubmit: %w", err)
		}
		return d.onUserPromptSubmit(sessionID, &ev), nil

	case "PreToolUse":
		var ev PreToolUseEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PreToolUse: %w", err)
		}
		return d.onPreToolUse(sessionID, &ev), nil

	case "PostToolUse":
		var ev PostToolUseEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PostToolUse: %w", err)
		}
		return d.onPostToolUse(sessionID, &ev, false), nil

	case "PostToolUseFailure":
		var ev PostToolUseEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PostToolUseFailure: %w", err)
		}
		return d.onPostToolUse(sessionID, &ev, true), nil

	case "Stop":
		var ev StopEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode Stop: %w", err)
		}
		return d.onStop(sessionID, &ev), nil

	case "SessionEnd":
		var ev SessionEndEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode SessionEnd: %w", err)
		}
		return d.onSessionEnd(sessionID, &ev), nil

	case "Notification":
		var ev NotificationEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode Notification: %w", err)
		}
		return d.onNotification(sessionID, &ev), nil

	case "PreCompact", "PostCompact":
		var ev CompactEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode %s: %w", event, err)
		}
		return d.onCompact(sessionID, event, &ev), nil

	case "PostToolBatch":
		var ev PostToolBatchEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PostToolBatch: %w", err)
		}
		return d.onPostToolBatch(sessionID, &ev), nil

	case "StopFailure":
		var ev StopFailureEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode StopFailure: %w", err)
		}
		return d.onStopFailure(sessionID, &ev), nil

	case "PermissionRequest":
		var ev PermissionRequestEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PermissionRequest: %w", err)
		}
		return d.onPermissionRequest(sessionID, &ev), nil

	case "PermissionDenied":
		var ev PermissionDeniedEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode PermissionDenied: %w", err)
		}
		return d.onPermissionDenied(sessionID, &ev), nil

	case "TaskCreated":
		var ev TaskCreatedEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode TaskCreated: %w", err)
		}
		return d.onTaskCreated(sessionID, &ev), nil

	case "TaskCompleted":
		var ev TaskCompletedEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode TaskCompleted: %w", err)
		}
		return d.onTaskCompleted(sessionID, &ev), nil

	case "SubagentStart":
		var ev SubagentStartEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode SubagentStart: %w", err)
		}
		return d.onSubagentStart(sessionID, &ev), nil

	case "SubagentStop":
		var ev SubagentStopEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode SubagentStop: %w", err)
		}
		return d.onSubagentStop(sessionID, &ev), nil

	case "CwdChanged":
		var ev CwdChangedEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode CwdChanged: %w", err)
		}
		return d.onCwdChanged(sessionID, &ev), nil

	case "InstructionsLoaded":
		var ev InstructionsLoadedEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode InstructionsLoaded: %w", err)
		}
		return d.onInstructionsLoaded(sessionID, &ev), nil

	case "ConfigChange":
		var ev ConfigChangeEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode ConfigChange: %w", err)
		}
		return d.onConfigChange(sessionID, &ev), nil

	case "Elicitation":
		var ev ElicitationEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode Elicitation: %w", err)
		}
		return d.onElicitation(sessionID, &ev), nil

	case "ElicitationResult":
		var ev ElicitationResultEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode ElicitationResult: %w", err)
		}
		return d.onElicitationResult(sessionID, &ev), nil

	case "WorktreeCreate":
		var ev WorktreeCreateEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode WorktreeCreate: %w", err)
		}
		return d.onWorktreeCreate(sessionID, &ev), nil

	case "WorktreeRemove":
		var ev WorktreeRemoveEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("decode WorktreeRemove: %w", err)
		}
		return d.onWorktreeRemove(sessionID, &ev), nil

	case "TeammateIdle":
		// No body fields we consume; the registry call is the action.
		d.registry.CheckForIdleAgents()
		return nil, nil

	default:
		// Unknown event: log once, accept silently. A future Claude
		// release adding an event we haven't wired shouldn't break
		// existing deployments.
		slog.Debug("claudehooks: unknown event", "session", sessionID, "event", event)
		return nil, nil
	}
}

// maybeLogFirstEvent emits a single info log per session the first
// time any hook fires. Loud and once-per-session -- proves the
// settings.local.json is wired correctly without spamming the log on
// every PostToolUse.
func (d *Dispatcher) maybeLogFirstEvent(sessionID, event string, registered bool) {
	d.mu.Lock()
	if d.firstSeen[sessionID] {
		d.mu.Unlock()
		return
	}
	d.firstSeen[sessionID] = true
	d.mu.Unlock()
	slog.Info("claudehooks first event",
		"session", sessionID,
		"event", event,
		"agentRegistered", registered,
		"at", time.Now().Format(time.RFC3339),
	)
}

// readLimited is decodeBody's body-reading half, extracted so this
// package doesn't depend on internal/web/handler. 1 MB cap matches
// the existing handler/office.go convention.
func readLimited(r *http.Request, max int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, max)
	defer r.Body.Close()
	const chunk = 4096
	buf := make([]byte, 0, chunk)
	tmp := make([]byte, chunk)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, http.ErrBodyReadAfterClose) || isEOF(err) {
				return buf, nil
			}
			return buf, err
		}
	}
}

func isEOF(err error) bool {
	return err != nil && strings.Contains(err.Error(), "EOF")
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
