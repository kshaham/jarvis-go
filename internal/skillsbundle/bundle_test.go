package skillsbundle

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMaterialize(t *testing.T) {
	dest := t.TempDir()
	n, err := Materialize(dest)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected files written > 0, got %d", n)
	}

	protocols := filepath.Join(dest, "office-protocols", "SKILL.md")
	info, err := os.Stat(protocols)
	if err != nil {
		t.Fatalf("stat %q: %v", protocols, err)
	}
	if info.Size() == 0 {
		t.Fatalf("office-protocols SKILL.md is empty")
	}

	// A non-office dev skill must materialize too. Pick whichever the
	// bundle actually reports so the test stays in sync with the bundle.
	names, err := Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	var devSkill string
	for _, candidate := range []string{"go-concurrency", "debug"} {
		if slices.Contains(names, candidate) {
			devSkill = candidate
			break
		}
	}
	if devSkill == "" {
		t.Fatalf("expected a known dev skill in bundle, names=%v", names)
	}
	devPath := filepath.Join(dest, devSkill, "SKILL.md")
	if _, err := os.Stat(devPath); err != nil {
		t.Fatalf("expected dev skill %q materialized: %v", devPath, err)
	}
}

func TestMaterializeIdempotent(t *testing.T) {
	dest := t.TempDir()
	first, err := Materialize(dest)
	if err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	second, err := Materialize(dest)
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if first != second {
		t.Fatalf("idempotent count mismatch: first=%d second=%d", first, second)
	}
}

func TestMaterializeDoesNotDeleteOthers(t *testing.T) {
	dest := t.TempDir()
	custom := filepath.Join(dest, "my-custom-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(custom), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const sentinel = "custom skill content sentinel"
	if err := os.WriteFile(custom, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if _, err := Materialize(dest); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("read sentinel after Materialize: %v", err)
	}
	if string(got) != sentinel {
		t.Fatalf("sentinel file was modified: got %q want %q", got, sentinel)
	}
}

func TestNames(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("expected non-empty names")
	}
	if !slices.IsSorted(names) {
		t.Fatalf("expected sorted names, got %v", names)
	}
	if !slices.Contains(names, "office-protocols") {
		t.Fatalf("expected office-protocols in names, got %v", names)
	}
}

// embeddedFileCount reports the number of regular files in the embedded
// FS — used to keep the bundle assertion in lockstep with what ships.
func embeddedFileCount(t *testing.T) int {
	t.Helper()
	count := 0
	err := fs.WalkDir(FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded FS: %v", err)
	}
	return count
}

func TestEmbeddedFileCountMatchesMaterialize(t *testing.T) {
	dest := t.TempDir()
	n, err := Materialize(dest)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := embeddedFileCount(t); got != n {
		t.Fatalf("embedded file count %d != materialized count %d", got, n)
	}
}
