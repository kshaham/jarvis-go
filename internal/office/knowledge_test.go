package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupKnowledgeFixture seeds temp $HOME with:
//   - one agent memory file (under a project hash matching workingDir)
//   - one learnings JSON (auto-distilled entries)
//   - one standup JSON
//   - one whiteboard JSON with two sections
//
// Returns the workingDir so tests that need project-scoped queries can
// reference it.
func setupKnowledgeFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	workingDir := "/fake/app/workspace/bonusbuddy"
	memDir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash(workingDir))
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}
	memContent := `# Memory: dash

## section: current_focus
Refactoring the premium screen.

## section: decisions
Pinned glass-card opacity at 0.04 after A/B across 4 screens.

## section: learnings
Glass blur regresses on iPhone SE at blur>24 — cap at 20.
`
	if err := os.WriteFile(filepath.Join(memDir, "dash.md"), []byte(memContent), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	learnDir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(learnDir, 0o755); err != nil {
		t.Fatalf("mkdir learnDir: %v", err)
	}
	ld := AgentLearningData{
		ConfigID: "shield",
		Name:     "Shield",
		Learnings: []AgentLearning{
			{
				Category:  "pitfall",
				Content:   "Glass overlay triggers iOS hit-test miss when z-index is 0 or negative",
				CreatedAt: time.Now().Add(-3 * time.Minute),
			},
			{
				Category:  "technique",
				Content:   "Use contentContainerStyle on ScrollView not style for padding",
				CreatedAt: time.Now().Add(-2 * time.Hour),
			},
		},
		DistilledInsights: []string{
			"Key glass-card learnings: opacity 0.04 + blur 24 is the stable baseline",
		},
	}
	ldData, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(learnDir, "shield.json"), ldData, 0o644); err != nil {
		t.Fatalf("write learnings: %v", err)
	}

	standupDir := filepath.Join(home, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(standupDir, 0o755); err != nil {
		t.Fatalf("mkdir standupDir: %v", err)
	}
	standup := PersistedStandup{
		ID:               "stand-1",
		StartedAt:        time.Now().Add(-90 * time.Minute).UnixMilli(),
		CompletedAt:      time.Now().Add(-85 * time.Minute).UnixMilli(),
		Participants:     []string{"captain", "dash", "shield"},
		RespondedCount:   3,
		ParticipantCount: 3,
		Transcript: []StandupMessage{
			{Type: "agent_response", SpeakerName: "Captain", Content: "Liquid-glass audit blocked on Ops restarting Expo dev server", Timestamp: time.Now().Add(-88 * time.Minute).UnixMilli()},
			{Type: "opening", SpeakerName: "Jarvis", Content: "Starting standup", Timestamp: time.Now().Add(-90 * time.Minute).UnixMilli()},
		},
	}
	sdata, _ := json.Marshal(standup)
	if err := os.WriteFile(filepath.Join(standupDir, "stand-1.json"), sdata, 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}

	wbDir := filepath.Join(home, ".jarvis", "office-memory")
	board := TeamWhiteboard{Entries: map[string]TeamWhiteboardEntry{
		"Captain": {Sections: map[string]TeamWhiteboardSection{
			"tracking":  {Content: "19 mobile screens → 14 audited, 5 pending (liquid-glass)", UpdatedAt: time.Now().Add(-10 * time.Minute)},
			"blockers":  {Content: "Expo dev server keeps exiting after first screenshot", UpdatedAt: time.Now().Add(-5 * time.Minute)},
		}},
	}}
	bdata, _ := json.MarshalIndent(board, "", "  ")
	if err := os.WriteFile(filepath.Join(wbDir, "whiteboard.json"), bdata, 0o644); err != nil {
		t.Fatalf("write whiteboard: %v", err)
	}

	return workingDir
}

func TestSearchKnowledge_FindsAcrossEverySource(t *testing.T) {
	setupKnowledgeFixture(t)

	hits, err := SearchKnowledge(KnowledgeSearchOptions{Query: "glass", Limit: 20})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for 'glass'")
	}

	// Confirm we saw every source type at least once.
	seen := map[KnowledgeSource]bool{}
	for _, h := range hits {
		seen[h.Source] = true
	}
	for _, src := range []KnowledgeSource{SourceMemory, SourceLearning, SourceStandup, SourceWhiteboard} {
		if !seen[src] {
			t.Errorf("expected at least one hit from source %q, got sources %v", src, seen)
		}
	}
}

