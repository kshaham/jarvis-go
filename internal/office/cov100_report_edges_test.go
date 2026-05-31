package office

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestListWrapUpReports_SkipsUnparseable drives the readReportMeta error
// continue in ListWrapUpReports (report.go ~168-170). A valid-named
// report file with 0000 permissions makes readReportMeta's os.Open fail
// (EACCES) for the non-root owner, so the entry is skipped while a
// readable sibling report is still returned.
func TestListWrapUpReports_SkipsUnparseable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permission checks")
	}
	t.Setenv("HOME", t.TempDir())
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A readable, parseable report.
	good := filepath.Join(dir, "wrap-up-20260101-010101.md")
	if err := os.WriteFile(good, []byte("# Wrap-up\n**Trigger:** manual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid-named report the owner cannot open (0000 perms).
	bad := filepath.Join(dir, "wrap-up-20260101-020202.md")
	if err := os.WriteFile(bad, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) }) // so TempDir cleanup can remove it

	list, err := ListWrapUpReports()
	if err != nil {
		t.Fatalf("ListWrapUpReports: %v", err)
	}
	// The good report is returned; the unreadable one is skipped.
	var sawGood, sawBad bool
	for _, m := range list {
		if m.Filename == "wrap-up-20260101-010101.md" {
			sawGood = true
		}
		if m.Filename == "wrap-up-20260101-020202.md" {
			sawBad = true
		}
	}
	if !sawGood {
		t.Errorf("expected the readable report in the list, got %+v", list)
	}
	if sawBad {
		t.Errorf("unreadable report should have been skipped, got %+v", list)
	}
}
