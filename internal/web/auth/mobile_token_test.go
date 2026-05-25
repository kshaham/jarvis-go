package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMobileTokenManager_IssueAndValidate(t *testing.T) {
	dir := t.TempDir()
	m := NewMobileTokenManager(dir)

	issued, err := m.Issue("Test Phone", 0)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if issued.Token == "" || len(issued.Token) != mobileTokenBytes*2 {
		t.Fatalf("Issue returned suspicious token length %d", len(issued.Token))
	}
	if issued.DeviceID == "" {
		t.Fatal("Issue returned empty deviceId")
	}
	if issued.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("ExpiresAt should be in the future, got %d", issued.ExpiresAt)
	}

	deviceID, ok := m.Validate(issued.Token)
	if !ok {
		t.Fatal("Validate returned false for freshly issued token")
	}
	if deviceID != issued.DeviceID {
		t.Errorf("Validate returned deviceID %q, want %q", deviceID, issued.DeviceID)
	}
}

func TestMobileTokenManager_ValidateRejectsUnknown(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	if _, ok := m.Validate("not-a-real-token"); ok {
		t.Error("Validate accepted an unknown token")
	}
	if _, ok := m.Validate(""); ok {
		t.Error("Validate accepted an empty token")
	}
}

func TestMobileTokenManager_ValidateRejectsExpired(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	issued, err := m.Issue("Old Phone", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	// Sleep past the TTL.
	time.Sleep(2 * time.Millisecond)

	if _, ok := m.Validate(issued.Token); ok {
		t.Error("Validate accepted an expired token")
	}
	// Expired entries should also be evicted from List.
	if got := m.List(); len(got) != 0 {
		t.Errorf("expected expired entry to be evicted, got %d entries", len(got))
	}
}

func TestMobileTokenManager_RevokeAndRevokeByToken(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	a, _ := m.Issue("Phone A", 0)
	b, _ := m.Issue("Phone B", 0)

	if !m.Revoke(a.DeviceID) {
		t.Error("Revoke returned false for known device")
	}
	if m.Revoke(a.DeviceID) {
		t.Error("Revoke returned true for already-revoked device")
	}
	if _, ok := m.Validate(a.Token); ok {
		t.Error("revoked token still validates")
	}

	if !m.RevokeByToken(b.Token) {
		t.Error("RevokeByToken returned false for known token")
	}
	if m.RevokeByToken(b.Token) {
		t.Error("RevokeByToken returned true on second call")
	}
	if _, ok := m.Validate(b.Token); ok {
		t.Error("revoked-by-token still validates")
	}
}

func TestMobileTokenManager_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	m1 := NewMobileTokenManager(dir)
	issued, err := m1.Issue("Reload Phone", 0)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Fresh manager pointing at the same dir should see the issued token.
	m2 := NewMobileTokenManager(dir)
	deviceID, ok := m2.Validate(issued.Token)
	if !ok {
		t.Fatal("reloaded manager does not recognise the token")
	}
	if deviceID != issued.DeviceID {
		t.Errorf("reloaded manager returned deviceID %q, want %q", deviceID, issued.DeviceID)
	}

	// The store on disk must not contain the raw token.
	data, err := os.ReadFile(filepath.Join(dir, "mobile-tokens.json"))
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if got := string(data); contains(got, issued.Token) {
		t.Error("raw token leaked into mobile-tokens.json")
	}

	// And the persisted shape is valid JSON of MobileTokenEntry slices.
	var entries []MobileTokenEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("persisted file is not valid JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(entries))
	}
	if entries[0].DeviceName != "Reload Phone" {
		t.Errorf("DeviceName=%q, want %q", entries[0].DeviceName, "Reload Phone")
	}
	if entries[0].TokenHash == "" {
		t.Error("TokenHash should be non-empty on disk")
	}
}

func TestMobileTokenManager_ListOmitsSecrets(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	m.Issue("Alpha", 0)
	m.Issue("Beta", 0)

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	// The public view has no Token or TokenHash field by construction;
	// confirm by JSON-roundtripping and checking the keys.
	data, _ := json.Marshal(list[0])
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, leaked := raw["token"]; leaked {
		t.Error("List leaked token field")
	}
	if _, leaked := raw["tokenHash"]; leaked {
		t.Error("List leaked tokenHash field")
	}
}

func TestMobileTokenManager_DefaultTTL(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	before := time.Now().UnixMilli()
	issued, err := m.Issue("Default TTL Phone", 0)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	expected := before + defaultMobileTokenTTL.Milliseconds()
	// Allow a small drift window between the test's clock reading and
	// the manager's internal time.Now() call.
	if issued.ExpiresAt < expected-5_000 || issued.ExpiresAt > expected+60_000 {
		t.Errorf("ExpiresAt=%d, want near %d", issued.ExpiresAt, expected)
	}
}

func TestMobileTokenManager_DeviceNameFallback(t *testing.T) {
	m := NewMobileTokenManager(t.TempDir())
	issued, err := m.Issue("", 0)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	list := m.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].DeviceID != issued.DeviceID {
		t.Errorf("DeviceID mismatch")
	}
	if list[0].DeviceName == "" {
		t.Error("blank deviceName should fall back to a placeholder, not stay empty")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
