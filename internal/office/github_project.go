package office

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kshaham/jarvis/internal/logscrub"
)

// GitHub-project flow. Sibling to new_project.go: where PrepareNewProject
// scaffolds an empty /app/workspace/<slug>, PrepareGitHubProject clones an
// existing repo into /app/workspace/<owner>-<repo> (or git-pulls it if the
// dir already exists) and hands the resolved path to the spawn flow.
//
// Security: the repo identifier is validated against a strict regex before
// it ever reaches a path or a git argument, git is invoked via exec with
// discrete args (never a shell string), and the auth token is injected into
// the clone URL only — never logged.

// repoSegmentRe constrains each of owner / repo. Letters, digits,
// underscore, hyphen, dot — but a segment may not be "." or ".." and may
// not start with "-" (which git could read as a flag). 1-100 chars.
var repoSegmentRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)

// ErrInvalidRepo is returned when the repo identifier fails validation.
// The handler maps it to 400.
var ErrInvalidRepo = errors.New("repo must be owner/repo or a github.com URL (letters, digits, _, -, . only)")

// slugUnsafe matches any run of characters not allowed in an on-disk
// directory slug; such runs collapse to a single hyphen.
var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func validSegment(s string) bool {
	if s == "." || s == ".." || strings.HasPrefix(s, "-") {
		return false
	}
	return repoSegmentRe.MatchString(s)
}

// normalizeRepoInput accepts "owner/repo" shorthand or a full
// https://github.com/owner/repo[.git] URL and returns the canonical
// https URL, the on-disk slug "owner-repo", and the display name
// "owner/repo". Anything else is ErrInvalidRepo.
func normalizeRepoInput(in string) (repoURL, slug, name string, err error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", "", "", ErrInvalidRepo
	}
	s := in
	for _, p := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
			break
		}
	}
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) != 2 || !validSegment(parts[0]) || !validSegment(parts[1]) {
		return "", "", "", ErrInvalidRepo
	}
	owner, repo := parts[0], parts[1]
	repoURL = "https://github.com/" + owner + "/" + repo
	slug = slugUnsafe.ReplaceAllString(owner, "-") + "-" + slugUnsafe.ReplaceAllString(repo, "-")
	name = owner + "/" + repo
	return repoURL, slug, name, nil
}

// NormalizeRepoInputExported exposes normalizeRepoInput to the handler so
// auto-save can record the canonical URL + name without re-cloning.
func NormalizeRepoInputExported(in string) (repoURL, slug, name string, err error) {
	return normalizeRepoInput(in)
}

// GitRunner runs a git invocation. The default shells out via exec;
// tests inject a fake. dir is the working directory for -C style
// commands (empty for clone). args are the git subcommand + flags.
type GitRunner func(ctx context.Context, dir string, args ...string) error

// DefaultGitRunner runs the real git binary with discrete args (no shell).
func DefaultGitRunner(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Never include the args (the clone URL may carry a token) in the
		// error; surface only git's output, scrubbed.
		return fmt.Errorf("git %s failed: %s", args[0], logscrub.All(string(out)))
	}
	return nil
}

// PrepareGitHubProject validates the repo identifier, clones it into
// NewProjectRoot/<owner>-<repo> (or git-pulls if already present), and
// returns the resolved dir, canonical repo URL, and display name. The
// token (optional) enables private repos and is injected into the clone
// URL only. broadcast (optional) fires office:projectCreated on first
// clone so other browsers refresh.
func PrepareGitHubProject(repoInput, token string, run GitRunner, broadcast SSEBroadcaster) (dir, repoURL, name string, err error) {
	repoURL, slug, name, err := normalizeRepoInput(repoInput)
	if err != nil {
		return "", "", "", err
	}
	if run == nil {
		run = DefaultGitRunner
	}
	target := filepath.Join(NewProjectRoot, slug)
	ctx := context.Background()

	if _, statErr := os.Stat(target); statErr == nil {
		// Already cloned: refresh.
		if pullErr := run(ctx, target, "-C", target, "pull", "--ff-only"); pullErr != nil {
			return "", "", "", fmt.Errorf("git pull failed for %s: %w", name, pullErr)
		}
		return target, repoURL, name, nil
	}

	cloneURL := repoURL + ".git"
	if token != "" {
		cloneURL = "https://x-access-token:" + token + "@github.com/" + strings.TrimPrefix(repoURL, "https://github.com/") + ".git"
	}
	if cloneErr := run(ctx, "", "clone", "--depth", "1", cloneURL, target); cloneErr != nil {
		hint := ""
		if token == "" {
			hint = " (private repos need a GitHub token in Settings)"
		}
		return "", "", "", fmt.Errorf("git clone failed for %s%s: %w", name, hint, cloneErr)
	}
	if broadcast != nil {
		broadcast("office:projectCreated", map[string]interface{}{"path": target, "repo": name})
	}
	return target, repoURL, name, nil
}
