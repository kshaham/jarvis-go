package pty

import (
	"strings"
	"testing"
)

func TestIsValidScreenName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"jarvis-abc123", true},
		{"jarvis-test", true},
		{"simple", true},
		{"with.dots", true},
		{"with_under", true},
		{"", false},
		{"has space", false},
		{"has\nnewline", false},
		{"has;semicolon", false},
		{"has$dollar", false},
	}
	for _, tc := range tests {
		if got := isValidScreenName(tc.name); got != tc.valid {
			t.Errorf("isValidScreenName(%q) = %v, want %v", tc.name, got, tc.valid)
		}
	}
}

func TestIsValidPath(t *testing.T) {
	tests := []struct {
		path  string
		valid bool
	}{
		{"/home/user/project", true},
		{"/tmp/test", true},
		{"", false},
		{"/path\x00with/null", false},
		{"/path\nwith/newline", false},
	}
	for _, tc := range tests {
		if got := isValidPath(tc.path); got != tc.valid {
			t.Errorf("isValidPath(%q) = %v, want %v", tc.path, got, tc.valid)
		}
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"hello", "hello"},
		{`say "hi"`, `say \"hi\"`},
		{"with $var", `with \$var`},
		{"back`tick", "back\\`tick"},
	}
	for _, tc := range tests {
		if got := shellEscape(tc.input); got != tc.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsValidEnvKey(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"FOO", true},
		{"_BAR", true},
		{"MY_VAR_123", true},
		{"a", true},
		{"_", true},
		{"A_B_C", true},
		{"", false},
		{"123START", false},
		{"FOO BAR", false},
		{"FOO;BAR", false},
		{"FOO=BAR", false},
		{"$(rm -rf /)", false},
		{"FOO\nBAR", false},
		{"key-with-dash", false},
		{"key.with.dot", false},
	}
	for _, tc := range tests {
		if got := isValidEnvKey(tc.key); got != tc.valid {
			t.Errorf("isValidEnvKey(%q) = %v, want %v", tc.key, got, tc.valid)
		}
	}
}

func TestBuildEnvExports(t *testing.T) {
	result := buildEnvExports("sess-1", "screen-1", map[string]string{"FOO": "bar"})
	if !strings.Contains(result, "JARVIS_SESSION_ID") {
		t.Error("missing JARVIS_SESSION_ID")
	}
	if !strings.Contains(result, "JARVIS_SCREEN=1") {
		t.Error("missing JARVIS_SCREEN")
	}
	if !strings.Contains(result, "FOO") {
		t.Error("missing custom env var FOO")
	}
}

func TestBuildEnvExportsRejectsInvalidKeys(t *testing.T) {
	result := buildEnvExports("sess-1", "screen-1", map[string]string{
		"VALID_KEY":    "ok",
		"$(inject)":    "bad",
		"key with spa": "bad",
	})
	if !strings.Contains(result, "VALID_KEY") {
		t.Error("missing valid env var VALID_KEY")
	}
	if strings.Contains(result, "$(inject)") {
		t.Error("should not contain injected key $(inject)")
	}
	if strings.Contains(result, "key with spa") {
		t.Error("should not contain key with spaces")
	}
}

func TestBuildBaseCommand(t *testing.T) {
	if cmd := buildBaseCommand(ModeClaude); !strings.Contains(cmd, "claude") {
		t.Errorf("expected claude command, got %q", cmd)
	}
	if cmd := buildBaseCommand(ModeShell); cmd != "bash" {
		t.Errorf("expected bash, got %q", cmd)
	}
}

func TestWrapWithNice(t *testing.T) {
	cmd := wrapWithNice("claude", NiceConfig{Enabled: true, Priority: 15})
	if !strings.Contains(cmd, "nice -n 15") {
		t.Errorf("expected nice wrapper, got %q", cmd)
	}

	cmd = wrapWithNice("claude", NiceConfig{Enabled: false})
	if strings.Contains(cmd, "nice") {
		t.Errorf("expected no nice wrapper, got %q", cmd)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate(abcdef, 3) = %q", got)
	}
	if got := truncate("ab", 5); got != "ab" {
		t.Errorf("truncate(ab, 5) = %q", got)
	}
}
