package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWrapUpReportsDir_NoHome covers the UserHomeDir error → "".
func TestWrapUpReportsDir_NoHome(t *testing.T) {
	withNoHome(t)
	if d := WrapUpReportsDir(); d != "" {
		t.Errorf("expected empty reports dir with no HOME, got %q", d)
	}
}

// TestGenerateWrapUpReport_NoHome covers the dir=="" error branch.
func TestGenerateWrapUpReport_NoHome(t *testing.T) {
	m := newTestManager()
	withNoHome(t)
	if _, err := GenerateWrapUpReport(m, "manual", ""); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestGenerateWrapUpReport_MkdirFails makes the reports parent a file so
// MkdirAll fails.
func TestGenerateWrapUpReport_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.jarvis/office-memory a file so MkdirAll(.../reports) fails.
	mem := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mem, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	if _, err := GenerateWrapUpReport(m, "manual", ""); err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

// TestGenerateAndBroadcastWrapUpReport_ErrorLogged drives the error path
// (generation fails) of the broadcast wrapper.
func TestGenerateAndBroadcastWrapUpReport_ErrorLogged(t *testing.T) {
	m := newTestManager()
	called := false
	withNoHome(t)
	GenerateAndBroadcastWrapUpReport(m, "manual", "x", func(string, interface{}) { called = true })
	if called {
		t.Error("broadcast should not fire when generation fails")
	}
}

// TestListWrapUpReports_NoHome covers the dir=="" → nil,nil branch.
func TestListWrapUpReports_NoHome(t *testing.T) {
	withNoHome(t)
	out, err := ListWrapUpReports()
	if err != nil || out != nil {
		t.Errorf("expected nil,nil with no HOME, got %v,%v", out, err)
	}
}

// TestListWrapUpReports_ReadDirError makes the reports path a file so
// os.ReadDir returns a non-NotExist error.
func TestListWrapUpReports_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	// reports is a FILE → ReadDir returns ENOTDIR (not IsNotExist).
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListWrapUpReports(); err == nil {
		t.Fatal("expected ReadDir error when reports path is a file")
	}
}

// TestListWrapUpReports_SkipsDirsAndInvalid verifies subdirs and
// non-matching filenames are skipped, and unparseable reports are
// skipped without aborting.
func TestListWrapUpReports_SkipsEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A subdir (skipped), an invalid name (skipped), and a valid report.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "random.md"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	valid := "wrap-up-20260101-120000.md"
	if err := os.WriteFile(filepath.Join(dir, valid), []byte("# Team Wrap-Up Report\n**Trigger:** manual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ListWrapUpReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 1 || out[0].Filename != valid {
		t.Fatalf("expected only the valid report, got %v", out)
	}
}

// TestLoadWrapUpReport_InvalidName covers the ValidateReportFilename guard.
func TestLoadWrapUpReport_InvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadWrapUpReport("../../etc/passwd"); err == nil {
		t.Fatal("expected error for invalid filename")
	}
}

// TestLoadWrapUpReport_NoHome covers the dir=="" branch.
func TestLoadWrapUpReport_NoHome(t *testing.T) {
	withNoHome(t)
	if _, err := LoadWrapUpReport("wrap-up-20260101-120000.md"); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestLoadWrapUpReport_Success round-trips a real report body.
func TestLoadWrapUpReport_Success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "wrap-up-20260101-120000.md"
	body := "# Team Wrap-Up Report — 2026-01-01 12:00:00 UTC\nbody text"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWrapUpReport(name)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != body {
		t.Errorf("body mismatch: %q", got)
	}
}

// TestReadReportMeta_StatError covers the os.Stat error branch.
func TestReadReportMeta_StatError(t *testing.T) {
	dir := t.TempDir()
	if _, err := readReportMeta(dir, "wrap-up-20260101-120000.md"); err == nil {
		t.Fatal("expected stat error for missing file")
	}
}

