package office

// Manager-side adapter that lets internal/claudehooks call into the
// office without importing it. The dispatcher only depends on the
// claudehooks.AgentRegistry interface; this file makes *Manager
// satisfy it without polluting manager.go.
//
// Why an adapter file rather than methods on Manager directly:
//   - Keeps the cycle-prevention contract obvious: claudehooks
//     defines the interface, office provides the impl.
//   - Lets us evolve the interface (add methods) by editing one file
//     here instead of hunting through manager.go.

import (
	"github.com/kshaham/jarvis/internal/claudehooks"
)

// Compile-time check that *Manager satisfies the AgentRegistry contract.
// If the interface gains a method, this line is the first to break.
var _ claudehooks.AgentRegistry = (*Manager)(nil)

// MutateAgent implements claudehooks.AgentRegistry. Looks up the
// agent and invokes fn with its mutex-guarded surface. Returns false
// if no such agent is registered (e.g. the spawned `claude` fired a
// hook before RegisterAgent landed -- benign; the dispatcher
// fail-opens with 200 in that case).
//
// The fn runs synchronously on the HTTP hook request goroutine so
// the spawned claude is blocked on it. Mutations on AgentState are
// fast (mutex + field write), but anything that does I/O or channel
// sends MUST happen outside fn (caller can launch a goroutine after
// MutateAgent returns).
func (m *Manager) MutateAgent(sessionID string, fn func(claudehooks.MutableAgent)) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	// AgentState's existing methods (SetActiveTool, SetSDKWorking,
	// RecordTurnUsage, TouchAgentAPI, FlushTurnText) match the
	// MutableAgent interface signatures byte-for-byte, so *AgentState
	// satisfies it via implicit interface satisfaction.
	fn(agent)
	return true
}

// Broadcast implements claudehooks.AgentRegistry by routing to the
// existing manager broadcast func. Same SSE channel the rest of the
// office uses for office:* events.
func (m *Manager) Broadcast(event string, data interface{}) {
	m.broadcast(event, data)
}

// SetDaemonURL records the base URL used in generated
// .claude/settings.local.json files. Called once during server
// bootstrap with the bind URL so spawned claude processes know
// where to POST hook events. Empty string disables hooks injection
// (worktree.go falls back to legacy ModeClaude behaviour).
func (m *Manager) SetDaemonURL(url string) {
	m.mu.Lock()
	m.daemonURL = url
	m.mu.Unlock()
}

// GetDaemonURL returns the base URL hooks-mode spawns will bake into
// settings.local.json. Read by worktree.go at spawn time.
func (m *Manager) GetDaemonURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.daemonURL
}
