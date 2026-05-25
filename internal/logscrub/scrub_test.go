package logscrub

import "testing"

func TestBearer_Redacts(t *testing.T) {
	in := "GET /v1/messages HTTP/1.1\nAuthorization: Bearer sk-ant-abcdef1234567890XYZ\nX-Other: keep\n"
	out := Bearer(in)
	if want := "Bearer [REDACTED]"; !contains(out, want) {
		t.Errorf("expected %q in output, got %q", want, out)
	}
	if contains(out, "abcdef1234567890XYZ") {
		t.Errorf("token leaked: %q", out)
	}
	if !contains(out, "X-Other: keep") {
		t.Error("scrubber dropped non-credential content")
	}
}

func TestBearer_NoMatchPassesThrough(t *testing.T) {
	in := "regular log line with no creds"
	if got := Bearer(in); got != in {
		t.Errorf("clean input mutated: %q", got)
	}
}

func TestBearer_CaseInsensitive(t *testing.T) {
	in := "bearer abcdef1234567890XYZ"
	out := Bearer(in)
	if contains(out, "abcdef1234567890") {
		t.Errorf("lowercase 'bearer' not matched: %q", out)
	}
}

func TestBearer_TooShortNotRedacted(t *testing.T) {
	// "Bearer foo" is a placeholder, not a real token. Leave it alone
	// so doc examples don't get mangled.
	in := "Bearer foo"
	if got := Bearer(in); got != in {
		t.Errorf("short token-like string mutated: %q", got)
	}
}

func TestAll_RedactsAnthropicKey(t *testing.T) {
	in := "ANTHROPIC_API_KEY=sk-ant-abcdef01234567890123 something else"
	out := All(in)
	if contains(out, "abcdef01234567890123") {
		t.Errorf("API key leaked: %q", out)
	}
	// Stronger contract since the env-secret regex landed: the full
	// KEY=VALUE pair gets blanked when the env name signals a secret.
	// The narrower `sk-ant-` regex still runs as a backstop for raw
	// key strings not wrapped in an env assignment.
	if !contains(out, "ANTHROPIC_API_KEY=[REDACTED]") {
		t.Errorf("expected env-pair redaction, got: %q", out)
	}
}

func TestAll_RedactsBareAnthropicKey(t *testing.T) {
	// Without an env-var wrapper, the value-only regex still catches
	// raw sk-ant- strings that show up in stderr / error messages.
	in := "error: invalid token sk-ant-abcdef01234567890123 returned by api"
	out := All(in)
	if contains(out, "abcdef01234567890123") {
		t.Errorf("API key leaked: %q", out)
	}
	if !contains(out, "sk-ant-[REDACTED]") {
		t.Errorf("expected sk-ant placeholder, got: %q", out)
	}
}

func TestAll_CombinesPatterns(t *testing.T) {
	in := "auth=Bearer abcdef1234567890XYZ key=sk-ant-abcdef01234567890123"
	out := All(in)
	if contains(out, "abcdef1234567890XYZ") || contains(out, "abcdef01234567890123") {
		t.Errorf("creds leaked: %q", out)
	}
}

func TestAll_RedactsAgentToken(t *testing.T) {
	in := "env: JARVIS_AGENT_TOKEN=eyJhbGc-abc.123.def_456 ANTHROPIC_BASE_URL=http://x"
	out := All(in)
	if contains(out, "eyJhbGc-abc.123.def_456") {
		t.Errorf("agent token leaked: %q", out)
	}
	if !contains(out, "JARVIS_AGENT_TOKEN=[REDACTED]") {
		t.Errorf("expected redaction placeholder, got: %q", out)
	}
	if !contains(out, "ANTHROPIC_BASE_URL=http://x") {
		t.Error("non-credential env survived (good); want preserved")
	}
}

func TestAll_RedactsGenericSecretEnvVars(t *testing.T) {
	cases := []string{
		"GITHUB_TOKEN=ghp_abc123def456",
		"OPENAI_API_KEY=sk-proj-aBc",
		"DB_PASSWORD=hunter2",
		"AWS_SECRET_ACCESS_KEY=AKIA...real-key",
		"MY_CUSTOM_CREDENTIAL=value",
	}
	for _, line := range cases {
		out := All("prefix " + line + " suffix")
		// The value should not appear verbatim.
		valStart := indexOf(line, "=") + 1
		val := line[valStart:]
		if val == "" {
			t.Fatalf("test bug: empty value in %q", line)
		}
		if contains(out, val) {
			t.Errorf("value leaked in %q → %q", line, out)
		}
		if !contains(out, "[REDACTED]") {
			t.Errorf("no redaction placeholder for %q → %q", line, out)
		}
	}
}

func TestAll_PreservesNonCredentialEnvVars(t *testing.T) {
	// PATH, ANTHROPIC_BASE_URL, LANG — none should match the regex.
	in := "PATH=/usr/local/bin:/usr/bin LANG=en_US.UTF-8 ANTHROPIC_BASE_URL=https://api.anthropic.com"
	out := All(in)
	if out != in {
		t.Errorf("benign env mutated:\n  in:  %q\n  out: %q", in, out)
	}
}

// contains is a tiny helper to avoid pulling strings just for this.
func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
