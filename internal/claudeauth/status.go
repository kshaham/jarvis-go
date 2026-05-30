// Package claudeauth inspects the Claude Code CLI's credential
// store (~/.claude/.credentials.json) and reports the current
// authentication state in a shape the dashboard and any future
// consumers (CLI, mobile API) can render directly.
//
// Why a dedicated package: the auth file is owned by the Claude
// Code CLI (not jarvis), so the parsing logic is fragile to CLI
// changes and worth centralising. Jarvis also performs a
// symlink-restoration side effect — when Claude Code atomically
// replaces the symlink jarvis installed with a regular file, this
// package re-establishes the symlink so the credentials survive
// container restarts (the symlink target lives on the bind-mounted
// volume).
package claudeauth

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Status describes the result of a credential-store check. JSON
// tags match what the dashboard expects on /api/claude-auth-status.
type Status struct {
	Authenticated    bool   `json:"authenticated"`
	Method           string `json:"method,omitempty"`           // "subscription" | "api_key"
	SubscriptionType string `json:"subscriptionType,omitempty"`
	// ExpiresAt is the OAuth expiry as a UnixMilli value. Encoded
	// as interface{} so we can pass through whatever JSON type the
	// upstream provided (float64 in practice) without forcing a
	// shape choice on the consumer.
	ExpiresAt interface{} `json:"expiresAt,omitempty"`
	// Reason is a machine-readable code when Authenticated is false.
	// See ReasonXxx constants below.
	Reason string `json:"reason,omitempty"`
}

// Reason codes returned in Status.Reason when Authenticated is
// false. Stable strings — the dashboard switches on these.
const (
	ReasonNoCredentials      = "no_credentials"
	ReasonEmptyCredentials   = "empty_credentials"
	ReasonInvalidCredentials = "invalid_credentials"
	ReasonTokenExpired       = "token_expired"
	ReasonNoValidToken       = "no_valid_token"
)

// nowMilli is the UnixMilli source. Swappable so tests can drive
// the expiry check without messing with the system clock.
var nowMilli = func() int64 {
	return timeNow().UnixMilli()
}

// timeNow is the indirection for the actual time source. Defined
// separately from nowMilli so tests can override one without
// touching the other.
var timeNow = defaultNow

// CheckStatus reads the Claude Code credential store and returns
// the current auth state. Side effect: when the file at
// ~/.claude/.credentials.json is a regular file (Claude Code
// replaced jarvis's symlink with an atomic write), this function
// restores the symlink pointing at ~/.claude-auth/.credentials.json
// so the credentials survive container restarts.
//
// Never panics; never returns an error — every failure mode
// surfaces as a Status with Authenticated=false + a Reason code.
func CheckStatus() Status {
	homeDir, _ := os.UserHomeDir()
	credPath := filepath.Join(homeDir, ".claude", ".credentials.json")
	symlinkTarget := filepath.Join(homeDir, ".claude-auth", ".credentials.json")

	pathToRead := pickCredentialPath(credPath, symlinkTarget)
	if pathToRead == "" {
		return Status{Authenticated: false, Reason: ReasonNoCredentials}
	}

	data, err := os.ReadFile(pathToRead)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return Status{Authenticated: false, Reason: ReasonEmptyCredentials}
	}

	// Side effect: restore the symlink if Claude Code overwrote it.
	if err := maybeRestoreSymlink(credPath, symlinkTarget); err != nil {
		slog.Debug("claudeauth: symlink restore failed (non-fatal)", "err", err)
	}

	var creds map[string]interface{}
	if err := json.Unmarshal(data, &creds); err != nil {
		return Status{Authenticated: false, Reason: ReasonInvalidCredentials}
	}

	if s := checkOAuth(creds, "claudeAiOauth", "accessToken"); s != nil {
		return *s
	}
	if s := checkOAuth(creds, "oauthAccount", "accessToken", "token"); s != nil {
		return *s
	}
	if apiKey, _ := creds["apiKey"].(string); apiKey != "" {
		return Status{Authenticated: true, Method: "api_key"}
	}
	return Status{Authenticated: false, Reason: ReasonNoValidToken}
}

// pickCredentialPath returns the first existing file from the
// (canonical, symlink-target) pair, or "" when neither exists.
func pickCredentialPath(canonical, symlinkTarget string) string {
	if fileExists(canonical) {
		return canonical
	}
	if fileExists(symlinkTarget) {
		return symlinkTarget
	}
	return ""
}

// maybeRestoreSymlink replaces a regular file at credPath with a
// symlink pointing at symlinkTarget (after copying the file's
// contents there). No-op when credPath is already a symlink or
// missing. Used because Claude Code's atomic-write pattern can
// overwrite the symlink jarvis installs at container start.
func maybeRestoreSymlink(credPath, symlinkTarget string) error {
	info, err := os.Lstat(credPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil // already a symlink
	}
	if err := os.MkdirAll(filepath.Dir(symlinkTarget), 0o755); err != nil {
		return err
	}
	if err := copyFile(credPath, symlinkTarget); err != nil {
		return err
	}
	// Atomically replace credPath with the symlink: create it at a temp name
	// and rename it over credPath. The previous Remove-then-Symlink left a
	// window where, if Symlink failed, credPath was gone and no symlink existed
	// (the canonical credentials file silently missing until the next restore).
	tmpLink := credPath + ".symlink.tmp"
	_ = os.Remove(tmpLink) // clear any stale temp from a prior crash
	if err := os.Symlink(symlinkTarget, tmpLink); err != nil {
		return err
	}
	if err := os.Rename(tmpLink, credPath); err != nil {
		_ = os.Remove(tmpLink)
		return err
	}
	slog.Info("Persisted Claude credentials to volume and restored symlink")
	return nil
}

// checkOAuth pulls an OAuth subdoc out of creds (under key) and
// reports a Status when it carries a non-empty access token in any
// of the tokenKeys positions. Returns nil when the subdoc is
// missing or all tokenKeys yield empty strings.
//
// Same shape used for both claudeAiOauth (key="accessToken") and
// oauthAccount (keys="accessToken","token") — the variadic keys
// avoid two near-duplicate helpers.
func checkOAuth(creds map[string]interface{}, subKey string, tokenKeys ...string) *Status {
	sub, ok := creds[subKey].(map[string]interface{})
	if !ok {
		return nil
	}
	hasToken := false
	for _, tk := range tokenKeys {
		if v, _ := sub[tk].(string); v != "" {
			hasToken = true
			break
		}
	}
	if !hasToken {
		return nil
	}
	if expiresAt, ok := sub["expiresAt"].(float64); ok && nowMilli() > int64(expiresAt) {
		return &Status{
			Authenticated: false,
			Reason:        ReasonTokenExpired,
			ExpiresAt:     expiresAt,
		}
	}
	subType, _ := sub["subscriptionType"].(string)
	if subType == "" {
		subType = "unknown"
	}
	return &Status{
		Authenticated:    true,
		Method:           "subscription",
		SubscriptionType: subType,
		ExpiresAt:        sub["expiresAt"],
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
