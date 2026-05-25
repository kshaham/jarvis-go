package claudeauth

import "time"

// defaultNow is the real-time source; tests swap it via timeNow.
func defaultNow() time.Time { return time.Now() }

// SetClockForTest replaces the time source. Returns a restore
// function suitable for t.Cleanup.
func SetClockForTest(now func() time.Time) (restore func()) {
	prev := timeNow
	timeNow = now
	return func() { timeNow = prev }
}
