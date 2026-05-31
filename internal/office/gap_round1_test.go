package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- knowledge scanners: WorkingDir-scoped + edge branches ----------------

func TestScanMemoryHits_WorkingDirScoped(t *testing.T) {
	// Reuse the shared fixture which seeds a memory file under the project
	// hash of workingDir. Scoping to that workingDir exercises the
	// `opts.WorkingDir != ""` single-project branch.
	workingDir := setupKnowledgeFixture(t)
	hits := scanMemoryHits("", KnowledgeSearchOptions{WorkingDir: workingDir})
	if len(hits) == 0 {
		t.Fatal("expected scoped memory hits, got none")
	}
	for _, h := range hits {
		if h.Source != SourceMemory {
			t.Errorf("expected SourceMemory, got %s", h.Source)
		}
		if h.AgentName != "dash" {
			t.Errorf("expected agent dash, got %s", h.AgentName)
		}
	}
}

func TestScanMemoryHits_WorkingDirMissingProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// No agent-memory dir at all for this project -> empty, no panic.
	hits := scanMemoryHits("", KnowledgeSearchOptions{WorkingDir: "/no/such/project"})
	if len(hits) != 0 {
		t.Errorf("expected no hits for missing project dir, got %d", len(hits))
	}
}

func TestScanMemoryHits_SectionHeaderAndAllowedFilter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workingDir := "/proj/x"
	memDir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash(workingDir))
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Memory: ace\n\n## section: focus\nbuild the thing\n\nblank line above counted in focus\n"
	if err := os.WriteFile(filepath.Join(memDir, "ace.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "other.md"), []byte("# Memory: other\nstuff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// AllowedAgents only includes "ace" -> "other" file is skipped.
	hits := scanMemoryHits("", KnowledgeSearchOptions{
		WorkingDir:    workingDir,
		AllowedAgents: map[string]bool{"ace": true},
	})
	if len(hits) == 0 {
		t.Fatal("expected hits for ace")
	}
	for _, h := range hits {
		if h.AgentName != "ace" {
			t.Errorf("AllowedAgents filter leaked agent %s", h.AgentName)
		}
		if h.Section != "focus" {
			t.Errorf("expected section parsed to 'focus', got %q", h.Section)
		}
	}
}

func TestScanWhiteboardHits_NeedleFiltersAndMissing(t *testing.T) {
	workingDir := setupKnowledgeFixture(t)
	_ = workingDir
	// Needle match: "expo" appears in a blockers section.
	hits := scanWhiteboardHits("expo")
	if len(hits) == 0 {
		t.Fatal("expected a whiteboard hit for needle 'expo'")
	}
	for _, h := range hits {
		if h.Source != SourceWhiteboard {
			t.Errorf("expected whiteboard source, got %s", h.Source)
		}
		if !strings.Contains(strings.ToLower(h.Excerpt), "expo") {
			t.Errorf("needle should filter to matching content, got %q", h.Excerpt)
		}
	}

	// Now point HOME at an empty dir: missing whiteboard.json -> nil.
	t.Setenv("HOME", t.TempDir())
	if got := scanWhiteboardHits("anything"); got != nil {
		t.Errorf("expected nil for missing whiteboard, got %v", got)
	}
}

func TestScanStandupHits_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := scanStandupHits("x", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil for missing standups dir, got %v", got)
	}
}

func TestScanLearningHits_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := scanLearningHits("x", KnowledgeSearchOptions{}); got != nil {
		t.Errorf("expected nil for missing learnings dir, got %v", got)
	}
}

func TestTailKnowledge_LimitClamp(t *testing.T) {
	setupKnowledgeFixture(t)
	// limit <= 0 falls back to defaultKnowledgeLimit; pass a tiny limit to
	// exercise the truncation branch.
	hits, err := TailKnowledge(1, nil)
	if err != nil {
		t.Fatalf("TailKnowledge: %v", err)
	}
	if len(hits) > 1 {
		t.Errorf("limit=1 should clamp to 1 hit, got %d", len(hits))
	}
	// limit<=0 path.
	all, err := TailKnowledge(0, nil)
	if err != nil {
		t.Fatalf("TailKnowledge(0): %v", err)
	}
	if len(all) == 0 {
		t.Error("expected hits with default limit")
	}
}