// TestReadReportMeta_ParsesHeaders verifies Trigger/Reason/Mission header
// parsing and the filename-timestamp path.
func TestReadReportMeta_ParsesHeaders(t *testing.T) {
	dir := t.TempDir()
	name := "wrap-up-20260101-120000.md"
	body := "# Team Wrap-Up Report — 2026-01-01 12:00:00 UTC\n" +
		"**Trigger:** wrap-up\n**Reason:** sprint over\n**Mission:** ship v2\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := readReportMeta(dir, name)
	if err != nil {
		t.Fatalf("readReportMeta: %v", err)
	}
	if meta.Trigger != "wrap-up" || meta.Reason != "sprint over" || meta.TeamMission != "ship v2" {
		t.Errorf("header parse mismatch: %+v", meta)
	}
	if meta.GeneratedAt.IsZero() {
		t.Error("expected GeneratedAt from filename")
	}
}

// TestReadReportMeta_BadFilenameFallsBackToModTime drives the
// parseFilenameTimestamp-error → ModTime fallback. We feed a name that
// passes readReportMeta (it doesn't validate) but fails the timestamp
// parse.
func TestReadReportMeta_ModTimeFallback(t *testing.T) {
	dir := t.TempDir()
	name := "not-a-wrap-up-name.md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := readReportMeta(dir, name)
	if err != nil {
		t.Fatalf("readReportMeta: %v", err)
	}
	if meta.GeneratedAt.IsZero() {
		t.Error("expected ModTime fallback to populate GeneratedAt")
	}
}

// TestParseFilenameTimestamp_Bad covers both the prefix/suffix guard and
// the parse-error path.
func TestParseFilenameTimestamp_Bad(t *testing.T) {
	if _, err := parseFilenameTimestamp("notmatching.txt"); err == nil {
		t.Error("expected error for non-matching name")
	}
	if _, err := parseFilenameTimestamp("wrap-up-XXXXXXXX-XXXXXX.md"); err == nil {
		t.Error("expected parse error for non-numeric timestamp")
	}
}

// TestPruneOldReports_ReadDirError covers the ReadDir error branch.
func TestPruneOldReports_ReadDirError(t *testing.T) {
	if err := pruneOldReports(filepath.Join(t.TempDir(), "does-not-exist"), 5); err == nil {
		t.Fatal("expected ReadDir error for missing dir")
	}
}

// TestPruneOldReports_DropsOldest verifies the keep-N logic drops the
// oldest reports and keeps the newest.
func TestPruneOldReports_DropsOldest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"wrap-up-20260101-000000.md",
		"wrap-up-20260102-000000.md",
		"wrap-up-20260103-000000.md",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also a subdir + non-matching file to exercise the skip branches.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneOldReports(dir, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	// Only the newest report should remain.
	if _, err := os.Stat(filepath.Join(dir, names[2])); err != nil {
		t.Errorf("newest report should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, names[0])); !os.IsNotExist(err) {
		t.Error("oldest report should be pruned")
	}
}

// TestGenerateWrapUpReport_PrunesOnGenerate creates more than the keep
// count of reports and verifies generation prunes them (exercising the
// pruneOldReports success path inside GenerateWrapUpReport).
func TestGenerateWrapUpReport_Prunes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed many old valid reports.
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < WrapUpReportsMaxKeep+3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		n := "wrap-up-" + ts.Format("20060102-150405") + ".md"
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestManager()
	if _, err := GenerateWrapUpReport(m, "manual", ""); err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := ListWrapUpReports()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > WrapUpReportsMaxKeep {
		t.Errorf("expected pruning to <= %d, got %d", WrapUpReportsMaxKeep, len(out))
	}
}

// TestLoadWrapUpReport_EscapeRejected verifies a symlink whose target
// escapes the reports dir is rejected. We create a valid-named symlink
// inside the dir pointing outside it.
func TestLoadWrapUpReport_EscapeRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real file OUTSIDE the reports dir.
	outside := filepath.Join(home, "secret.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "wrap-up-20260101-120000.md"
	link := filepath.Join(dir, name)
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := LoadWrapUpReport(name)
	if err == nil || !strings.Contains(err.Error(), "escaped") {
		t.Fatalf("expected escape rejection, got %v", err)
	}
}
