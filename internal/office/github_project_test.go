package office

import (
	"context"
	"os"
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