func TestTrimExcerpt_LongAndMultibyte(t *testing.T) {
	short := "hello world"
	if got := trimExcerpt("  " + short + "  "); got != short {
		t.Errorf("short line should be trimmed of whitespace only, got %q", got)
	}
	// Build a string longer than maxExcerptChars (220) ending mid-multibyte.
	long := strings.Repeat("a", 218) + "é" + strings.Repeat("b", 20)
	got := trimExcerpt(long)
	if len(got) > maxExcerptChars {
		t.Errorf("excerpt exceeds max: len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("long excerpt should end with ellipsis, got %q", got)
	}
}

// --- skills.go ------------------------------------------------------------

func TestReadSkillDescription_Variants(t *testing.T) {
	dir := t.TempDir()

	// 1. Missing file -> error.
	if _, err := readSkillDescription(filepath.Join(dir, "missing.md")); err == nil {
		t.Error("expected error for missing SKILL.md")
	}

	// 2. Valid frontmatter with quoted description.
	good := filepath.Join(dir, "good.md")
	os.WriteFile(good, []byte("---\nname: Foo\ndescription: \"Does a thing\"\n---\n# Foo\n"), 0o644)
	desc, err := readSkillDescription(good)
	if err != nil {
		t.Fatalf("readSkillDescription: %v", err)
	}
	if desc != "Does a thing" {
		t.Errorf("description = %q, want 'Does a thing'", desc)
	}

	// 3. Frontmatter present but no description field -> "".
	nodesc := filepath.Join(dir, "nodesc.md")
	os.WriteFile(nodesc, []byte("---\nname: Bar\n---\nbody\n"), 0o644)
	if d, _ := readSkillDescription(nodesc); d != "" {
		t.Errorf("expected empty desc when no description field, got %q", d)
	}

	// 4. No frontmatter at the top -> "".
	noFm := filepath.Join(dir, "nofm.md")
	os.WriteFile(noFm, []byte("# Just a heading\nsome text\nmore text\n"), 0o644)
	if d, _ := readSkillDescription(noFm); d != "" {
		t.Errorf("expected empty desc when no frontmatter, got %q", d)
	}
}

func TestOneLine_CollapsesAndTruncates(t *testing.T) {
	in := "line one\nline   two\tthree"
	got := oneLine(in)
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("oneLine should collapse newlines/tabs, got %q", got)
	}
	if got != "line one line two three" {
		t.Errorf("oneLine = %q", got)
	}
	// Truncation: > 110 chars.
	long := strings.Repeat("x ", 100)
	out := oneLine(long)
	if len(out) > 110 {
		t.Errorf("oneLine should cap at 110, got len=%d", len(out))
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("truncated oneLine should end with ..., got %q", out)
	}
}

func TestOneLine_MultibyteTruncation(t *testing.T) {
	// A long string with a multibyte rune near the cut point must not panic
	// and must stay valid UTF-8.
	long := strings.Repeat("a", 108) + "é" + strings.Repeat("b", 20)
	out := oneLine(long)
	if len(out) > 110 {
		t.Errorf("len=%d exceeds 110", len(out))
	}
	if !utf8ValidString(out) {
		t.Errorf("oneLine produced invalid UTF-8: %q", out)
	}
}

// --- shared_skills: empty-name root branch --------------------------------

func TestIsWithinSkillsRoot_RootItselfAllowed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := sharedSkillsRoot()
	if !isWithinSkillsRoot(root) {
		t.Error("the skills root itself should be considered within-root")
	}
	// A sibling that shares the prefix but is NOT a child must be rejected.
	if isWithinSkillsRoot(root + "XY") {
		t.Error("prefix-sharing sibling should be rejected")
	}
	// A genuine child is allowed.
	if !isWithinSkillsRoot(filepath.Join(root, "my-skill")) {
		t.Error("a child path should be within root")
	}
}

// --- standup persistence error branches -----------------------------------

func TestLoadPersistedStandup_CorruptJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPersistedStandup("bad"); err == nil {
		t.Error("expected error loading corrupt standup json")
	}
}

func TestLoadPersistedStandup_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadPersistedStandup("nope"); err == nil {
		t.Error("expected error for missing standup file")
	}
}

func TestLatestCompletedStandupID_SkipsCorruptPicksValid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid standup (older).
	valid := PersistedStandup{ID: "good", CompletedAt: time.Now().UnixMilli()}
	vd, _ := json.Marshal(valid)
	os.WriteFile(filepath.Join(dir, "good.json"), vd, 0o644)
	// Make the corrupt one newer (later mtime) so it is tried first and skipped.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{broken"), 0o644)

	id, err := LatestCompletedStandupID()
	if err != nil {
		t.Fatalf("LatestCompletedStandupID: %v", err)
	}
	if id != "good" {
		t.Errorf("expected to fall through corrupt to 'good', got %q", id)
	}
}

// --- manager.PersistMetrics happy path ------------------------------------

