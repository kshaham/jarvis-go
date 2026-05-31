package pty

import (
	"strings"
	"sync"
)

// safeBuf is a goroutine-safe string accumulator for tests. Several live-PTY
// tests register an output observer via AddOutputObserver; that callback runs
// on the Session's readLoop goroutine (notifyOutputObservers, called from
// processOutput) while the test's main goroutine also reads the accumulated
// output after waiting on a done/ready signal. A bare strings.Builder shared
// that way is a data race (-race flags it). safeBuf serializes Write and String
// behind a mutex so both goroutines can touch it safely.
//
// It is a drop-in replacement for the racy `var sb strings.Builder` pattern:
//   - `sb.WriteString(d)` becomes `sb.Write(d)`
//   - `sb.String()` stays the same
//
// Behavior and assertions are unchanged; only the shared access is made safe.
type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

// Write appends s to the buffer under the lock. Named Write (not WriteString)
// so call sites read `sb.Write(d)`; it never returns an error.
func (s *safeBuf) Write(str string) {
	s.mu.Lock()
	s.b.WriteString(str)
	s.mu.Unlock()
}

// String returns the accumulated contents under the lock.
func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
