package office

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRepoInput(t *testing.T) {
	cases := []struct {
		in, wantURL, wantSlug, wantName string
		wantErr                         bool
	}{
		{"owner/repo", "https://github.com/owner/repo", "owner-repo", "owner/repo", false},
		{"https://github.com/owner/repo", "https://github.com/owner/repo", "owner-repo", "owner/repo", false},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo", "owner-repo", "owner/repo", false},
		{"owner/repo.with.dots", "https://github.com/owner/repo.with.dots", "owner-repo-with-dots", "owner/repo.with.dots", false},
		{"../evil", "", "", "", true},
		{"owner/repo; rm -rf /", "", "", "", true},
		{"https://gitlab.com/o/r", "", "", "", true},
		{"", "", "", "", true},
	}
	for _, c := range cases {
		url, slug, name, err := normalizeRepoInput(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if url != c.wantURL || slug != c.wantSlug || name != c.wantName {
			t.Errorf("%q: got (%q,%q,%q) want (%q,%q,%q)", c.in, url, slug, name, c.wantURL, c.wantSlug, c.wantName)
		}
	}
}

func TestPrepareGitHubProjectCloneAndPull(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	var calls [][]string
	run := func(_ context.Context, dir string, args ...string) error {
		calls = append(calls, append([]string{dir}, args...))
		// Simulate clone by creating the target dir.
		if len(args) > 0 && args[0] == "clone" {
			return os.MkdirAll(args[len(args)-1], 0o755)
		}
		return nil
	}

	// First call: dir absent -> clone.
	dir, url, name, err := PrepareGitHubProject("owner/repo", "", run, nil)
	if err != nil {
		t.Fatalf("clone err: %v", err)
	}
	if dir != filepath.Join(root, "owner-repo") || url != "https://github.com/owner/repo" || name != "owner/repo" {
		t.Fatalf("unexpected result: %q %q %q", dir, url, name)
	}
	if calls[0][1] != "clone" {
		t.Fatalf("expected first git op to be clone, got %v", calls[0])
	}

	// Second call: dir now present -> pull.
	_, _, _, err = PrepareGitHubProject("owner/repo", "", run, nil)
	if err != nil {
		t.Fatalf("pull err: %v", err)
	}
	// calls[1] == [target, "-C", target, "pull", "--ff-only"]
	if calls[1][1] != "-C" || calls[1][3] != "pull" {
		t.Fatalf("expected second git op to be a pull, got %v", calls[1])
	}
}

func TestNormalizeRepoInputExported(t *testing.T) {
	// The exported wrapper must delegate to normalizeRepoInput unchanged.
	url, slug, name, err := NormalizeRepoInputExported("owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/owner/repo" || slug != "owner-repo" || name != "owner/repo" {
		t.Fatalf("got (%q,%q,%q)", url, slug, name)
	}
	if _, _, _, err := NormalizeRepoInputExported("../evil"); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestDefaultGitRunner(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	// Success path: a real, harmless git command in a temp dir.
	if err := DefaultGitRunner(context.Background(), dir, "init"); err != nil {
		t.Fatalf("git init should succeed: %v", err)
	}
	// Error path: an invalid subcommand surfaces a wrapped, scrubbed error.
	err := DefaultGitRunner(context.Background(), dir, "no-such-subcmd")
	if err == nil {
		t.Fatal("expected error for invalid git subcommand")
	}
	if !strings.Contains(err.Error(), "git no-such-subcmd failed") {
		t.Fatalf("expected wrapped error, got %q", err.Error())
	}
}

func TestPrepareGitHubProjectInvalidInput(t *testing.T) {
	// Invalid identifier must fail before any git op (run is never called).
	called := false
	run := func(_ context.Context, _ string, _ ...string) error { called = true; return nil }
	_, _, _, err := PrepareGitHubProject("../evil", "", run, nil)
	if !errors.Is(err, ErrInvalidRepo) {
		t.Fatalf("expected ErrInvalidRepo, got %v", err)
	}
	if called {
		t.Fatal("git runner should not run for invalid input")
	}
}

func TestPrepareGitHubProjectNilRunnerUsesDefault(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	// Pre-create the (non-repo) target so the pull path runs through the
	// real DefaultGitRunner when run == nil. git pull fails fast (not a repo),
	// with no network access required.
	if err := os.MkdirAll(filepath.Join(root, "owner-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := PrepareGitHubProject("owner/repo", "", nil, nil)
	if err == nil {
		t.Fatal("expected pull to fail on a non-repo dir")
	}
}

func TestPrepareGitHubProjectPullFailure(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	// Pre-create the target dir so PrepareGitHubProject takes the pull path.
	if err := os.MkdirAll(filepath.Join(root, "owner-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(_ context.Context, _ string, _ ...string) error {
		return errors.New("boom")
	}
	_, _, _, err := PrepareGitHubProject("owner/repo", "", run, nil)
	if err == nil || !strings.Contains(err.Error(), "git pull failed for owner/repo") {
		t.Fatalf("expected pull failure wrap, got %v", err)
	}
}

func TestPrepareGitHubProjectCloneFailureHint(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	run := func(_ context.Context, _ string, _ ...string) error {
		return errors.New("auth required")
	}
	// token == "" => the error must carry the private-repo hint.
	_, _, _, err := PrepareGitHubProject("owner/repo", "", run, nil)
	if err == nil || !strings.Contains(err.Error(), "private repos need a GitHub token") {
		t.Fatalf("expected clone failure with token hint, got %v", err)
	}
}

func TestPrepareGitHubProjectBroadcastsOnClone(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	run := func(_ context.Context, _ string, args ...string) error {
		if len(args) > 0 && args[0] == "clone" {
			return os.MkdirAll(args[len(args)-1], 0o755)
		}
		return nil
	}
	var gotEvent string
	var gotData map[string]interface{}
	broadcast := func(event string, data interface{}) {
		gotEvent = event
		if m, ok := data.(map[string]interface{}); ok {
			gotData = m
		}
	}
	dir, _, _, err := PrepareGitHubProject("owner/repo", "", run, broadcast)
	if err != nil {
		t.Fatalf("clone err: %v", err)
	}
	if gotEvent != "office:projectCreated" {
		t.Fatalf("expected projectCreated broadcast, got %q", gotEvent)
	}
	if gotData["path"] != dir || gotData["repo"] != "owner/repo" {
		t.Fatalf("unexpected broadcast payload: %v", gotData)
	}
}

func TestPrepareGitHubProjectTokenInClone(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()
	var cloneURL string
	run := func(_ context.Context, dir string, args ...string) error {
		if args[0] == "clone" {
			cloneURL = args[len(args)-2] // ...clone, --depth, 1, <url>, <dir>
			return os.MkdirAll(args[len(args)-1], 0o755)
		}
		return nil
	}
	_, _, _, err := PrepareGitHubProject("owner/repo", "ghp_secrettoken", run, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cloneURL, "x-access-token:ghp_secrettoken@github.com") {
		t.Fatalf("expected token in clone URL, got %q", cloneURL)
	}
}
