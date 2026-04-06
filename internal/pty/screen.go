package pty

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScreenManager manages GNU Screen sessions that wrap Claude CLI processes.
type ScreenManager struct {
	mu          sync.RWMutex
	screens     map[string]*ScreenSession
	screensFile string
}

// NewScreenManager creates a new screen manager.
func NewScreenManager(dataDir string) *ScreenManager {
	sm := &ScreenManager{
		screens:     make(map[string]*ScreenSession),
		screensFile: filepath.Join(dataDir, "screens.json"),
	}
	sm.loadScreens()
	return sm
}

// CreateScreen creates a new GNU Screen session for a Jarvis session.
func (sm *ScreenManager) CreateScreen(sessionID, workingDir string, mode SessionMode, name string, niceConfig NiceConfig, providerEnv map[string]string) (*ScreenSession, error) {
	screenName := fmt.Sprintf("jarvis-%s", truncate(sessionID, 20))

	if !isValidScreenName(screenName) {
		return nil, fmt.Errorf("invalid screen name: %q", screenName)
	}
	if !isValidPath(workingDir) {
		return nil, fmt.Errorf("invalid working directory: %q", workingDir)
	}

	sm.mu.Lock()
	if existing, ok := sm.screens[sessionID]; ok {
		sm.mu.Unlock()
		return existing, nil
	}
	// Insert placeholder to prevent concurrent callers from passing the check
	placeholder := &ScreenSession{
		SessionID:  sessionID,
		ScreenName: screenName,
		PID:        0,
		CreatedAt:  time.Now(),
		WorkingDir: workingDir,
		Mode:       mode,
		Name:       name,
	}
	sm.screens[sessionID] = placeholder
	sm.mu.Unlock()

	envExports := buildEnvExports(sessionID, screenName, providerEnv)
	baseCmd := buildBaseCommand(mode)
	cmd := wrapWithNice(baseCmd, niceConfig)
	fullCmd := fmt.Sprintf("cd %s && %s%s", shellQuote(workingDir), envExports, cmd)

	screenCmd := fmt.Sprintf("screen -dmS %s -c /dev/null bash -c %s",
		screenName, shellQuote(fullCmd))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := runShellCommand(ctx, screenCmd); err != nil {
		// Remove placeholder on failure
		sm.mu.Lock()
		delete(sm.screens, sessionID)
		sm.mu.Unlock()
		return nil, fmt.Errorf("failed to create screen: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	pid, err := getScreenPID(screenName)
	if err != nil || pid == 0 {
		// Remove placeholder on failure
		sm.mu.Lock()
		delete(sm.screens, sessionID)
		sm.mu.Unlock()
		return nil, fmt.Errorf("screen created but PID not found for %s", screenName)
	}

	screen := &ScreenSession{
		SessionID:  sessionID,
		ScreenName: screenName,
		PID:        pid,
		CreatedAt:  time.Now(),
		WorkingDir: workingDir,
		Mode:       mode,
		Name:       name,
	}

	sm.mu.Lock()
	sm.screens[sessionID] = screen
	sm.mu.Unlock()
	sm.saveScreens()

	return screen, nil
}

const stuffChunkSize = 300

// SendInput sends input to a screen session via screen -X stuff.
func (sm *ScreenManager) SendInput(sessionID, input string) bool {
	sm.mu.RLock()
	screen, ok := sm.screens[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return false
	}

	if !isValidScreenName(screen.ScreenName) {
		return false
	}

	hasCR := strings.Contains(input, "\r")
	textPart := strings.ReplaceAll(input, "\r", "")
	textPart = strings.ReplaceAll(textPart, "\n", "")
	textPart = strings.TrimRight(textPart, " \t")

	// Send text in chunks
	for i := 0; i < len(textPart); i += stuffChunkSize {
		end := i + stuffChunkSize
		if end > len(textPart) {
			end = len(textPart)
		}
		chunk := textPart[i:end]
		escaped := shellEscape(chunk)
		if escaped == "" {
			continue
		}
		cmd := fmt.Sprintf(`screen -S %s -p 0 -X stuff "%s"`, screen.ScreenName, escaped)
		if err := runShellCommandTimeout(cmd, 10*time.Second); err != nil {
			return false
		}
		if end < len(textPart) {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if hasCR {
		if textPart != "" {
			time.Sleep(100 * time.Millisecond)
		}
		crCmd := fmt.Sprintf(`screen -S %s -p 0 -X stuff "$(printf '\015')"`, screen.ScreenName)
		for attempt := 1; attempt <= 3; attempt++ {
			if err := runShellCommandTimeout(crCmd, 10*time.Second); err == nil {
				return true
			}
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
		}
		return false
	}

	return true
}

// KillScreen stops and removes a screen session.
func (sm *ScreenManager) KillScreen(sessionID string) (bool, error) {
	sm.mu.RLock()
	screen, ok := sm.screens[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("screen not found: %s", sessionID)
	}

	// Strategy 1: screen -X quit
	_ = runShellCommandTimeout(
		fmt.Sprintf("screen -S %s -X quit", screen.ScreenName),
		5*time.Second,
	)

	// Strategy 2: SIGTERM to screen PID
	if screen.PID > 0 {
		proc, err := os.FindProcess(screen.PID)
		if err == nil {
			_ = proc.Signal(os.Interrupt)
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Strategy 3: SIGKILL if still alive
	if screen.PID > 0 {
		if isProcessAlive(screen.PID) {
			proc, _ := os.FindProcess(screen.PID)
			if proc != nil {
				_ = proc.Kill()
			}
		}
	}

	sm.mu.Lock()
	delete(sm.screens, sessionID)
	sm.mu.Unlock()
	sm.saveScreens()

	return true, nil
}

// GetScreen returns a screen session by session ID.
func (sm *ScreenManager) GetScreen(sessionID string) (*ScreenSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.screens[sessionID]
	return s, ok
}

// GetScreens returns all screen sessions.
func (sm *ScreenManager) GetScreens() []*ScreenSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]*ScreenSession, 0, len(sm.screens))
	for _, s := range sm.screens {
		result = append(result, s)
	}
	return result
}

// GetScreenCount returns the number of tracked screens.
func (sm *ScreenManager) GetScreenCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.screens)
}

// --- Persistence ---

func (sm *ScreenManager) loadScreens() {
	data, err := os.ReadFile(sm.screensFile)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("Failed to load screens", "error", err)
		}
		return
	}
	var screens []*ScreenSession
	if err := json.Unmarshal(data, &screens); err != nil {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range screens {
		sm.screens[s.SessionID] = s
	}
}

