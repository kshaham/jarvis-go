package office

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

// TestRunMemifyTick_PromotionListError drives the RunPromotion
// error-return branch (memify_tick.go line ~102). RunPromotion errors
// when ledger.List() fails, which happens when the on-disk ledger JSON
// is corrupt and the ledger hasn't been loaded into the in-memory cache
// yet. We write a corrupt .promotion-ledger.json into a FRESH temp
// library root (distinct path => no cache hit => fresh unloaded ledger),
// then run the tick. RunMemify succeeds (experiences dir is valid +
// empty), OpenLedger returns the fresh ledger, and RunPromotion's
// List()->load() fails parsing the malformed JSON, returning the error.
func TestRunMemifyTick_PromotionListError(t *testing.T) {
	root := t.TempDir()
	// Valid (empty) experiences + workflows dirs so RunMemify + purge
	// both succeed without error.
	for _, sub := range []string{"experiences", "workflows"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Corrupt the ledger file BEFORE the cache is populated.
	ledgerPath := filepath.Join(root, ".promotion-ledger.json")
	if err := os.WriteFile(ledgerPath, []byte("{ this is not valid json ]["), 0o644); err != nil {
		t.Fatal(err)
	}

	restore := library.SetDirForTest(root)
	t.Cleanup(restore)

	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	// Run the tick: RunPromotion should fail on the corrupt ledger and
	// the tick returns early without broadcasting.
	m.runMemifyTick()

	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("no promotion broadcast expected on corrupt-ledger error; events=%v", broker.topics())
		}
	}
}
