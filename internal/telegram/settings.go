package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Settings holds all Telegram integration configuration.
// This is the authoritative store for Telegram config, persisted to telegram-settings.json.
type Settings struct {
	Enabled             bool              `json:"enabled"`
	BotToken            string            `json:"botToken"`
	ChatID              string            `json:"chatId"`
	Mode                string            `json:"mode"` // "polling" or "webhook"
	RateLimitPerMinute  int               `json:"rateLimitPerMinute"`
	ParseMode           string            `json:"parseMode"` // "HTML"
	RequireChatIDVerify bool              `json:"requireChatIdVerification"`
	ForwardOutputToChat bool              `json:"forwardOutputToChat"`
	WebhookSecret       string            `json:"webhookSecret"`
	Notifications       NotificationPrefs `json:"notifications"`
	// R35 · LeadSessionID pins a specific session as "the lead" for
	// the /lead + /ask Telegram commands. Empty = auto-resolve (first
	// strategic-tier agent). Honored via OfficeBridge.GetLeadAgentInfo.
	LeadSessionID string `json:"leadSessionId,omitempty"`
}

// NotificationPrefs controls which event types trigger Telegram notifications.
type NotificationPrefs struct {
	SessionStart     bool `json:"sessionStart"`
	SessionComplete  bool `json:"sessionComplete"`
	SessionError     bool `json:"sessionError"`
	AgentSpawn       bool `json:"agentSpawn"`
	AgentComplete    bool `json:"agentComplete"`
	ProgressUpdates  bool `json:"progressUpdates"`
	ProgressInterval int  `json:"progressIntervalMs"` // milliseconds, default 300000 (5 min)
	StuckState       bool `json:"stuckState"`
	PermissionPrompt bool `json:"permissionPrompt"`
	Elicitation      bool `json:"elicitation"`
	RespawnBlocked   bool `json:"respawnBlocked"`
	PlanApproval     bool `json:"planApproval"`
	// R35 · Push a one-line summary to Telegram when an office standup
	// finalises. Default true (StandupComplete is a high-signal
	// progress beat operators want).
	StandupComplete bool `json:"standupComplete"`
}

// DefaultSettings returns production-safe defaults matching the Node.js implementation.
// All notifications are enabled except progressUpdates.
func DefaultSettings() Settings {
	return Settings{
		Enabled:             false,
		BotToken:            "",
		ChatID:              "",
		Mode:                "polling",
		RateLimitPerMinute:  20,
		ParseMode:           "HTML",
		RequireChatIDVerify: true,
		ForwardOutputToChat: false,
		WebhookSecret:       "",
		Notifications: NotificationPrefs{
			SessionStart:     true,
			SessionComplete:  true,
			SessionError:     true,
			AgentSpawn:       true,
			AgentComplete:    true,
			ProgressUpdates:  false,
			ProgressInterval: 300000, // 5 minutes
			StuckState:       true,
			PermissionPrompt: true,
			Elicitation:      true,
			RespawnBlocked:   true,
			PlanApproval:     true,
			StandupComplete:  true,
		},
	}
}

// SettingsStore provides thread-safe access to Telegram settings with file persistence.
type SettingsStore struct {
	mu       sync.RWMutex
	filePath string
	current  Settings
}

// NewSettingsStore creates a SettingsStore that persists to {dataDir}/telegram-settings.json.
// If the settings file exists on disk it is loaded; otherwise defaults are used.
// When stateSettings is non-nil and the settings file does not yet exist, botToken and
// chatId are migrated from the legacy state store map.
func NewSettingsStore(dataDir string, stateSettings map[string]interface{}) (*SettingsStore, error) {
	fp := filepath.Join(dataDir, "telegram-settings.json")
	store := &SettingsStore{
		filePath: fp,
		current:  DefaultSettings(),
	}

	if _, err := os.Stat(fp); err == nil {
		// File exists -- load it.
		if loadErr := store.Load(); loadErr != nil {
			return nil, fmt.Errorf("loading telegram settings: %w", loadErr)
		}
		return store, nil
	}

	// File does not exist. Migrate from legacy state store if available.
	if stateSettings != nil {
		if token, ok := stateSettings["botToken"].(string); ok && token != "" {
			store.current.BotToken = token
		}
		if chatID, ok := stateSettings["chatId"].(string); ok && chatID != "" {
			store.current.ChatID = chatID
		}
		if store.current.BotToken != "" && store.current.ChatID != "" {
			store.current.Enabled = true
		}
	}

	// Persist the initial settings (defaults or migrated values).
	if err := store.Save(); err != nil {
		return nil, fmt.Errorf("saving initial telegram settings: %w", err)
	}

	return store, nil
}

// Load reads settings from disk. If the file is missing or unreadable, current
// settings are reset to defaults.
func (s *SettingsStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		s.current = DefaultSettings()
		return nil
	}
	// R27 security review · Tighten perms on existing settings files
	// written before R27 with 0644. Best-effort — a chmod failure is
	// logged but non-fatal (the caller may not own the file).
	if info, statErr := os.Stat(s.filePath); statErr == nil {
		if info.Mode().Perm() != 0o600 {
			_ = os.Chmod(s.filePath, 0o600)
		}
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		s.current = DefaultSettings()
		return fmt.Errorf("parsing telegram settings JSON: %w", err)
	}

	s.current = loaded
	return nil
}

// Save writes the current settings to disk atomically (write tmp, then rename).
func (s *SettingsStore) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.current, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshalling telegram settings: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}

	tmpPath := s.filePath + ".tmp"
	// R27 security review · Telegram settings carry the bot token,
	// which is a credential. 0600 so only the Jarvis process owner
	// can read it — matches the agent-token / auth.json pattern.
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("writing telegram settings tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		// Clean up the tmp file on rename failure.
		os.Remove(tmpPath)
		return fmt.Errorf("renaming telegram settings tmp file: %w", err)
	}

	return nil
}

// Get returns a copy of the current settings. Safe for concurrent use.
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update applies a mutation function to the settings under an exclusive lock,
// then persists the result to disk.
func (s *SettingsStore) Update(fn func(*Settings)) error {
	s.mu.Lock()
	fn(&s.current)
	s.mu.Unlock()

	return s.Save()
}
