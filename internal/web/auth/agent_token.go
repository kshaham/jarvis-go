package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// agentTokenBytes controls the length of the raw token. 32 bytes = 64 hex chars,
// enough entropy to resist brute-force on the loopback interface.
const agentTokenBytes = 32

// AgentTokenManager loads or generates the process-local token that the `cm`
// CLI uses to authenticate to the /api/office/agent/* endpoints. The token is
// persisted at `configDir/agent-token` with 0600 permissions so it survives
// restarts; operators can force rotation by deleting the file.
//
// The token intentionally lives alongside the HMAC signing secret. Both are
// secrets known to the server process; neither is ever returned from the API.
type AgentTokenManager struct {
	path  string
	mu    sync.RWMutex
	token string
}

// NewAgentTokenManager loads the persisted token or generates a new one.
func NewAgentTokenManager(configDir string) *AgentTokenManager {
	m := &AgentTokenManager{
		path: filepath.Join(configDir, "agent-token"),
	}
	m.loadOrCreate()
	return m
}

func (m *AgentTokenManager) loadOrCreate() {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		slog.Error("Failed to create config dir for agent token", "error", err)
		return
	}
	if data, err := os.ReadFile(m.path); err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) == agentTokenBytes*2 { // hex-encoded length
			m.mu.Lock()
			m.token = token
			m.mu.Unlock()
			slog.Info("Loaded existing agent token")
			return
		}
		slog.Warn("Agent token file had unexpected length; regenerating")
	}

	raw := make([]byte, agentTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("Failed to generate agent token", "error", err)
		return
	}
	encoded := hex.EncodeToString(raw)

	m.mu.Lock()
	m.token = encoded
	m.mu.Unlock()

	// R26 state review: atomic write (temp + rename) so a crash
	// mid-write can't leave the token file truncated. A truncated
	// token invalidates every in-flight agent session on the next
	// boot, which is avoidable.
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(encoded), 0600); err != nil {
		slog.Error("Failed to persist agent token (write tmp)", "error", err)
		return
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		slog.Error("Failed to persist agent token (rename)", "error", err)
		return
	}
	slog.Info("Generated new agent token", "path", m.path)
}

// Token returns the current agent token. Used by the web server to thread the
// value into session provider env vars so `cm` can read it.
func (m *AgentTokenManager) Token() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.token
}

// Validate returns true when the presented token matches the current one.
// Uses constant-time comparison to avoid timing side channels.
func (m *AgentTokenManager) Validate(presented string) bool {
	m.mu.RLock()
	cur := m.token
	m.mu.RUnlock()
	if cur == "" || len(presented) != len(cur) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cur), []byte(presented)) == 1
}
