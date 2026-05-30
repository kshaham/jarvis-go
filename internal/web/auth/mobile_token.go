package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// mobileTokenBytes controls the length of the raw mobile token. 32 bytes
// = 64 hex chars, same entropy as the agent token but with per-device
// scoping and operator-revocable lifetime.
const mobileTokenBytes = 32

// defaultMobileTokenTTL is the lifetime applied to newly issued tokens
// when the caller does not specify one. 90 days strikes a balance:
// long enough that operators rarely re-log-in, short enough that a
// leaked token doesn't grant indefinite access.
const defaultMobileTokenTTL = 90 * 24 * time.Hour

// MobileTokenEntry is the persisted shape of a single device's token.
type MobileTokenEntry struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	TokenHash  string `json:"tokenHash"` // SHA-256 hex; raw token only returned at issue time
	CreatedAt  int64  `json:"createdAt"` // unix ms
	LastUsedAt int64  `json:"lastUsedAt,omitempty"`
	ExpiresAt  int64  `json:"expiresAt"` // unix ms; 0 = never
}

// PublicMobileTokenEntry is the operator-visible view (no token, no hash).
type PublicMobileTokenEntry struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt,omitempty"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// IssuedMobileToken is what login returns to the client.
type IssuedMobileToken struct {
	Token     string `json:"token"`
	DeviceID  string `json:"deviceId"`
	ExpiresAt int64  `json:"expiresAt"`
}

// MobileTokenManager stores per-device mobile-app tokens, persisted to
// configDir/mobile-tokens.json. Each entry tracks the SHA-256 hash of
// the raw token (so a snapshot of the on-disk file is not usable as a
// credential), plus operator-visible metadata for the Settings UI.
//
// Operations are safe for concurrent use.
type MobileTokenManager struct {
	path    string
	mu      sync.RWMutex
	entries map[string]*MobileTokenEntry // deviceID -> entry
	// lastUsedPersist (unix ms) debounces persisting LastUsedAt bumps so a
	// synchronous disk write doesn't run under the write lock on every
	// authenticated request (the auth hot path). See Validate.
	lastUsedPersist int64
}

// NewMobileTokenManager loads any existing mobile-token store from disk
// or starts with an empty store. It does not block on I/O failure --
// a missing or malformed file means "no devices registered yet".
func NewMobileTokenManager(configDir string) *MobileTokenManager {
	m := &MobileTokenManager{
		path:    filepath.Join(configDir, "mobile-tokens.json"),
		entries: make(map[string]*MobileTokenEntry),
	}
	m.load()
	return m
}

func (m *MobileTokenManager) load() {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		slog.Error("Failed to create config dir for mobile tokens", "error", err)
		return
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("Failed to read mobile tokens file", "error", err)
		}
		return
	}
	var entries []*MobileTokenEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("Mobile tokens file is malformed; starting with empty store", "error", err)
		return
	}
	for _, e := range entries {
		if e == nil || e.DeviceID == "" || e.TokenHash == "" {
			continue
		}
		m.entries[e.DeviceID] = e
	}
	slog.Info("Loaded mobile tokens", "count", len(m.entries))
}

