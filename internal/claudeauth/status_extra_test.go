package claudeauth

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMaybeRestoreSymlink_ReplacesRegularFile covers the main restore path:
// a regular file is copied to the volume target and replaced by a symlink.
func TestMaybeRestoreSymlink_ReplacesRegularFile(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")
	target := filepath.Join(dir, "volume", "credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"k":"v"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := maybeRestoreSymlink(credPath, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fi, err := os.Lstat(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected credPath to be a symlink after restore")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != `{"k":"v"}` {
		t.Fatalf("expected contents copied to target, got %q (%v)", data, err)
	}
}

// TestMaybeRestoreSymlink_AlreadySymlinkNoOp covers the no-op branch.
func TestMaybeRestoreSymlink_AlreadySymlinkNoOp(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "wherever"), credPath); err != nil {
		t.Fatal(err)
	}
	if err := maybeRestoreSymlink(credPath, filepath.Join(dir, "target")); err != nil {
		t.Fatalf("expected no-op nil, got %v", err)
	}
}

// TestMaybeRestoreSymlink_MissingPathErrors covers the Lstat error branch.
func TestMaybeRestoreSymlink_MissingPathErrors(t *testing.T) {
	dir := t.TempDir()
	if err := maybeRestoreSymlink(filepath.Join(dir, "nope"), filepath.Join(dir, "t")); err == nil {
		t.Fatal("expected error for missing cred path")
	}
}

// TestCopyFile covers both the success and the read-error branches.
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copy success: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "hello" {
		t.Fatalf("unexpected dst contents: %q", b)
	}
	if err := copyFile(filepath.Join(dir, "missing"), dst); err == nil {
		t.Fatal("expected read error for missing src")
	}
}

// TestCheckOAuth_NoTokenReturnsNil covers the empty-token branch.
func TestCheckOAuth_NoTokenReturnsNil(t *testing.T) {
	creds := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{"accessToken": ""},
	}
	if got := checkOAuth(creds, "claudeAiOauth", "accessToken"); got != nil {
		t.Fatalf("expected nil for empty token, got %#v", got)
	}
	// Missing subdoc also returns nil.
	if got := checkOAuth(creds, "absent", "accessToken"); got != nil {
		t.Fatalf("expected nil for missing subdoc, got %#v", got)
	}
}
