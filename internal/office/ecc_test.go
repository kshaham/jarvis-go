package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P10 · Tests for the ECC vendoring (embedded agents + commands,
// vendored skill bundle on disk, NpxAvailable cache, attribution
// invariant).

func TestECC_BuildECCAgents_LoadsFourAgents(t *testing.T) {
	agents := BuildECCAgents()
	if len(agents) < 4 {
		t.Fatalf("expected at least 4 ECC agents, got %d", len(agents))
	}
	names := map[string]bool{}
	for _, a := range agents {
		names[a.Name] = true
		if a.Description == "" {
			t.Errorf("agent %q has empty description", a.Name)
		}
		if a.Body == "" {
			t.Errorf("agent %q has empty body", a.Name)
		}
		if !hasFrontmatterAttribution(a.Body) {
			t.Errorf("agent %q body missing MIT attribution string", a.Name)
		}
	}
	for _, expected := range []string{"code-architect", "security-reviewer", "pr-test-analyzer", "code-simplifier"} {
		if !names[expected] {
			t.Errorf("expected ECC agent %q not present; got %v", expected, names)
		}
	}
}

func TestECC_BuildECCCommands_LoadsThreeCommands(t *testing.T) {
	cmds := BuildECCCommands()
	if len(cmds) < 3 {
		t.Fatalf("expected at least 3 ECC commands, got %d", len(cmds))
	}
	names := map[string]bool{}
	for _, c := range cmds {
		names[c.Name] = true
		if c.Body == "" {
			t.Errorf("command %q has empty body", c.Name)
		}
		if !hasFrontmatterAttribution(c.Body) {
			t.Errorf("command %q body missing MIT attribution string", c.Name)
		}
	}
	for _, expected := range []string{"security-scan", "test-coverage", "refactor-clean"} {
		if !names[expected] {
			t.Errorf("expected ECC command %q not present; got %v", expected, names)
		}
	}
}

func TestECC_SkillBundleOnDisk_HasMinFifteenSkillsWithAttribution(t *testing.T) {
	// Walk skills/ecc/ in the repo and assert every <name>/SKILL.md is
	// non-empty, has YAML frontmatter, and carries the attribution
	// string. P11.3.14 · use runtime.Caller for stable repoRoot
	// resolution regardless of where `go test` is invoked from.
	repoRoot := repoRootFromTestFile(t)
	bundleDir := filepath.Join(repoRoot, "skills", "ecc")

	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		t.Fatalf("read %s: %v", bundleDir, err)
	}
	skillCount := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		skillMD := filepath.Join(bundleDir, ent.Name(), "SKILL.md")
		body, err := os.ReadFile(skillMD)
		if err != nil {
			t.Errorf("missing SKILL.md for %s: %v", ent.Name(), err)
			continue
		}
		s := string(body)
		if !strings.HasPrefix(s, "---") {
			t.Errorf("%s: missing YAML frontmatter at top", ent.Name())
		}
		// P11.3.15 · attribution match relaxed to org+repo path so
		// upstream URL drift (HTTPS → SSH, repo rename) doesn't break
		// the invariant. The full text in the file remains the long
		// `> Sourced from https://github.com/...` form; we just don't
		// require the prose framing.
		if !strings.Contains(s, "github.com/affaan-m/ECC") {
			t.Errorf("%s: missing MIT attribution comment (looking for 'github.com/affaan-m/ECC')", ent.Name())
		}
		if !strings.Contains(s, "name:") {
			t.Errorf("%s: frontmatter missing 'name:' key", ent.Name())
		}
		if !strings.Contains(s, "description:") {
			t.Errorf("%s: frontmatter missing 'description:' key", ent.Name())
		}
		skillCount++
	}
	if skillCount < 15 {
		t.Errorf("expected at least 15 ECC skills bundled, found %d", skillCount)
	}
}

func TestECC_NpxAvailable_CachesLookup(t *testing.T) {
	resetNpxCacheForTest()
	t.Cleanup(resetNpxCacheForTest)
	// First call performs the LookPath; cache result.
	first := NpxAvailable()
	// Second call must return the same cached value regardless of
	// environment changes mid-test.
	second := NpxAvailable()
	if first != second {
		t.Errorf("NpxAvailable not stable across calls: first=%v second=%v", first, second)
	}
}

func TestECC_NpxAvailable_FalseWhenBinaryMissing(t *testing.T) {
	// Clobber PATH to a known-empty dir; ensure NpxAvailable reports
	// false. P10.4 graceful-degrade contract.
	t.Setenv("PATH", t.TempDir())
	resetNpxCacheForTest()
	t.Cleanup(resetNpxCacheForTest)
	if NpxAvailable() {
		t.Error("expected NpxAvailable=false with empty PATH")
	}
}

func TestECC_SplitFrontmatter_TolerantOfMissingFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter("no frontmatter here\njust body")
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter map, got %v", fm)
	}
	if body != "no frontmatter here\njust body" {
		t.Errorf("body altered when no frontmatter: %q", body)
	}
}

