package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost         = 12
	sessionTokenBytes  = 32
	secretKeyBytes     = 64
	maxPasswordLength  = 128
	sessionTimeoutDur  = 24 * time.Hour
	cookieName         = "jarvis_session"

	rateLimitTier1Attempts = 5
	rateLimitTier1Block    = 5 * time.Minute
	rateLimitTier2Attempts = 10
	rateLimitTier2Block    = 30 * time.Minute
)

// authConfig is the JSON structure stored in auth.json.
type authConfig struct {
	PasswordHash string `json:"passwordHash"`
}

// SessionEntry tracks an active session.
type SessionEntry struct {
	Token        string
	CreatedAt    time.Time
	LastActivity time.Time
}

// RateLimitEntry tracks failed login attempts per IP.
type RateLimitEntry struct {
	FailedAttempts int
	BlockedUntil   time.Time
}

// Manager handles password authentication, session management, and rate limiting.
type Manager struct {
	configDir          string
	authConfigPath     string
	secretKeyPath      string
	signingSecret      []byte
	sessions           map[string]*SessionEntry
	rateLimits         map[string]*RateLimitEntry
	setupInProgress    bool
	passwordConfigured int32 // 0=unknown, 1=yes, -1=no (atomic cache)
	mu                 sync.RWMutex
}

// NewManager creates a new auth manager. configDir is typically ~/.jarvis.
// The Secure flag on session cookies is decided per-request based on whether
// the request arrived over TLS -- see BuildSessionCookie.
func NewManager(configDir string, _ bool) *Manager {
	m := &Manager{
		configDir:      configDir,
		authConfigPath: filepath.Join(configDir, "auth.json"),
		secretKeyPath:  filepath.Join(configDir, "auth-secret.key"),
		sessions:       make(map[string]*SessionEntry),
		rateLimits:     make(map[string]*RateLimitEntry),
	}
	m.loadOrCreateSecret()

	// Start periodic cleanup of expired sessions and rate limits.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.CleanupExpiredRateLimits()
			m.cleanupExpiredSessions()
		}
	}()

	return m
}

// cleanupExpiredSessions removes sessions that have been inactive beyond the timeout.
func (m *Manager) cleanupExpiredSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for token, session := range m.sessions {
		if now.Sub(session.LastActivity) > sessionTimeoutDur {
			delete(m.sessions, token)
		}
	}
}

// loadOrCreateSecret reads the HMAC signing secret from disk, or generates a new one.
func (m *Manager) loadOrCreateSecret() {
	if err := os.MkdirAll(m.configDir, 0700); err != nil {
		slog.Error("Failed to create config dir", "path", m.configDir, "error", err)
		return
	}

	data, err := os.ReadFile(m.secretKeyPath)
	if err == nil {
		decoded, decErr := hex.DecodeString(strings.TrimSpace(string(data)))
		if decErr == nil && len(decoded) == secretKeyBytes {
			m.signingSecret = decoded
			slog.Info("Loaded existing auth signing secret")
			return
		}
		slog.Error("Failed to decode auth secret, generating new one", "error", decErr)
	}

	secret := make([]byte, secretKeyBytes)
	if _, err := rand.Read(secret); err != nil {
		slog.Error("Failed to generate signing secret", "error", err)
		return
	}
	m.signingSecret = secret

	if err := os.WriteFile(m.secretKeyPath, []byte(hex.EncodeToString(secret)), 0600); err != nil {
		slog.Error("Failed to persist signing secret", "error", err)
	}
	slog.Info("Generated new auth signing secret")
}

// SigningKey returns the HMAC signing secret for use by other handlers that
// need to verify signed payloads (e.g. the hook-event HMAC check). Never
// log or return this over the API.
func (m *Manager) SigningKey() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.signingSecret
}

// IsPasswordConfigured returns true if a password hash exists in auth.json.
// Result is cached to avoid disk I/O on every authenticated request.
func (m *Manager) IsPasswordConfigured() bool {
	cached := atomic.LoadInt32(&m.passwordConfigured)
	if cached != 0 {
		return cached > 0
	}
	data, err := os.ReadFile(m.authConfigPath)
	if err != nil {
		atomic.StoreInt32(&m.passwordConfigured, -1)
		return false
	}
	var cfg authConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		atomic.StoreInt32(&m.passwordConfigured, -1)
		return false
	}
	if cfg.PasswordHash != "" {
		atomic.StoreInt32(&m.passwordConfigured, 1)
		return true
	}
	atomic.StoreInt32(&m.passwordConfigured, -1)
	return false
}

// invalidatePasswordCache resets the cached IsPasswordConfigured result.
func (m *Manager) invalidatePasswordCache() {
	atomic.StoreInt32(&m.passwordConfigured, 0)
}

// ValidatePasswordInput checks that a password meets length requirements.
// Returns a human-readable error string, or "" if valid.
func (m *Manager) ValidatePasswordInput(password string) string {
	if len(password) < 8 {
		return "Password must be at least 8 characters"
	}
	if len(password) > maxPasswordLength {
		return fmt.Sprintf("Password must be at most %d characters", maxPasswordLength)
	}
	return ""
}