func TestPersistMetrics_RoundTripCounters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestManager()
	m.metrics.TotalTasks.Store(7)
	m.metrics.TotalMessages.Store(3)

	m.PersistMetrics()

	path := filepath.Join(home, ".jarvis", "office-memory", "metrics.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("metrics.json should be written: %v", err)
	}
	var snap MetricsSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("metrics.json should be valid JSON: %v", err)
	}
	if snap.TotalTasks != 7 {
		t.Errorf("persisted TotalTasks = %d, want 7", snap.TotalTasks)
	}

	// Round-trip: a fresh manager loads the same counters back.
	m2 := newTestManager()
	m2.LoadPersistedMetrics()
	if got := m2.metrics.TotalTasks.Load(); got != 7 {
		t.Errorf("loaded TotalTasks = %d, want 7", got)
	}
}

// --- new_project validation -----------------------------------------------

func TestPrepareNewProject_InvalidNames(t *testing.T) {
	bad := []string{
		"",
		"has spaces",
		"has/slash",
		"has.dot",
		"../escape",
		strings.Repeat("a", 65), // > 64 chars
	}
	for _, name := range bad {
		_, err := PrepareNewProject(name, "mission", nil)
		if err != ErrInvalidProjectName {
			t.Errorf("PrepareNewProject(%q) err = %v, want ErrInvalidProjectName", name, err)
		}
	}
}

func TestPrepareNewProject_ValidNameAccepted(t *testing.T) {
	// Valid names pass the regex. The mkdir target is the fixed
	// NewProjectRoot which may not be writable in the sandbox; tolerate a
	// mkdir error but assert it is NOT the validation error.
	_, err := PrepareNewProject("valid-name_123", "mission", nil)
	if err == ErrInvalidProjectName {
		t.Errorf("valid name should not be rejected by validation: %v", err)
	}
}

// --- persistence read-error branches (non-IsNotExist) ---------------------

func TestLoadJSONFile_ReadErrorOnDirectory(t *testing.T) {
	broadcast, snap := captureBroadcasts()
	// Reading a *directory* as a file yields a non-IsNotExist error,
	// exercising the read-error branch (present=true, loaded=false + a
	// stateCorruption broadcast with kind=read).
	dirPath := t.TempDir()
	var dst map[string]int
	present, loaded := loadJSONFile(dirPath, &dst, broadcast)
	if !present || loaded {
		t.Fatalf("dir read should be (present=true, loaded=false), got (%v,%v)", present, loaded)
	}
	evs := snap()
	if len(evs) != 1 || evs[0] != "office:stateCorruption" {
		t.Fatalf("expected one stateCorruption broadcast, got %v", evs)
	}
}

func TestLoadJSONFile_ReadErrorNilBroadcastNoCrash(t *testing.T) {
	dirPath := t.TempDir()
	var dst map[string]int
	// nil broadcast must not panic on the read-error path.
	present, loaded := loadJSONFile(dirPath, &dst, nil)
	if !present || loaded {
		t.Fatalf("expected (true,false), got (%v,%v)", present, loaded)
	}
}

func TestLoadFloat64File_ReadErrorOnDirectory(t *testing.T) {
	broadcast, snap := captureBroadcasts()
	dirPath := t.TempDir()
	v, ok := loadFloat64File(dirPath, broadcast)
	if v != 0 || ok {
		t.Fatalf("dir read should yield (0,false), got (%v,%v)", v, ok)
	}
	if evs := snap(); len(evs) != 1 || evs[0] != "office:stateCorruption" {
		t.Fatalf("expected one stateCorruption broadcast (kind=read), got %v", evs)
	}
}

func TestLoadFloat64File_MissingReturnsZeroFalse(t *testing.T) {
	broadcast, _ := captureBroadcasts()
	path := filepath.Join(t.TempDir(), "missing.txt")
	if v, ok := loadFloat64File(path, broadcast); v != 0 || ok {
		t.Errorf("missing file should yield (0,false), got (%v,%v)", v, ok)
	}
}

func TestQuarantineCorruptFile_RenameFailureReturnsEmpty(t *testing.T) {
	// Quarantining a non-existent file can't rename -> returns "".
	missing := filepath.Join(t.TempDir(), "ghost.json")
	if got := quarantineCorruptFile(missing, os.ErrInvalid); got != "" {
		t.Errorf("expected empty corruptPath on rename failure, got %q", got)
	}
}

func TestQuarantineCorruptFile_RenamesExisting(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "state.json")
	if err := os.WriteFile(orig, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrupt := quarantineCorruptFile(orig, os.ErrInvalid)
	if corrupt == "" {
		t.Fatal("expected a corruptPath for a real file")
	}
	if !strings.Contains(corrupt, ".corrupt-") {
		t.Errorf("corruptPath should carry .corrupt- suffix, got %q", corrupt)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Errorf("original should have been renamed away, stat err=%v", err)
	}
	if _, err := os.Stat(corrupt); err != nil {
		t.Errorf("corrupt file should exist at %s: %v", corrupt, err)
	}
}
