package office

// R20 · Persistence-hardening tests. The behavior that matters:
//   - missing file -> not present, no broadcast
//   - parse failure -> quarantined + broadcast fired
//   - negative number -> clamped with WARN
//   - bogus timestamps -> dropped / clamped

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func captureBroadcasts() (BroadcastFunc, func() []string) {
	var mu sync.Mutex
	var events []string
	fn := func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	snap := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(events))
		copy(out, events)
		return out
	}
	return fn, snap
}

func TestLoadJSONFile_MissingReturnsFalseFalse(t *testing.T) {
	broadcast, snap := captureBroadcasts()
	path := filepath.Join(t.TempDir(), "nope.json")
	var dst map[string]int
	present, loaded := loadJSONFile(path, &dst, broadcast)
	if present || loaded {
		t.Fatalf("expected (false,false), got (%v,%v)", present, loaded)
	}
	if evs := snap(); len(evs) != 0 {
		t.Fatalf("missing file should not broadcast, got %v", evs)
	}
}

func TestLoadJSONFile_ParseErrorQuarantinesAndBroadcasts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	broadcast, snap := captureBroadcasts()
	var dst map[string]int
	present, loaded := loadJSONFile(path, &dst, broadcast)
	if !present {
		t.Fatal("present should be true when file exists")
	}
	if loaded {
		t.Fatal("loaded should be false on parse error")
	}
	// Original file should be renamed to <path>.corrupt-<unix>.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected original file to be renamed away")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("expected one quarantine file, got %v", matches)
	}
	evs := snap()
	if len(evs) != 1 || evs[0] != "office:stateCorruption" {
		t.Fatalf("expected office:stateCorruption broadcast, got %v", evs)
	}
}

func TestLoadJSONFile_ValidParsesCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.json")
	if err := os.WriteFile(path, []byte(`{"k":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	broadcast, snap := captureBroadcasts()
	var dst map[string]int
	present, loaded := loadJSONFile(path, &dst, broadcast)
	if !present || !loaded {
		t.Fatalf("expected (true,true), got (%v,%v)", present, loaded)
	}
	if dst["k"] != 1 {
		t.Fatalf("expected parsed k=1, got %v", dst)
	}
	if len(snap()) != 0 {
		t.Fatal("successful load should not broadcast")
	}
}

func TestLoadFloat64File_NegativeClampsToZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "budget.txt")
	if err := os.WriteFile(path, []byte("-42.5"), 0644); err != nil {
		t.Fatal(err)
	}
	broadcast, _ := captureBroadcasts()
	v, ok := loadFloat64File(path, broadcast)
	if !ok {
		t.Fatal("expected loaded=true for negative input")
	}
	if v != 0 {
		t.Fatalf("expected clamp to 0, got %v", v)
	}
}

func TestLoadFloat64File_GarbageQuarantines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-budget.txt")
	if err := os.WriteFile(path, []byte("not-a-number"), 0644); err != nil {
		t.Fatal(err)
	}
	broadcast, snap := captureBroadcasts()
	v, ok := loadFloat64File(path, broadcast)
	if ok || v != 0 {
		t.Fatalf("expected (0,false), got (%v,%v)", v, ok)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected quarantine rename")
	}
	evs := snap()
	if len(evs) != 1 || evs[0] != "office:stateCorruption" {
		t.Fatalf("expected stateCorruption broadcast, got %v", evs)
	}
}

func TestSanitizeMessageTimestamps_DropsZeroAndClampsFuture(t *testing.T) {
	now := time.Now()
	in := []OfficeMessage{
		{ID: "good", Timestamp: now.Add(-5 * time.Minute)},
		{ID: "zero", Timestamp: time.Time{}},
		{ID: "future", Timestamp: now.Add(48 * time.Hour)},
	}
	out, dropped, clamped := sanitizeMessageTimestamps(in)
	if dropped != 1 {
		t.Fatalf("expected 1 dropped, got %d", dropped)
	}
	if clamped != 1 {
		t.Fatalf("expected 1 clamped, got %d", clamped)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(out))
	}
	for _, m := range out {
		if strings.Contains(m.ID, "zero") {
			t.Fatalf("zero-timestamp message should have been dropped")
		}
	}
}
