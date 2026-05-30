package state

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestAppState_ConcurrentReadWrite hammers the mutating and reading entry
// points of AppState from many goroutines at once. Run under -race it acts as
// a regression guard for the data-race classes that were fixed: the RWMutex
// guarding Settings/Config/Sessions/DailyStats, the map-copy reads, and the
// debounced background flush goroutine (time.AfterFunc) that touches the same
// fields and marshals the struct concurrently with these callers.
//
// No time.Sleep ordering, no flakiness: every goroutine does a fixed number of
// iterations and the WaitGroup is the only synchronization. The race window is
// hit by sheer overlap (>=20 readers + >=20 writers x hundreds of iterations).
func TestAppState_ConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	s := NewStore(primary)

	const (
		writerGoroutines = 24
		readerGoroutines = 24
		iterations       = 400
	)

	var wg sync.WaitGroup

	// Writers: mutate Settings, Config, Sessions and DailyStats. These take the
	// write lock and also arm the debounced flush timer, which fires its own
	// goroutine that re-locks and marshals the whole struct.
	for w := 0; w < writerGoroutines; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				s.UpdateSettings(map[string]interface{}{
					fmt.Sprintf("writer-%d-key", id): i,
					"shared-key":                      id*1000 + i,
				})
				s.UpdateConfig(map[string]interface{}{
					fmt.Sprintf("cfg-%d", id): i,
				})
				s.SaveSession(&SessionState{
					ID:     fmt.Sprintf("sess-%d-%d", id, i%4),
					Name:   "n",
					Status: "running",
				})
				s.RecordDailyStats(int64(i), int64(i), "claude-sonnet-4")
			}
		}(w)
	}

	// Readers: take the read lock and copy out the maps/slices, and also
	// marshal the live struct (the production flush path marshals it too, so
	// this mirrors a concurrent read+marshal against ongoing writes).
	for r := 0; r < readerGoroutines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = s.GetSettings()
				_ = s.GetConfig()
				_ = s.GetSessions()
				_ = s.GetDailyStats()
				// Marshal through the public accessor copies (the values they
				// return must be safe to serialize off-lock).
				if b, err := json.Marshal(s.GetSettings()); err != nil {
					t.Errorf("marshal settings copy: %v", err)
				} else {
					_ = b
				}
			}
		}()
	}

	wg.Wait()

	// Force any pending debounced write to land; must not race with the timer.
	s.Flush()

	// Sanity: the shared key written by every writer must be readable.
	if _, ok := s.GetSettings()["shared-key"]; !ok {
		t.Errorf("expected shared-key to be present after concurrent writes")
	}
}

// TestAppState_IncrementSettingCounter_NoLostIncrements proves the new atomic
// counter does not lose increments under contention. K goroutines each call
// IncrementSettingCounter on the SAME key M times; if the increment were a
// racy read-modify-write across separate Get/Update calls, increments would be
// lost and the final value would be < K*M. We assert final == K*M exactly.
//
// The counter is stored as float64 (see IncrementSettingCounter), so we read
// the final value back as float64. We also confirm the per-call return values
// form the complete set 1..K*M with no duplicates, which is a stronger proof
// of atomicity than the final total alone.
func TestAppState_IncrementSettingCounter_NoLostIncrements(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	s := NewStore(primary)

	const (
		goroutines        = 32
		incrementsPerGoro = 250
		key               = "checkpoint-version"
	)
	const expected = goroutines * incrementsPerGoro

	var (
		wg   sync.WaitGroup
		seen [expected + 1]int32 // index = returned value; counts collisions
		mu   sync.Mutex
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < incrementsPerGoro; i++ {
				v := s.IncrementSettingCounter(key)
				if v < 1 || v > expected {
					t.Errorf("returned counter %d out of range [1,%d]", v, expected)
					continue
				}
				mu.Lock()
				seen[v]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	s.Flush()

	// Final stored value must equal K*M with no lost increments.
	got, ok := s.GetSettings()[key].(float64)
	if !ok {
		t.Fatalf("expected counter stored as float64, got %T (%v)",
			s.GetSettings()[key], s.GetSettings()[key])
	}
	if int(got) != expected {
		t.Errorf("lost increments: final counter = %d, want %d", int(got), expected)
	}

	// Each value 1..expected must have been returned exactly once: no
	// duplicates (would mean two callers saw the same pre-increment value) and
	// no gaps.
	for v := 1; v <= expected; v++ {
		switch seen[v] {
		case 1:
			// expected
		case 0:
			t.Errorf("counter value %d was never returned (gap / lost increment)", v)
		default:
			t.Errorf("counter value %d was returned %d times (duplicate / lost increment)", v, seen[v])
		}
	}
}
