package office

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectNameRe_AcceptsValidSlugs(t *testing.T) {
	for _, name := range []string{"abc", "ABC123", "with-dash", "with_underscore", "a", strings.Repeat("a", 64)} {
		if !ProjectNameRe.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
}

func TestProjectNameRe_RejectsInvalidSlugs(t *testing.T) {
	for _, name := range []string{
		"",                       // empty
		"has space",              // space
		"with.dot",               // dot
		"with/slash",             // slash
		"../escape",              // traversal
		"weird@char",             // special char
		strings.Repeat("a", 65),  // too long
	} {
		if ProjectNameRe.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestPrepareNewProject_InvalidNameReturnsErrInvalidProjectName(t *testing.T) {
	_, err := PrepareNewProject("../escape", "mission", nil)
	if !errors.Is(err, ErrInvalidProjectName) {
		t.Errorf("expected ErrInvalidProjectName, got %v", err)
	}
}

// To exercise the mkdir path without writing under /app/workspace,
// we temporarily redirect NewProjectRoot via package-level variable
// substitution. Since NewProjectRoot is a const, we instead test via
// a temp HOME and verify the relative behaviour: a path that maps
// under a writable temp dir. For the const-rooted path we can't
// easily unit-test the happy path without root write, so the test
// guards against an environment where /app/workspace doesn't exist
// and is unwritable (the common laptop dev case).
//
// Skip the on-disk happy-path test when NewProjectRoot is not
// writable; production / docker images mount it as the project
// root and the test runs cleanly there.

func TestPrepareNewProject_HappyPathWhenRootWritable(t *testing.T) {
	// Skip when /app/workspace isn't writable on the host (laptop
	// dev, CI without the bind mount).
	if _, err := os.Stat(NewProjectRoot); err != nil {
		t.Skipf("NewProjectRoot %s not present; skipping happy-path test", NewProjectRoot)
	}
	test, _ := os.Create(filepath.Join(NewProjectRoot, ".write-probe"))
	if test != nil {
		_ = test.Close()
		_ = os.Remove(filepath.Join(NewProjectRoot, ".write-probe"))
	} else {
		t.Skip("NewProjectRoot not writable; skipping happy-path test")
	}

	// Use a unique slug so the test is rerunnable without cleanup.
	slug := "test-prep-" + strings.ReplaceAll(t.Name(), "/", "-")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(NewProjectRoot, slug)) })

	var sseEvent string
	broadcast := func(event string, _ interface{}) {
		sseEvent = event
	}

	target, err := PrepareNewProject(slug, "mission for the test", broadcast)
	if err != nil {
		t.Fatalf("PrepareNewProject: %v", err)
	}
	if target != filepath.Join(NewProjectRoot, slug) {
		t.Errorf("target=%q want %q", target, filepath.Join(NewProjectRoot, slug))
	}
	if sseEvent != "office:projectCreated" {
		t.Errorf("expected SSE office:projectCreated, got %q", sseEvent)
	}

	// Re-running with the same slug should NOT emit the SSE event
	// (directory already exists).
	sseEvent = ""
	_, err = PrepareNewProject(slug, "mission", broadcast)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if sseEvent != "" {
		t.Errorf("re-creation should not broadcast, got %q", sseEvent)
	}
}

func TestPrepareNewProject_NilBroadcastNoCrash(t *testing.T) {
	// Documents the contract: passing nil broadcast is valid (test
	// path / orchestrator without an SSE broker). Validation should
	// still fire first so we don't even reach the broadcast branch.
	_, err := PrepareNewProject("../escape", "mission", nil)
	if !errors.Is(err, ErrInvalidProjectName) {
		t.Errorf("expected ErrInvalidProjectName, got %v", err)
	}
}