func TestECC_SplitToolList_HandlesBothQuotedAndBare(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`[Read, Write, Edit]`, []string{"Read", "Write", "Edit"}},
		{`["Read", "Write", "Edit"]`, []string{"Read", "Write", "Edit"}},
		{`[Bash]`, []string{"Bash"}},
		{``, nil},
	}
	for _, tc := range cases {
		got := splitToolList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitToolList(%q) len=%d want=%d (got=%v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitToolList(%q)[%d]=%q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestECC_DedupeSkills_PreservesFirstOccurrence(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b", "d"}
	got := dedupeSkills(in)
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("dedupeSkills len=%d want=%d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupeSkills[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestECC_PickSkillsForRole_IncludesECCAdditions(t *testing.T) {
	// Each role gained ECC entries in P10.5; assert at least one
	// known-ECC skill name appears in the picked list. Acts as a
	// regression guard against a future "clean up the recommender"
	// PR that drops the ECC layer.
	cases := map[string]string{
		"manager":   "agent-eval",
		"architect": "api-design",
		"frontend":  "frontend-design-direction",
		"backend":   "backend-patterns",
		"devops":    "git-workflow",
		"qa":        "tdd-workflow",
		"designer":  "frontend-design-direction",
	}
	for role, expected := range cases {
		got := pickSkillsForRole(role, true)
		found := false
		for _, s := range got {
			if s == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pickSkillsForRole(%q, eccEnabled=true) missing ECC skill %q; got %v", role, expected, got)
		}
	}
}

// P11.2.7 regression: ECC entries must NOT appear when eccEnabled=false.
func TestECC_PickSkillsForRole_FiltersECCWhenDisabled(t *testing.T) {
	for _, role := range []string{"manager", "architect", "frontend", "backend", "devops", "qa", "designer"} {
		got := pickSkillsForRole(role, false)
		for _, s := range got {
			if _, isECC := eccSkillNames[s]; isECC {
				t.Errorf("pickSkillsForRole(%q, eccEnabled=false) leaked ECC skill %q; full list: %v", role, s, got)
			}
		}
	}
}

// P11.2.6 regression: splitFrontmatter must handle CRLF line endings
// (a vendored .md checked in from a Windows host). Pre-fix, the
// `---\r` line wouldn't equal `---` and the entire frontmatter was
// silently discarded.
func TestECC_SplitFrontmatter_HandlesCRLF(t *testing.T) {
	crlf := "---\r\nname: x\r\ndescription: y\r\n---\r\n\r\nbody\r\n"
	fm, body := splitFrontmatter(crlf)
	if fm["name"] != "x" || fm["description"] != "y" {
		t.Errorf("CRLF frontmatter not parsed: %v", fm)
	}
	if !strings.Contains(body, "body") {
		t.Errorf("CRLF body lost: %q", body)
	}
}

// P11.1.2 regression: safeBasename must reject path-traversal shapes
// so a poisoned frontmatter `name: ../../etc/passwd` doesn't reach
// the downstream WriteAgents filesystem write.
func TestECC_SafeBasename_RejectsTraversal(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"safe", true},
		{"with-dashes", true},
		{"agent_name", true},
		{"x.md", true}, // dot is fine; safeBasename only blocks leading dot
		{"", false},
		{".hidden", false},
		{".", false},
		{"..", false},
		{"foo/bar", false},
		{"foo\\bar", false},
		{"../escape", false},
		{"/absolute", false},
	}
	for _, c := range cases {
		_, ok := safeBasename(c.in)
		if ok != c.ok {
			t.Errorf("safeBasename(%q) = %v, want %v", c.in, ok, c.ok)
		}
	}
}

// P11.1.3 regression: hasBodyAttribution must reject content where
// the attribution string appears OUTSIDE the first 200 bytes (a
// "vendor cleanup" PR that buries it deep in the body).
func TestECC_HasBodyAttribution_FirstWindowOnly(t *testing.T) {
	prefix := strings.Repeat("filler text ", 50) // 600 bytes of filler > 200 byte window
	deep := prefix + "github.com/affaan-m/ECC"
	if hasBodyAttribution(deep) {
		t.Error("attribution buried past 200-byte window should fail the check")
	}
	leading := "github.com/affaan-m/ECC — " + prefix
	if !hasBodyAttribution(leading) {
		t.Error("attribution in the leading window should pass")
	}
}

// P11.2.8 regression: BuildECCAgents must return a distinct slice on
// each call so a downstream caller mutating its copy doesn't poison
// future callers' results.
func TestECC_BuildECCAgents_ReturnsDistinctSlices(t *testing.T) {
	a := BuildECCAgents()
	b := BuildECCAgents()
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("expected non-empty slices; got len(a)=%d len(b)=%d", len(a), len(b))
	}
	// Mutate a; b should be unaffected.
	a[0].Name = "POISONED"
	if b[0].Name == "POISONED" {
		t.Error("BuildECCAgents leaked the cached slice; mutation of a polluted b")
	}
}

func TestECC_EmbedSummary_NonEmpty(t *testing.T) {
	got := EmbedSummaryForLogging()
	if !strings.Contains(got, "agents") || !strings.Contains(got, "commands") {
		t.Errorf("embed summary should mention both agents and commands; got %q", got)
	}
}
