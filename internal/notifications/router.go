package notifications

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kshaham/jarvis/internal/metrics"
)

// dispatcherWorkers bounds how many concurrent channel sends can be in
// flight at once. Low because each one issues network I/O; a burst of SSE
// events shouldn't cause the process to open hundreds of outbound sockets.
const dispatcherWorkers = 8

// Router evaluates persisted Routes against each event and fans out to
// matching channels. Callers pass in the Store (for route lookup) and a
// TelegramSender (for telegram channel dispatch); the router itself stays
// loose of web-package imports.
type Router struct {
	store    *Store
	telegram TelegramSender

	httpClient *http.Client

	work chan dispatchJob
	wg   sync.WaitGroup

	ctx     context.Context
	cancel  context.CancelFunc
	stopped atomic.Bool // set by Stop(); OnEvent early-returns once true
}

type dispatchJob struct {
	route Route
	spec  ChannelSpec
	d     Dispatch
}

// NewRouter builds the router but does not start its workers.
func NewRouter(store *Store, telegram TelegramSender) *Router {
	return &Router{
		store:      store,
		telegram:   telegram,
		httpClient: &http.Client{Timeout: webhookTimeout},
		work:       make(chan dispatchJob, 256),
	}
}

// Start launches the worker pool. Call Stop to drain and shutdown.
func (r *Router) Start(ctx context.Context) {
	r.ctx, r.cancel = context.WithCancel(ctx)
	for i := 0; i < dispatcherWorkers; i++ {
		r.wg.Add(1)
		go r.worker()
	}
}

// Stop cancels in-flight work and waits up to 5s for workers to exit.
func (r *Router) Stop() {
	r.stopped.Store(true)
	if r.cancel != nil {
		r.cancel()
	}
	// Do NOT close(r.work): OnEvent runs SYNCHRONOUSLY inside broker.Broadcast on
	// arbitrary goroutines (idle monitor, session forwarders, in-flight handlers)
	// that can still fire during shutdown, and a send on a closed channel panics.
	// Workers exit on ctx.Done; OnEvent early-returns once stopped is set.
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		slog.Warn("notifications: workers did not exit in 5s")
	}
}

// OnEvent is wired into Broker.AddPostBroadcastHook. Cheap: evaluates each
// route and drops a dispatch job on the queue for any matching channel.
// Returns immediately; actual I/O happens in worker goroutines.
func (r *Router) OnEvent(event string, data json.RawMessage) {
	if r.stopped.Load() {
		return
	}
	// Fix 1a: skip ALL per-event work (unmarshal, snapshot, sort) when no
	// routes are registered. The atomic read is a few nanoseconds; the work
	// it avoids is O(routes) allocations + sort + json.Unmarshal.
	if r.store.Len() == 0 {
		return
	}

	// Fix 1c: gate the unmarshal behind the count>0 check above so it only
	// runs when at least one route exists. Errors are non-fatal -- we still
	// match on event type even if the payload doesn't unmarshal -- but we log
	// them so corrupt broker payloads are discoverable rather than silent.
	var payload map[string]interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			slog.Debug("notifications: payload unmarshal failed", "event", event, "err", err)
		}
	}

	sessionID := extractSessionID(payload)
	now := time.Now()
	// Fix 1b: use routesSnapshot (unsorted) instead of List (sorted). Matching
	// does not require order; sort only matters for the operator UI list.
	for _, route := range r.store.routesSnapshot() {
		if !route.Enabled {
			continue
		}
		if !matchEvent(route.Match, event, sessionID) {
			continue
		}
		d := Dispatch{
			Event:     event,
			Data:      payload,
			RouteID:   route.ID,
			RouteName: route.Name,
			Timestamp: now,
		}
		for _, spec := range route.Channels {
			select {
			case r.work <- dispatchJob{route: *route, spec: spec, d: d}:
			default:
				// Queue full -- drop rather than block the SSE hot path.
				metrics.NotificationsDispatched.WithLabelValues(spec.Type, "dropped").Inc()
				slog.Warn("notifications: queue full, dropped",
					"route", route.Name, "channel", spec.Type)
			}
		}
	}
}

// Test fires a synthetic event through a specific route. Used by the
// /test endpoint so operators can verify a new route without waiting
// for a real event.
func (r *Router) Test(routeID string, event string, payload interface{}) error {
	route := r.store.Get(routeID)
	if route == nil {
		return ErrNotFound
	}
	data, _ := json.Marshal(payload)
	r.OnEvent(event, data)
	return nil
}

func (r *Router) worker() {
	defer r.wg.Done()
	for {
		select {
		case job := <-r.work:
			r.dispatch(job)
		case <-r.ctx.Done():
			return
		}
	}
}

// dispatch runs a single channel send, recovering from panics so a single
// buggy webhook target can't take down the worker pool.
func (r *Router) dispatch(job dispatchJob) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("notifications: dispatch panicked",
				"route", job.route.Name, "channel", job.spec.Type,
				"panic", rec, "stack", string(debug.Stack()))
			metrics.NotificationsDispatched.WithLabelValues(job.spec.Type, "panic").Inc()
		}
	}()
	var err error
	switch job.spec.Type {
	case ChannelTelegram:
		err = sendTelegram(r.telegram, job.spec, job.d)
	case ChannelWebhook:
		err = sendWebhook(r.ctx, r.httpClient, job.spec, job.d)
	case ChannelLog:
		err = sendLog(job.spec, job.d)
	case ChannelExpoPush:
		err = sendExpoPush(r.ctx, r.httpClient, job.spec, job.d)
	default:
		err = errUnknownChannel(job.spec.Type)
	}
	status := "ok"
	if err != nil {
		status = "error"
		slog.Warn("notifications: dispatch failed",
			"route", job.route.Name, "channel", job.spec.Type, "error", err)
	}
	metrics.NotificationsDispatched.WithLabelValues(job.spec.Type, status).Inc()
}

// matchEvent returns true if the event + session pair satisfies the rule.
// Pattern language is limited to exact match + trailing wildcard.
func matchEvent(rule MatchRule, event, sessionID string) bool {
	if !matchPattern(rule.EventPattern, event) {
		return false
	}
	if len(rule.SessionIDs) == 0 {
		return true
	}
	for _, id := range rule.SessionIDs {
		if id == sessionID {
			return true
		}
	}
	return false
}

func matchPattern(pattern, event string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(event, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == event
}

// extractSessionID reads the payload's "id" or "sessionId" field -- both
// are used across the event schema (session:* emits {id}, office:* emits
// {sessionId}). Returns empty string if neither present.
func extractSessionID(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["sessionId"].(string); ok {
		return v
	}
	if v, ok := payload["id"].(string); ok {
		return v
	}
	return ""
}

type unknownChannelErr string

func (e unknownChannelErr) Error() string { return "unknown channel type: " + string(e) }
func errUnknownChannel(s string) error    { return unknownChannelErr(s) }
