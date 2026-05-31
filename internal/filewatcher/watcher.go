package filewatcher

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/kshaham/jarvis/internal/safego"
)

type fileStat struct {
	size    int64
	modTime time.Time
}

type Op int

const (
	Create Op = iota
	Write
	Remove
	Rename
)

type Event struct {
	Path      string
	Op        Op
	Timestamp time.Time
}

type EventCallback func(Event)

type Watcher struct {
	fsWatcher    *fsnotify.Watcher
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	callbacks    map[string][]EventCallback
	watchCount   int
	maxWatches   int
	fallbackMode bool
	pollInterval time.Duration
	lastStats    map[string]fileStat
}

const (
	defaultMaxWatches   = 500
	initialPollInterval = 5 * time.Second
	maxPollInterval     = 30 * time.Second
	pollJitter          = 2 * time.Second
)

func New(ctx context.Context, maxWatches int) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if maxWatches <= 0 {
		maxWatches = defaultMaxWatches
	}

	childCtx, cancel := context.WithCancel(ctx)

	w := &Watcher{
		fsWatcher:    fsw,
		ctx:          childCtx,
		cancel:       cancel,
		callbacks:    make(map[string][]EventCallback),
		maxWatches:   maxWatches,
		pollInterval: initialPollInterval,
		lastStats:    make(map[string]fileStat),
	}

	safego.Go("filewatcher.eventLoop", w.eventLoop)

	return w, nil
}

func (w *Watcher) Watch(path string, cb EventCallback) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// watchCount tracks DISTINCT paths (Unwatch removes the whole path + all its
	// callbacks, decrementing by 1). Counting per-call instead would desync the
	// count from reality when a path is watched more than once, eventually
	// exhausting the watch budget with phantom watches.
	_, existed := w.callbacks[path]
	if !existed && w.watchCount >= w.maxWatches {
		return ErrMaxWatchesReached
	}

	if !existed && !w.fallbackMode {
		if err := w.fsWatcher.Add(path); err != nil {
			if isWatchLimitError(err) { // inotify exhaustion
				slog.Warn("inotify limit reached, switching to fallback polling", "path", path)
				w.switchToFallback()
			} else {
				return err // don't register a callback / count for a watch that didn't take
			}
		}
	}

	w.callbacks[path] = append(w.callbacks[path], cb)
	if !existed {
		w.watchCount++
	}
	return nil
}

func (w *Watcher) Unwatch(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.callbacks[path]; !ok {
		return nil
	}

	delete(w.callbacks, path)
	w.watchCount--

	if !w.fallbackMode {
		_ = w.fsWatcher.Remove(path)
	}

	return nil
}

func (w *Watcher) WatchCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.watchCount
}

func (w *Watcher) Close() error {
	w.cancel()
	return w.fsWatcher.Close()
}

func (w *Watcher) eventLoop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.dispatch(event.Name, mapOp(event.Op))
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.handleWatchError(err)
		}
	}
}

// handleWatchError processes an error surfaced on the fsnotify Errors
// channel. A watch-limit error flips the watcher into fallback polling
// mode; all errors are logged at debug level. Extracted from eventLoop so
// the fallback-on-error path can be exercised without racing fsnotify's
// internally-owned Errors channel.
func (w *Watcher) handleWatchError(err error) {
	if isWatchLimitError(err) {
		w.mu.Lock()
		w.switchToFallback()
		w.mu.Unlock()
	}
	slog.Debug("fsnotify error", "error", err)
}

func (w *Watcher) dispatch(path string, op Op) {
	w.mu.RLock()
	cbs := w.callbacks[path]
	w.mu.RUnlock()

	event := Event{Path: path, Op: op, Timestamp: time.Now()}
	for _, cb := range cbs {
		cb(event)
	}
}

// switchToFallback enables fallback polling mode. Must be called while holding w.mu.
func (w *Watcher) switchToFallback() {
	if w.fallbackMode {
		return
	}
	w.fallbackMode = true
	safego.Go("filewatcher.pollLoop", w.pollLoop)
}

func (w *Watcher) pollLoop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(w.currentPollInterval()):
			w.mu.RLock()
			paths := make([]string, 0, len(w.callbacks))
			for p := range w.callbacks {
				paths = append(paths, p)
			}
			w.mu.RUnlock()

			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				current := fileStat{size: info.Size(), modTime: info.ModTime()}

				w.mu.Lock()
				prev, exists := w.lastStats[path]
				w.lastStats[path] = current
				w.mu.Unlock()

				if !exists || current.size != prev.size || !current.modTime.Equal(prev.modTime) {
					w.dispatch(path, Write)
				}
			}
		}
	}
}

func (w *Watcher) currentPollInterval() time.Duration {
	jitter := time.Duration(rand.Int63n(int64(pollJitter)))
	return w.pollInterval + jitter
}

func mapOp(op fsnotify.Op) Op {
	switch {
	case op.Has(fsnotify.Create):
		return Create
	case op.Has(fsnotify.Write):
		return Write
	case op.Has(fsnotify.Remove):
		return Remove
	case op.Has(fsnotify.Rename):
		return Rename
	default:
		return Write
	}
}

func isWatchLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no space left on device") ||
		strings.Contains(s, "too many open files") ||
		strings.Contains(s, "ENOSPC")
}

var ErrMaxWatchesReached = errorString("max watches reached")

type errorString string

func (e errorString) Error() string { return string(e) }