func (sm *ScreenManager) saveScreens() {
	sm.mu.RLock()
	screens := make([]*ScreenSession, 0, len(sm.screens))
	for _, s := range sm.screens {
		screens = append(screens, s)
	}
	sm.mu.RUnlock()

	data, err := json.MarshalIndent(screens, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(sm.screensFile), 0o755)
	if err := os.WriteFile(sm.screensFile, data, 0o644); err != nil {
		slog.Warn("Failed to save screens", "error", err)
	}
}

// --- Helpers ---

var validScreenNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func isValidScreenName(name string) bool {
	return len(name) > 0 && len(name) <= 100 && validScreenNamePattern.MatchString(name)
}

func isValidPath(path string) bool {
	return len(path) > 0 && !strings.ContainsAny(path, "\x00\n\r")
}

func shellEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", `\$`)
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var validEnvKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidEnvKey(key string) bool {
	return validEnvKeyPattern.MatchString(key)
}

func buildEnvExports(sessionID, screenName string, providerEnv map[string]string) string {
	parts := []string{
		fmt.Sprintf("export JARVIS_SESSION_ID=%q", sessionID),
		fmt.Sprintf("export JARVIS_SCREEN=1"),
		fmt.Sprintf("export JARVIS_SCREEN_NAME=%q", screenName),
	}
	for k, v := range providerEnv {
		if !isValidEnvKey(k) {
			log.Printf("WARNING: skipping invalid environment variable key: %q", k)
			continue
		}
		parts = append(parts, fmt.Sprintf("export %s=%q", k, v))
	}
	return strings.Join(parts, " && ") + " && "
}

func buildBaseCommand(mode SessionMode) string {
	switch mode {
	case ModeClaude:
		return "claude --dangerously-skip-permissions"
	case ModeShell:
		return "bash"
	default:
		return "bash"
	}
}

func wrapWithNice(cmd string, nc NiceConfig) string {
	if !nc.Enabled {
		return cmd
	}
	return fmt.Sprintf("nice -n %d %s", nc.Priority, cmd)
}

func runShellCommand(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func runShellCommandTimeout(command string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runShellCommand(ctx, command)
}

func getScreenPID(screenName string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Use CombinedOutput and ignore exit code: screen -ls exits 1 when sessions exist, which is normal
	out, _ := exec.CommandContext(ctx, "screen", "-ls").CombinedOutput()

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, screenName) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				parts := strings.SplitN(fields[0], ".", 2)
				if len(parts) > 0 {
					pid, err := strconv.Atoi(parts[0])
					if err == nil {
						return pid, nil
					}
				}
			}
		}
	}
	return 0, fmt.Errorf("screen %s not found in screen -ls output", screenName)
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists without killing it
	return proc.Signal(nil) == nil
}