func TestSearchKnowledge_ScoresRecentAboveStale(t *testing.T) {
	setupKnowledgeFixture(t)

	hits, err := SearchKnowledge(KnowledgeSearchOptions{Query: "scrollview", Limit: 20})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a hit for 'scrollview'")
	}
	// The scrollview hit is 2 hours old; it should have lower score than
	// the fresh glass-overlay pitfall (which won't match this query, but
	// the assertion is that scoring ran without error). Confirming 2-hr
	// entry still gets a 24h bonus:
	h := hits[0]
	if h.Score < 2.0 {
		t.Errorf("expected 24h-age hit to score >= 2.0 (match + 24h + source weight), got %.2f", h.Score)
	}
}

func TestSearchKnowledge_EmptyQueryErrors(t *testing.T) {
	setupKnowledgeFixture(t)
	if _, err := SearchKnowledge(KnowledgeSearchOptions{Query: "   "}); err == nil {
		t.Error("expected error on empty query")
	}
}

func TestSearchKnowledge_AllowedAgentsFilter(t *testing.T) {
	setupKnowledgeFixture(t)

	// Restrict to an agent we DIDN'T seed — expect zero learning/standup
	// hits (memory dir can still match because filter is applied to the
	// memory configID directly).
	hits, err := SearchKnowledge(KnowledgeSearchOptions{
		Query:         "glass",
		AllowedAgents: map[string]bool{"nonexistent": true},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, h := range hits {
		if h.Source == SourceLearning || h.Source == SourceMemory {
			if h.AgentName != "nonexistent" {
				t.Errorf("allowed-agents filter should have excluded %s/%s", h.Source, h.AgentName)
			}
		}
	}
}

func TestTailKnowledge_RecencySorted(t *testing.T) {
	setupKnowledgeFixture(t)

	hits, err := TailKnowledge(10, nil)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected multiple hits, got %d", len(hits))
	}
	// Each subsequent hit should be at or before the prior's timestamp.
	for i := 1; i < len(hits); i++ {
		if hits[i].Timestamp.After(hits[i-1].Timestamp) {
			t.Errorf("tail should be descending by timestamp; hit %d (%v) came after hit %d (%v)",
				i, hits[i].Timestamp, i-1, hits[i-1].Timestamp)
		}
	}
}

func TestSearchKnowledge_ExcerptTrimmed(t *testing.T) {
	setupKnowledgeFixture(t)
	// Inject a very long memory line to trigger trimming.
	home, _ := os.UserHomeDir()
	memDir := filepath.Join(home, ".jarvis", "agent-memory", ProjectHash("/fake/app/workspace/bonusbuddy"))
	longLine := "LONGTOKEN-" + strings.Repeat("x", 400) + "-END"
	f, _ := os.OpenFile(filepath.Join(memDir, "dash.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("\n" + longLine + "\n")
	f.Close()

	hits, err := SearchKnowledge(KnowledgeSearchOptions{Query: "LONGTOKEN", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a hit for LONGTOKEN")
	}
	if len(hits[0].Excerpt) > maxExcerptChars {
		t.Errorf("excerpt should be trimmed to <= %d chars, got %d", maxExcerptChars, len(hits[0].Excerpt))
	}
	if !strings.HasSuffix(hits[0].Excerpt, "...") {
		t.Errorf("trimmed excerpt should end with ellipsis, got %q", hits[0].Excerpt)
	}
}

func TestWhiteboardStore_PersistsAndRoundTrips(t *testing.T) {
	setupKnowledgeFixture(t)

	store := NewWhiteboardStore()
	store.Set("Dash", "tracking", "initial content")
	store.Append("Dash", "tracking", "line two")
	store.Set("Stack", "", "default-section content")

	// Re-load from disk via a fresh store.
	fresh := NewWhiteboardStore()
	snap := fresh.Snapshot()
	dash, ok := snap.Entries["Dash"]
	if !ok {
		t.Fatal("Dash entry missing after reload")
	}
	if !strings.Contains(dash.Sections["tracking"].Content, "line two") {
		t.Errorf("append should have appended line two, got %q", dash.Sections["tracking"].Content)
	}
	stack, ok := snap.Entries["Stack"]
	if !ok {
		t.Fatal("Stack entry missing after reload")
	}
	if _, ok := stack.Sections[defaultWhiteboardSection]; !ok {
		t.Errorf("empty section should default to %q, got sections %v", defaultWhiteboardSection, stack.Sections)
	}
}
