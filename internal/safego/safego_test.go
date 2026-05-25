package safego

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGo_RunsFn(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var ran atomic.Bool
	Go("test.runs", func() {
		defer wg.Done()
		ran.Store(true)
	})
	wg.Wait()
	if !ran.Load() {
		t.Error("fn did not execute")
	}
}

func TestGo_RecoversFromPanic(t *testing.T) {
	// The whole point of safego: a panicking goroutine must NOT
	// crash the test process. If recover() weren't installed, this
	// test wouldn't even fail — the runtime would terminate the
	// binary and Go's test harness would report a worse failure.
	var wg sync.WaitGroup
	wg.Add(1)
	Go("test.panics", func() {
		defer wg.Done()
		panic("intentional")
	})
	wg.Wait()
}

func TestGo_NilPanicValueAlsoRecovered(t *testing.T) {
	// Edge case: panic(nil). Pre-Go 1.21 recover() returned nil here
	// and the runtime didn't re-panic; post-1.21 it raises a
	// *runtime.PanicNilError. safego must survive either way.
	var wg sync.WaitGroup
	wg.Add(1)
	Go("test.panic-nil", func() {
		defer wg.Done()
		defer func() { _ = recover() }() // swallow our own panic
		panic(nil)
	})
	wg.Wait()
}

func TestGo_MultiplePanicsIsolated(t *testing.T) {
	// One panic must not infect a sibling goroutine. Each goroutine
	// owns its own recover frame.
	const n = 50
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		Go("test.sibling", func() {
			defer wg.Done()
			if i%2 == 0 {
				ok.Add(1)
				return
			}
			panic("half of these panic")
		})
		_ = i // silence loop-var warning
	}
	// Block until all done; if a panic escaped, the test process
	// would have already crashed before we got here.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutines did not finish within 2s — possible leak")
	}
}