// SetPassword hashes the given password with bcrypt and saves it to auth.json.
func (m *Manager) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("bcrypt hash failed: %w", err)
	}
	cfg := authConfig{PasswordHash: string(hash)}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth config: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := m.authConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write auth config: %w", err)
	}
	if err := os.Rename(tmpPath, m.authConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename auth config: %w", err)
	}
	m.invalidatePasswordCache()
	slog.Info("Password configured")
	return nil
}

// VerifyPassword compares the given password against the stored bcrypt hash.
func (m *Manager) VerifyPassword(password string) (bool, error) {
	data, err := os.ReadFile(m.authConfigPath)
	if err != nil {
		return false, nil
	}
	var cfg authConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse auth config: %w", err)
	}
	err = bcrypt.CompareHashAndPassword([]byte(cfg.PasswordHash), []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("bcrypt compare: %w", err)
	}
	return true, nil
}

// ChangePassword verifies the current password, then sets the new one.
// On success, all existing sessions are invalidated.
func (m *Manager) ChangePassword(currentPassword, newPassword string) (bool, error) {
	valid, err := m.VerifyPassword(currentPassword)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}
	if err := m.SetPassword(newPassword); err != nil {
		return false, err
	}
	m.mu.Lock()
	m.sessions = make(map[string]*SessionEntry)
	m.mu.Unlock()
	return true, nil
}

// AcquireSetupLock atomically acquires the setup lock. Returns false if already held.
func (m *Manager) AcquireSetupLock() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setupInProgress {
		return false
	}
	m.setupInProgress = true
	return true
}

// ReleaseSetupLock releases the setup lock.
func (m *Manager) ReleaseSetupLock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setupInProgress = false
}

// CreateSession generates a new session token and stores it.
// Returns the raw token (to be signed into a cookie).
func (m *Manager) CreateSession() string {
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		slog.Error("Failed to generate session token", "error", err)
		return ""
	}
	token := hex.EncodeToString(tokenBytes)
	now := time.Now()

	m.mu.Lock()
	m.sessions[token] = &SessionEntry{
		Token:        token,
		CreatedAt:    now,
		LastActivity: now,
	}
	m.mu.Unlock()

	slog.Info("Created new session")
	return token
}

// ValidateSession checks whether the token corresponds to a valid, non-expired session.
func (m *Manager) ValidateSession(token string) bool {
	m.mu.Lock()
	session, ok := m.sessions[token]
	if !ok {
		m.mu.Unlock()
		return false
	}
	if time.Since(session.LastActivity) > sessionTimeoutDur {
		delete(m.sessions, token)
		m.mu.Unlock()
		slog.Info("Session expired due to inactivity")
		return false
	}
	session.LastActivity = time.Now()
	m.mu.Unlock()
	return true
}

// DestroySession removes the given session token.
func (m *Manager) DestroySession(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
	slog.Info("Session destroyed")
}

// signToken creates an HMAC-SHA256 signature for the given token.
func (m *Manager) signToken(token string) string {
	mac := hmac.New(sha256.New, m.signingSecret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// isRequestSecure returns true if the request arrived over TLS. This is
// used to decide whether cookies should carry the Secure flag on a
// per-request basis (the server may be running both HTTP and HTTPS
// listeners when --https is enabled).
func isRequestSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	// Honor reverse proxy headers if the server is fronted by one.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	return false
}

// BuildSessionCookie returns an http.Cookie for the given session token.
// The Secure flag is set when either (a) the server is globally configured
// for HTTPS-only AND the request is TLS, or (b) the request is TLS.
// On the parallel HTTP listener, Secure is intentionally omitted so the
// browser will actually send the cookie back.
func (m *Manager) BuildSessionCookie(token string, r *http.Request) *http.Cookie {
	signature := m.signToken(token)
	value := token + "." + signature

	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTimeoutDur.Seconds()),
	}
	if isRequestSecure(r) {
		cookie.Secure = true
	}
	return cookie
}

// BuildClearCookie returns an http.Cookie that clears the session cookie.
func (m *Manager) BuildClearCookie(r *http.Request) *http.Cookie {
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
	if isRequestSecure(r) {
		cookie.Secure = true
	}
	return cookie
}

// ParseSessionCookie extracts and validates the session token from a request.
// Returns the raw token if the HMAC signature is valid, or "" if invalid.
func (m *Manager) ParseSessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	value := cookie.Value
	dotIdx := strings.LastIndex(value, ".")
	if dotIdx == -1 {
		return ""
	}

	token := value[:dotIdx]
	signature := value[dotIdx+1:]

	expectedSignature := m.signToken(token)

	// Constant-time comparison to prevent timing attacks
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		slog.Warn("Invalid session cookie signature (malformed hex)")
		return ""
	}
	expectedBytes, err := hex.DecodeString(expectedSignature)
	if err != nil {
		slog.Warn("Invalid expected signature (malformed hex)")
		return ""
	}
	if len(sigBytes) != len(expectedBytes) {
		slog.Warn("Invalid session cookie signature (length mismatch)")
		return ""
	}
	if subtle.ConstantTimeCompare(sigBytes, expectedBytes) != 1 {
		slog.Warn("Invalid session cookie signature")
		return ""
	}

	return token
}

