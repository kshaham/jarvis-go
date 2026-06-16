package office

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		in       string
		redacted bool
	}{
		{"deploy with key sk-abcdefghijklmnopqrstuvwxyz123", true},
		{"aws id AKIAIOSFODNN7EXAMPLE here", true},
		{"github ghp_abcdefghijklmnopqrstuvwxyz0123456789 token", true},
		{"just a normal task title", false},
	}
	for _, tc := range cases {
		got := RedactSecrets(tc.in)
		if tc.redacted && got == tc.in {
			t.Errorf("expected %q to be redacted, unchanged", tc.in)
		}
		if !tc.redacted && got != tc.in {
			t.Errorf("expected %q unchanged, got %q", tc.in, got)
		}
	}
}

// TestRedactSecrets_ModernOpenAIKeys verifies the regex catches OpenAI's
// project/service-account/admin key shapes (introduced 2024), not just the
// legacy alnum-only form -- so a modern sk-proj- key can't leak through.
func TestRedactSecrets_ModernOpenAIKeys(t *testing.T) {
	modern := []string{
		"key sk-proj-abcdefghijklmnopqrstuvwxyz0123 here",
		"key sk-svcacct-abcdefghijklmnopqrstuvwxyz0123 here",
		"key sk-admin-abcdefghijklmnopqrstuvwxyz0123 here",
	}
	for _, in := range modern {
		if got := RedactSecrets(in); got == in {
			t.Errorf("expected modern OpenAI key redacted, unchanged: %q", in)
		}
	}
	// And scanForSecrets must flag a committed modern key.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.env"),
		[]byte("OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz0123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := scanForSecrets(dir); len(findings) != 1 {
		t.Errorf("expected 1 finding for a committed sk-proj- key, got %d", len(findings))
	}
}

// TestRedactSecrets_NoFalsePositiveOnHyphenatedNames guards against the
// over-broad `sk-[alnum][alnum_-]{19,}` pattern: ordinary hyphenated names that
// happen to start with "sk-" must NOT be treated as secrets, because a single
// committed match halts the whole replenishment loop.
func TestRedactSecrets_NoFalsePositiveOnHyphenatedNames(t *testing.T) {
	legit := []string{
		"depend on sk-learn-package-dependencies-012 today",
		"branch sk-service-key-rotation-plan here",
		"file sk-migration-v1-data-pipeline.md",
	}
	for _, in := range legit {
		if got := RedactSecrets(in); got != in {
			t.Errorf("legit hyphenated name was wrongly redacted: %q -> %q", in, got)
		}
	}
	// And a committed file full of such names must NOT halt the loop.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deps.txt"),
		[]byte("sk-learn-package-dependencies-012\nsk-service-key-rotation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := scanForSecrets(dir); len(findings) != 0 {
		t.Errorf("expected NO findings for hyphenated non-secret names, got %d: %+v", len(findings), findings)
	}
}

func TestScanForSecrets_FindsAndSkips(t *testing.T) {
	dir := t.TempDir()
	// A committed secret.
	if err := os.WriteFile(filepath.Join(dir, "config.txt"),
		[]byte("token = ghp_abcdefghijklmnopqrstuvwxyz0123456789\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A clean file.
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret inside .git must be skipped.
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "leak"),
		[]byte("AKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := scanForSecrets(dir)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding (config.txt), got %d: %+v", len(findings), findings)
	}
	if findings[0].Path != "config.txt" {
		t.Errorf("expected finding in config.txt, got %s", findings[0].Path)
	}
}

func TestScanForSecrets_EmptyDir(t *testing.T) {
	if f := scanForSecrets(t.TempDir()); len(f) != 0 {
		t.Errorf("expected no findings in clean dir, got %+v", f)
	}
	if f := scanForSecrets(""); f != nil {
		t.Errorf("expected nil for empty working dir, got %+v", f)
	}
}

// TestSecurityScan_ClearsImmediatelyAfterSecretRemoved verifies the cooldown
// does NOT keep replenishment latched once the operator removes the secret:
// while blocked, a re-scan runs every tick (bypassing the cooldown).
func TestSecurityScan_ClearsImmediatelyAfterSecretRemoved(t *testing.T) {
	workDir := t.TempDir()
	secret := filepath.Join(workDir, "leak.env")
	if err := os.WriteFile(secret, []byte("OPENAI_KEY=sk-abcdefghijklmnopqrstuvwxyz0123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", workDir)

	if !m.maybeRunSecurityScan() {
		t.Fatal("expected blocked while the secret is present")
	}
	// Remove the secret; the next scan must run despite the 15m cooldown
	// (because we're currently blocked) and clear the block.
	if err := os.Remove(secret); err != nil {
		t.Fatal(err)
	}
	if m.maybeRunSecurityScan() {
		t.Error("expected NOT blocked after the secret is removed (cooldown must not latch)")
	}
}

// TestSecurityScan_HaltsReplenishment verifies a committed secret in the working
// tree blocks the perpetual replenishment loop.
func TestSecurityScan_HaltsReplenishment(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // escalation log
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "leak.env"),
		[]byte("OPENAI_KEY=sk-abcdefghijklmnopqrstuvwxyz0123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := idleTestConfig()
	m, store, rec := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)
	m.SetSpawnContext("tmpl", workDir)
	m.RegisterAgent("sess-sec", "sec", &TeamMemberConfig{ID: "sec", Name: "Sec", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()

	if len(store.writesFor("sess-sec")) != 0 {
		t.Error("expected NO replenishment while a committed secret is present")
	}
	if !rec.has("office:securityIssueDetected") {
		t.Error("expected office:securityIssueDetected broadcast")
	}
}