// persist writes the current entries map to disk atomically. Caller
// must hold m.mu (write lock).
func (m *MobileTokenManager) persist() error {
	list := make([]*MobileTokenEntry, 0, len(m.entries))
	for _, e := range m.entries {
		list = append(list, e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mobile tokens: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write mobile tokens: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename mobile tokens: %w", err)
	}
	return nil
}

// Issue mints a new token for the given device. deviceName is a
// human-readable label shown in the operator's Settings list (e.g.
// "My iPhone"). If ttl is zero, defaultMobileTokenTTL is used.
// Returns the raw token exactly once -- callers must store it
// immediately; only the hash is persisted.
func (m *MobileTokenManager) Issue(deviceName string, ttl time.Duration) (IssuedMobileToken, error) {
	if deviceName == "" {
		deviceName = "Unnamed device"
	}
	if ttl <= 0 {
		ttl = defaultMobileTokenTTL
	}

	rawToken := make([]byte, mobileTokenBytes)
	if _, err := rand.Read(rawToken); err != nil {
		return IssuedMobileToken{}, fmt.Errorf("generate mobile token: %w", err)
	}
	token := hex.EncodeToString(rawToken)

	rawDeviceID := make([]byte, 8)
	if _, err := rand.Read(rawDeviceID); err != nil {
		return IssuedMobileToken{}, fmt.Errorf("generate device id: %w", err)
	}
	deviceID := hex.EncodeToString(rawDeviceID)

	now := time.Now()
	expires := now.Add(ttl)
	entry := &MobileTokenEntry{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		TokenHash:  hashMobileToken(token),
		CreatedAt:  now.UnixMilli(),
		ExpiresAt:  expires.UnixMilli(),
	}

	m.mu.Lock()
	m.entries[deviceID] = entry
	err := m.persist()
	m.mu.Unlock()
	if err != nil {
		slog.Error("Failed to persist mobile token", "error", err, "deviceId", deviceID)
		// Leave the in-memory entry valid for this run; persistence
		// failure is non-fatal for the current request.
	}

	slog.Info("Issued mobile token", "deviceId", deviceID, "deviceName", deviceName, "expiresAt", expires)
	return IssuedMobileToken{
		Token:     token,
		DeviceID:  deviceID,
		ExpiresAt: entry.ExpiresAt,
	}, nil
}

// Validate checks the presented raw token against the store. On a hit
// it bumps LastUsedAt (best-effort) and returns the entry's deviceID.
// Returns ("", false) for missing, expired, or unknown tokens.
func (m *MobileTokenManager) Validate(presented string) (string, bool) {
	if presented == "" {
		return "", false
	}
	presentedHash := hashMobileToken(presented)
	now := time.Now().UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	for deviceID, entry := range m.entries {
		// Constant-time comparison guards against hash-prefix timing
		// attacks even though the input is already hashed.
		if subtle.ConstantTimeCompare([]byte(entry.TokenHash), []byte(presentedHash)) != 1 {
			continue
		}
		if entry.ExpiresAt > 0 && entry.ExpiresAt < now {
			// Lazily drop expired entries so the store doesn't bloat.
			delete(m.entries, deviceID)
			_ = m.persist()
			return "", false
		}
		entry.LastUsedAt = now
		// Debounce the persist: LastUsedAt is best-effort metadata. Writing it
		// to disk on EVERY authenticated request would serialize all mobile-token
		// validation behind a synchronous disk write under the write lock. The
		// in-memory value is always current; the on-disk "last seen" lags by at
		// most a minute.
		if now-m.lastUsedPersist >= 60_000 {
			m.lastUsedPersist = now
			_ = m.persist() // best-effort
		}
		return deviceID, true
	}
	return "", false
}

// Revoke removes the token for the given deviceID. Returns true if a
// token was actually removed.
func (m *MobileTokenManager) Revoke(deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[deviceID]; !ok {
		return false
	}
	delete(m.entries, deviceID)
	if err := m.persist(); err != nil {
		slog.Error("Failed to persist mobile token revocation", "error", err, "deviceId", deviceID)
	}
	slog.Info("Revoked mobile token", "deviceId", deviceID)
	return true
}

// RevokeByToken finds the entry matching the presented raw token and
// removes it. Used by /api/auth/mobile/logout where the client only
// knows its own token, not the deviceID.
func (m *MobileTokenManager) RevokeByToken(presented string) bool {
	if presented == "" {
		return false
	}
	presentedHash := hashMobileToken(presented)
	m.mu.Lock()
	defer m.mu.Unlock()
	for deviceID, entry := range m.entries {
		if subtle.ConstantTimeCompare([]byte(entry.TokenHash), []byte(presentedHash)) == 1 {
			delete(m.entries, deviceID)
			if err := m.persist(); err != nil {
				slog.Error("Failed to persist mobile token revocation", "error", err)
			}
			return true
		}
	}
	return false
}

// List returns the operator-visible view of every registered device.
// Tokens and hashes are intentionally omitted.
func (m *MobileTokenManager) List() []PublicMobileTokenEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PublicMobileTokenEntry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, PublicMobileTokenEntry{
			DeviceID:   e.DeviceID,
			DeviceName: e.DeviceName,
			CreatedAt:  e.CreatedAt,
			LastUsedAt: e.LastUsedAt,
			ExpiresAt:  e.ExpiresAt,
		})
	}
	return out
}

// hashMobileToken returns the lowercase hex SHA-256 of a raw token.
// Centralised so issue + validate share the exact same encoding.
func hashMobileToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