// ValidateSessionFromRequest combines cookie parsing and session validation.
// Returns true if the request carries a valid, non-expired session.
func (m *Manager) ValidateSessionFromRequest(r *http.Request) bool {
	token := m.ParseSessionCookie(r)
	if token == "" {
		return false
	}
	return m.ValidateSession(token)
}

// IsRateLimited checks whether the given IP is currently rate-limited.
// Returns (blocked, retryAfterMs).
func (m *Manager) IsRateLimited(ip string) (bool, int) {
	m.mu.RLock()
	entry, ok := m.rateLimits[ip]
	m.mu.RUnlock()
	if !ok {
		return false, 0
	}
	remaining := time.Until(entry.BlockedUntil)
	if remaining > 0 {
		return true, int(remaining.Milliseconds())
	}
	return false, 0
}

// RecordFailedAttempt increments the failure counter for the given IP
// and applies escalating lockouts. Returns (blocked, retryAfterMs).
func (m *Manager) RecordFailedAttempt(ip string) (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.rateLimits[ip]
	if !ok {
		entry = &RateLimitEntry{}
		m.rateLimits[ip] = entry
	}
	entry.FailedAttempts++

	var blockDuration time.Duration
	if entry.FailedAttempts >= rateLimitTier2Attempts {
		blockDuration = rateLimitTier2Block
	} else if entry.FailedAttempts >= rateLimitTier1Attempts {
		blockDuration = rateLimitTier1Block
	}

	if blockDuration > 0 {
		entry.BlockedUntil = time.Now().Add(blockDuration)
		slog.Warn("Rate limited IP",
			"ip", ip,
			"failedAttempts", entry.FailedAttempts,
			"blockedForSeconds", int(blockDuration.Seconds()),
		)
		return true, int(blockDuration.Milliseconds())
	}
	return false, 0
}

// ResetRateLimit clears the failure record for the given IP after a successful login.
func (m *Manager) ResetRateLimit(ip string) {
	m.mu.Lock()
	delete(m.rateLimits, ip)
	m.mu.Unlock()
}

// CleanupExpiredRateLimits removes stale rate limit entries.
func (m *Manager) CleanupExpiredRateLimits() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for ip, entry := range m.rateLimits {
		if !entry.BlockedUntil.IsZero() && now.After(entry.BlockedUntil) {
			if entry.FailedAttempts < rateLimitTier2Attempts {
				delete(m.rateLimits, ip)
			} else if now.Sub(entry.BlockedUntil) > rateLimitTier2Block {
				delete(m.rateLimits, ip)
			}
		}
	}
}

// trustedProxyCIDRs lists CIDR ranges whose requests may set the
// X-Forwarded-For / X-Real-IP headers we will trust. Populated via
// SetTrustedProxyCIDRs at server startup from settings.
var (
	trustedProxyMu    sync.RWMutex
	trustedProxyCIDRs []*net.IPNet
)

// SetTrustedProxyCIDRs configures which source IPs are trusted to set
// X-Forwarded-For. Pass an empty slice (default) to ignore the header on
// every request -- the safe choice when no reverse proxy is deployed.
func SetTrustedProxyCIDRs(cidrs []string) {
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			parsed = append(parsed, n)
		} else {
			slog.Warn("Invalid trustedProxyCIDR; ignoring", "cidr", c, "error", err)
		}
	}
	trustedProxyMu.Lock()
	trustedProxyCIDRs = parsed
	trustedProxyMu.Unlock()
}

// remoteAddrIP returns just the IP portion of r.RemoteAddr (strips :port).
func remoteAddrIP(r *http.Request) string {
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// requestFromTrustedProxy reports whether r.RemoteAddr matches one of the
// configured trusted-proxy CIDRs.
func requestFromTrustedProxy(r *http.Request) bool {
	trustedProxyMu.RLock()
	defer trustedProxyMu.RUnlock()
	if len(trustedProxyCIDRs) == 0 {
		return false
	}
	ip := net.ParseIP(remoteAddrIP(r))
	if ip == nil {
		return false
	}
	for _, n := range trustedProxyCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP extracts the client IP from the request.
// X-Forwarded-For / X-Real-IP headers are honored only when the request
// itself arrived from a trusted proxy CIDR. Without that gate, an external
// client could trivially spoof a different source IP per request to bypass
// rate limits.
func ClientIP(r *http.Request) string {
	if requestFromTrustedProxy(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the first IP in the chain (the client the proxy saw).
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	return remoteAddrIP(r)
}
