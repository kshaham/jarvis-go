package filewatcher

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_WatchAndDetect(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := New(ctx, 100)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	var detected atomic.Bool
	err = w.Watch(testFile, func(e Event) {
		detected.Store(true)
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Modify file
	os.WriteFile(testFile, []byte("modified"), 0644)
	time.Sleep(200 * time.Millisecond)

	if !detected.Load() {
		t.Error("expected file change to be detected")
	}
}

func TestWatcher_WatchCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := New(ctx, 100)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("a"), 0644)
	os.WriteFile(f2, []byte("b"), 0644)

	w.Watch(f1, func(e Event) {})
	w.Watch(f2, func(e Event) {})

	if w.WatchCount() != 2 {
		t.Errorf("expected 2 watches, got %d", w.WatchCount())
	}

	w.Unwatch(f1)
	if w.WatchCount() != 1 {
		t.Errorf("expected 1 watch after unwatch, got %d", w.WatchCount())
	}
}

func TestWatcher_MaxWatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := New(ctx, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		f := filepath.Join(dir, string(rune('a'+i))+".txt")
		os.WriteFile(f, []byte("x"), 0644)
		err := w.Watch(f, func(e Event) {})
		if i < 2 && err != nil {
			t.Fatalf("watch %d: %v", i, err)
		}
		if i == 2 && err == nil {
			t.Error("expected error at max watches")
		}
	}
}

func TestWatcher_Close(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := New(ctx, 100)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
