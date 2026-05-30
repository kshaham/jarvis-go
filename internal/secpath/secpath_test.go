package secpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUnderRoot_SymlinkEscapeBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a sibling dir OUTSIDE root
	// Plant a symlink inside root that points outside it (as an agent could).
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	// A lexically-in-root path that traverses the symlink to a file outside root
	// must be rejected (the bug: lexical checks alone passed it through).
	if _, err := ResolveUnderRoot(root, "escape/secret.txt"); !errors.Is(err, ErrPathTraversal) {
		t.Errorf("symlink escape not blocked: err=%v want ErrPathTraversal", err)
	}
	// A normal in-root (non-symlink) path still resolves fine.
	if _, err := ResolveUnderRoot(root, "sub/ok.txt"); err != nil {
		t.Errorf("in-root path wrongly rejected: %v", err)
	}
}

func TestResolveUnderRoot_HappyPath(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveUnderRoot(root, "sub/file.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "sub", "file.txt")
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestResolveUnderRoot_EmptyRelResolvesToRoot(t *testing.T) {
	// Edge case: empty relative path. filepath.Join(root, "") returns
	// the cleaned root, and we accept that as "the root itself."
	root := t.TempDir()
	got, err := ResolveUnderRoot(root, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantRoot, _ := filepath.Abs(root)
	if got != wantRoot {
		t.Errorf("got=%q want=%q", got, wantRoot)
	}
}

func TestResolveUnderRoot_AbsoluteRelRejected(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveUnderRoot(root, "/etc/passwd")
	if !errors.Is(err, ErrPathAbsolute) {
		t.Errorf("expected ErrPathAbsolute, got %v", err)
	}
}

func TestResolveUnderRoot_TraversalRejected(t *testing.T) {
	root := t.TempDir()
	// Climb out of the tempdir's nearest parent.
	_, err := ResolveUnderRoot(root, "../../../etc/passwd")
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got %v", err)
	}
}

func TestResolveUnderRoot_DotDotInsideStillAllowed(t *testing.T) {
	// "sub/../file" cleans to "file" which is inside root. Should
	// resolve happily — this is a normal-shape input.
	root := t.TempDir()
	got, err := ResolveUnderRoot(root, "sub/../file.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "file.txt")
	if got != want {
		t.Errorf("got=%q want=%q (sub/.. should clean to root)", got, want)
	}
}

func TestResolveUnderRoot_SiblingRootRejected(t *testing.T) {
	// Catch the classic prefix-match-false-positive: /tmp/foo vs
	// /tmp/foobar. Constructing a path that resolves into a sibling
	// must NOT be accepted just because the prefix matches.
	root := t.TempDir()
	parent := filepath.Dir(root)
	siblingName := filepath.Base(root) + "-sibling"
	siblingAbs := filepath.Join(parent, siblingName)
	// rel = "../<base>-sibling" climbs to parent then into sibling.
	_, err := ResolveUnderRoot(root, filepath.Join("..", siblingName))
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal for sibling escape (target=%q), got %v",
			siblingAbs, err)
	}
}

func TestResolveUnderRoot_NestedSubdir(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveUnderRoot(root, "a/b/c/d.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "a", "b", "c", "d.txt")
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
