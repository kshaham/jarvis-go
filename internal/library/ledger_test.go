package library

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLedger_FirstIncrementCreatesEntry(t *testing.T) {
	withTempLibrary(t)
	l, err := OpenLedger()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Increment("experiences/vex/task-1.md", 0.80); err != nil {
		t.Fatal(err)
	}
	e, err := l.Get("experiences/vex/task-1.md")
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Fatal("entry not created")
	}
	if e.ReuseCount != 1 {
		t.Errorf("ReuseCount=%d want 1", e.ReuseCount)
	}
	if e.AvgOutcome < 0.79 || e.AvgOutcome > 0.81 {
		t.Errorf("AvgOutcome=%v want ~0.80", e.AvgOutcome)
	}
	if e.FirstSeenAt.IsZero() || e.LastUsedAt.IsZero() {
		t.Error("timestamps not set on first increment")
	}
}

func TestLedger_IncrementRollingAverage(t *testing.T) {
	withTempLibrary(t)
	l, _ := OpenLedger()
	// Three increments: 0.50, 0.70, 0.90. Mean = 0.70.
	for _, s := range []float64{0.50, 0.70, 0.90} {
		if err := l.Increment("p", s); err != nil {
			t.Fatal(err)
		}
	}
	e, _ := l.Get("p")
	if e.ReuseCount != 3 {
		t.Errorf("ReuseCount=%d want 3", e.ReuseCount)
	}
	if e.AvgOutcome < 0.69 || e.AvgOutcome > 0.71 {
		t.Errorf("AvgOutcome=%v want ~0.70", e.AvgOutcome)
	}
}

func TestLedger_PersistsAcrossInstances(t *testing.T) {
	// Two distinct Ledger handles to the same library dir must see
	// the same state on disk.
	withTempLibrary(t)
	first, _ := OpenLedger()
	if err := first.Increment("p", 0.85); err != nil {
		t.Fatal(err)
	}

	second, _ := OpenLedger()
	e, err := second.Get("p")
	if err != nil {
		t.Fatal(err)
	}
	if e == nil || e.ReuseCount != 1 {
		t.Errorf("second handle didn't see persisted entry: %+v", e)
	}
}

func TestLedger_MarkPromotedSetsTimestamp(t *testing.T) {
	withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.Increment("p", 0.85); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkPromoted("p"); err != nil {
		t.Fatal(err)
	}
	e, _ := l.Get("p")
	if e.PromotedAt.IsZero() {
		t.Error("PromotedAt not stamped")
	}
}

func TestLedger_MarkPromotedUnknownPathIsNoOp(t *testing.T) {
	withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.MarkPromoted("never-seen"); err != nil {
		t.Errorf("unexpected error for unknown path: %v", err)
	}
	if e, _ := l.Get("never-seen"); e != nil {
		t.Errorf("ledger created entry for unknown path: %+v", e)
	}
}

func TestLedger_MarkRevokedClearsPromotedAt(t *testing.T) {
	// Revocation should reset PromotedAt so the 24h cooldown +
	// re-promotion path works as designed.
	withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.Increment("p", 0.85); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkPromoted("p"); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkRevoked("p", "operator concern"); err != nil {
		t.Fatal(err)
	}
	e, _ := l.Get("p")
	if !e.PromotedAt.IsZero() {
		t.Error("PromotedAt should be cleared on revoke")
	}
	if e.RevokedAt.IsZero() {
		t.Error("RevokedAt not stamped")
	}
	if e.RevokedReason != "operator concern" {
		t.Errorf("RevokedReason=%q want 'operator concern'", e.RevokedReason)
	}
}

func TestLedger_MarkRevokedCreatesEntryIfMissing(t *testing.T) {
	// Operators can preemptively block a path they haven't seen
	// cited yet.
	withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.MarkRevoked("future-path", "blocking proactively"); err != nil {
		t.Fatal(err)
	}
	e, _ := l.Get("future-path")
	if e == nil {
		t.Fatal("entry not created on preemptive revoke")
	}
	if e.RevokedAt.IsZero() {
		t.Error("RevokedAt not set")
	}
}

func TestLedger_ListSortedByReuseDescending(t *testing.T) {
	withTempLibrary(t)
	l, _ := OpenLedger()
	_ = l.Increment("low", 0.7)
	_ = l.Increment("mid", 0.7)
	_ = l.Increment("mid", 0.8)
	_ = l.Increment("high", 0.6)
	_ = l.Increment("high", 0.7)
	_ = l.Increment("high", 0.8)

	rows, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("List returned %d rows, want 3", len(rows))
	}
	if rows[0].Path != "high" || rows[1].Path != "mid" || rows[2].Path != "low" {
		t.Errorf("order wrong: %s, %s, %s",
			rows[0].Path, rows[1].Path, rows[2].Path)
	}
}

func TestLedger_EmptyPathIsNoOp(t *testing.T) {
	withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.Increment("", 0.85); err != nil {
		t.Errorf("empty path should be no-op, got %v", err)
	}
	rows, _ := l.List()
	if len(rows) != 0 {
		t.Errorf("ledger should be empty, got %d entries", len(rows))
	}
}

func TestLedger_ConcurrentIncrementsSerialize(t *testing.T) {
	// 50 concurrent increments to the same path. ReuseCount should
	// land at exactly 50 (mutex protects the read-modify-write).
	withTempLibrary(t)
	l, _ := OpenLedger()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Increment("p", 0.5)
		}()
	}
	wg.Wait()
	e, _ := l.Get("p")
	if e == nil || e.ReuseCount != 50 {
		t.Errorf("ReuseCount=%d want 50", e.ReuseCount)
	}
}

func TestLedger_OnDiskFileLandsUnderRoot(t *testing.T) {
	root := withTempLibrary(t)
	l, _ := OpenLedger()
	if err := l.Increment("p", 0.5); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".promotion-ledger.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("ledger file not at %s: %v", path, err)
	}
}
