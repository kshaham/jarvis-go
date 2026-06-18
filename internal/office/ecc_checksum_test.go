package office

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// P11.1.4 · Vendor-content integrity check. Re-hashes every vendored
// ECC file and compares against skills/ecc/CHECKSUMS.sha256 (which
// was generated at vendor time). Catches accidental in-tree edits
// that drift from upstream without going through the documented
// refresh workflow in docs/ECC.md.
//
// To intentionally update content (e.g. pulling new upstream files):
//   find skills/ecc internal/office/ecc_agents internal/office/ecc_commands \
//     -name '*.md' -print0 | sort -z | xargs -0 shasum -a 256 \
//     > skills/ecc/CHECKSUMS.sha256
//
// Then commit BOTH the updated .md files AND the regenerated
// CHECKSUMS.sha256 in the same PR. This test will fail loudly if
// they fall out of sync.

func TestECC_VendoredContentChecksumMatches(t *testing.T) {
	repoRoot := repoRootFromTestFile(t)
	checksumPath := filepath.Join(repoRoot, skillsBundleRoot, "ecc", "CHECKSUMS.sha256")

	wantBySuffix, err := readChecksumFile(checksumPath)
	if err != nil {
		t.Fatalf("read %s: %v", checksumPath, err)
	}
	if len(wantBySuffix) == 0 {
		t.Fatalf("checksum file %s is empty; regenerate via the command in this test's doc comment", checksumPath)
	}

	// The bundled skill files now live under internal/skillsbundle/skills/,
	// but CHECKSUMS.sha256 records them under the original skills/ prefix
	// (the vendor-time path). recordedRel maps an on-disk path back to that
	// recorded path so the content hashes still match without regenerating
	// the checksum file.
	gotBySuffix := map[string]string{}
	for _, root := range []string{
		filepath.Join(skillsBundleRoot, "ecc"),
		filepath.Join("internal", "office", "ecc_agents"),
		filepath.Join("internal", "office", "ecc_commands"),
	} {
		absRoot := filepath.Join(repoRoot, root)
		if err := filepath.WalkDir(absRoot, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rel := recordedRel(repoRoot, p)
			// Normalise to forward-slash separators so Windows-built
			// test binaries don't drift from the recorded paths.
			relUnix := filepath.ToSlash(rel)
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			gotBySuffix[relUnix] = hex.EncodeToString(sum[:])
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", absRoot, err)
		}
	}

	// Symmetric diff: every recorded path must be present + match.
	// Every found path must be recorded.
	for relPath, wantSum := range wantBySuffix {
		gotSum, ok := gotBySuffix[relPath]
		if !ok {
			t.Errorf("checksums.sha256 lists %s but the file is missing on disk", relPath)
			continue
		}
		if gotSum != wantSum {
			t.Errorf("checksum mismatch for %s\n  recorded: %s\n  on-disk:  %s\nIf this was intentional, regenerate skills/ecc/CHECKSUMS.sha256 (see test doc).", relPath, wantSum, gotSum)
		}
	}
	for relPath := range gotBySuffix {
		if _, ok := wantBySuffix[relPath]; !ok {
			t.Errorf("file %s exists on disk but isn't in CHECKSUMS.sha256; regenerate the checksum file", relPath)
		}
	}
}

// readChecksumFile parses `shasum -a 256` output. Each line:
//
//	<64 hex>  <path>
//
// (two spaces between hash and path). Returns map[path]hex.
func readChecksumFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 66 {
			continue // skip blank / malformed
		}
		// shasum format: "<hash>  <path>" (two spaces)
		sep := strings.Index(line, "  ")
		if sep < 0 {
			continue
		}
		hash := strings.TrimSpace(line[:sep])
		path := strings.TrimSpace(line[sep+2:])
		out[filepath.ToSlash(path)] = hash
	}
	return out, scanner.Err()
}

// skillsBundleRoot is the repo-relative directory where the curated skill
// bundle now lives after being relocated under an embeddable Go package
// (internal/skillsbundle). The bundle is embedded into the binary via
// internal/skillsbundle and materialized into ~/.claude/skills at startup.
var skillsBundleRoot = filepath.Join("internal", "skillsbundle", "skills")

// recordedRel maps an on-disk path under the relocated bundle back to the
// path recorded in CHECKSUMS.sha256, which still uses the original
// `skills/...` prefix from vendor time. Paths outside the bundle (the
// embedded ecc_agents / agentskills_agents trees) are returned relative to
// repoRoot unchanged.
func recordedRel(repoRoot, p string) string {
	rel, _ := filepath.Rel(repoRoot, p)
	relUnix := filepath.ToSlash(rel)
	prefix := filepath.ToSlash(skillsBundleRoot) + "/"
	if strings.HasPrefix(relUnix, prefix) {
		return strings.TrimPrefix(relUnix, filepath.ToSlash(filepath.Join("internal", "skillsbundle"))+"/")
	}
	return relUnix
}

// repoRootFromTestFile resolves the repo root from this test file's
// own path via runtime.Caller. P11.3.14: stable regardless of where
// `go test` is invoked from (matches the audit's recommendation).
func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot resolve repo root")
	}
	// This file lives at internal/office/ecc_checksum_test.go — walk
	// up two levels to reach the repo root.
	return filepath.Join(filepath.Dir(file), "..", "..")
}
