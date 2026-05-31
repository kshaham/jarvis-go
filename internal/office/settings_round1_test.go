package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpdateOfficeSettings_AllFields drives every recognised setting field
// through UpdateOfficeSettings and asserts the config picked up the new
// values. Each numeric setting also has a floor; values below the floor are
// ignored (exercised by the separate sub-threshold test below).
func TestUpdateOfficeSettings_AllFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	m.UpdateOfficeSettings(map[string]interface{}{
		"brainstormCooldownMs":        float64(120000),
		"brainstormResponseTimeoutMs": float64(45000),
		"idleThresholdMs":             float64(60000),
		"meetingInactivityTimeoutMs":  float64(20000),
		"standupResponseTimeoutMs":    float64(40000),
		"fileLockExpiryMs":            float64(90000),
		"curiosityEnabled":            true,
		"curiosityCooldownMs":         float64(600000),
		"reflectionResponseTimeoutMs": float64(20000),
		"teamMission":                 "win the quarter",
	})

	cfg := m.GetConfig()
	if cfg.BrainstormCooldown != 120000*time.Millisecond {
		t.Errorf("brainstorm cooldown not applied: %v", cfg.BrainstormCooldown)
	}
	if cfg.BrainstormTimeout != 45000*time.Millisecond {
		t.Errorf("brainstorm timeout not applied: %v", cfg.BrainstormTimeout)
	}
	if cfg.IdleThreshold != 60000*time.Millisecond {
		t.Errorf("idle threshold not applied: %v", cfg.IdleThreshold)
	}
	if cfg.MeetingInactivityTimeout != 20000*time.Millisecond {
		t.Errorf("meeting inactivity timeout not applied: %v", cfg.MeetingInactivityTimeout)
	}
	if cfg.StandupResponseTimeout != 40000*time.Millisecond {
		t.Errorf("standup timeout not applied: %v", cfg.StandupResponseTimeout)
	}
	if cfg.FileLockTTL != 90000*time.Millisecond {
		t.Errorf("file lock TTL not applied: %v", cfg.FileLockTTL)
	}
	if cfg.CuriosityCooldown != 600000*time.Millisecond {
		t.Errorf("curiosity cooldown not applied: %v", cfg.CuriosityCooldown)
	}
	if cfg.ReflectionTimeout != 20000*time.Millisecond {
		t.Errorf("reflection timeout not applied: %v", cfg.ReflectionTimeout)
	}
	if !m.IsCuriosityEnabled() {
		t.Error("curiosity should be enabled")
	}
	if m.GetTeamMission() != "win the quarter" {
		t.Errorf("team mission not applied: %q", m.GetTeamMission())
	}
}

// TestUpdateOfficeSettings_SubThresholdIgnored verifies values below the
// per-field floor are rejected (config keeps its prior value).
func TestUpdateOfficeSettings_SubThresholdIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	before := m.GetConfig().BrainstormCooldown

	m.UpdateOfficeSettings(map[string]interface{}{
		"brainstormCooldownMs": float64(1000), // < 60000 floor -> ignored
		"idleThresholdMs":      float64(100),  // < 30000 floor -> ignored
		"curiosityEnabled":     false,
	})

	if m.GetConfig().BrainstormCooldown != before {
		t.Errorf("sub-threshold cooldown should have been ignored, got %v", m.GetConfig().BrainstormCooldown)
	}
	if m.IsCuriosityEnabled() {
		t.Error("curiosity should be disabled")
	}
}

// TestUpdateOfficeSettings_EmptyMapNoOp ensures an empty/typeless map is a safe
// no-op (no panic, config unchanged).
func TestUpdateOfficeSettings_EmptyMapNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	before := *m.GetConfig()
	m.UpdateOfficeSettings(map[string]interface{}{
		"unknownKey":           "ignored",
		"brainstormCooldownMs": "not-a-float", // wrong type -> ignored
	})
	if m.GetConfig().BrainstormCooldown != before.BrainstormCooldown {
		t.Error("config should be unchanged for unknown/mistyped settings")
	}
}

// TestLoadDevInstances_MigratesLegacyFlatFiles seeds the legacy
// team-dev-{url,pid,creds}.txt files, then constructs a fresh manager and
// asserts they were migrated into the "app" JSON slot and the flat files
// removed.
func TestLoadDevInstances_MigratesLegacyFlatFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team-dev-url.txt"), []byte("http://jarvis:5000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team-dev-pid.txt"), []byte("4242\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team-dev-creds.txt"), []byte("u:p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestManager() // constructor calls loadPersistedMission -> loadDevInstances

	inst, ok := m.GetDevInstance("app")
	if !ok {
		t.Fatal("expected legacy files migrated into app slot")
	}
	if inst.URL != "http://jarvis:5000" || inst.PID != 4242 || inst.Credentials != "u:p" {
		t.Errorf("migrated instance mismatch: %+v", inst)
	}
	// Legacy flat files must be removed after migration.
	for _, f := range []string{"team-dev-url.txt", "team-dev-pid.txt", "team-dev-creds.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected legacy file %s removed after migration", f)
		}
	}
}

// TestUltrareviewResult_HasBlockers covers both the blocker-present and
// no-blocker branches.
func TestUltrareviewResult_HasBlockers(t *testing.T) {
	withBlocker := &UltrareviewResult{Findings: []UltrareviewFinding{
		{Severity: "nit"}, {Severity: "blocker"},
	}}
	if !withBlocker.HasBlockers() {
		t.Error("expected HasBlockers true when a blocker is present")
	}
	noBlocker := &UltrareviewResult{Findings: []UltrareviewFinding{
		{Severity: "warning"}, {Severity: "nit"},
	}}
	if noBlocker.HasBlockers() {
		t.Error("expected HasBlockers false with no blocker finding")
	}
	empty := &UltrareviewResult{}
	if empty.HasBlockers() {
		t.Error("expected HasBlockers false for empty findings")
	}
}

// TestPortFromURL covers a port-bearing URL, a portless URL, and a malformed
// input.
func TestPortFromURL(t *testing.T) {
	if got := portFromURL("http://jarvis:3000/path"); got != "3000" {
		t.Errorf("expected port 3000, got %q", got)
	}
	if got := portFromURL("http://jarvis/path"); got != "" {
		t.Errorf("expected empty port for portless URL, got %q", got)
	}
	if got := portFromURL("://bad url"); got != "" {
		t.Errorf("expected empty for malformed URL, got %q", got)
	}
}

// TestTruncateCodegraphLog truncates oversized output, leaves short output.
func TestTruncateCodegraphLog(t *testing.T) {
	short := "small codegraph output"
	if got := truncateCodegraphLog(short); got != short {
		t.Errorf("expected short output unchanged, got %q", got)
	}
	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'z'
	}
	got := truncateCodegraphLog(string(long))
	if len(got) >= 3000 {
		t.Errorf("expected truncated length, got %d", len(got))
	}
}
