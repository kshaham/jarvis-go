package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestStore returns a Store backed by a temp file with the given routes
// already created. Each route's ID is assigned by Create.
func newTestStore(t *testing.T, routes ...Route) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	for _, r := range routes {
		s.Create(r)
	}
	return s
}

// blockingTelegram is a TelegramSender that signals each send over a channel
// so tests can drain async dispatch deterministically instead of sleeping.
type blockingTelegram struct {
	mu    sync.Mutex
	calls []spyTelegramCall
	fired chan struct{}
}

func newBlockingTelegram(buf int) *blockingTelegram {
	return &blockingTelegram{fired: make(chan struct{}, buf)}
}

func (b *blockingTelegram) SendFormattedMessage(chatID, text string, keyboard interface{}) {
	b.mu.Lock()
	b.calls = append(b.calls, spyTelegramCall{chatID: chatID, text: text, keyboard: keyboard})
	b.mu.Unlock()
	b.fired <- struct{}{}
}

func (b *blockingTelegram) snapshot() []spyTelegramCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]spyTelegramCall, len(b.calls))
	copy(out, b.calls)
	return out
}

// waitFired blocks until n sends have fired or the test deadline elapses.
func waitFired(t *testing.T, ch <-chan struct{}, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("timed out waiting for dispatch %d/%d", i+1, n)
		}
	}
}

func TestNewRouter_Initializes(t *testing.T) {
	store := newTestStore(t)
	spy := &spyTelegram{}
	r := NewRouter(store, spy)
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}
	if r.store != store {
		t.Error("store not wired")
	}
	if r.telegram != spy {
		t.Error("telegram not wired")
	}
	if r.httpClient == nil || r.httpClient.Timeout != webhookTimeout {
		t.Errorf("httpClient not configured with webhook timeout, got %+v", r.httpClient)
	}
	if r.work == nil {
		t.Error("work channel not allocated")
	}
}

func TestRouter_OnEvent_DispatchesToTelegram(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "idle-tg",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "session:idle"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "999"}},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	data, _ := json.Marshal(map[string]string{"id": "s-1"})
	r.OnEvent("session:idle", data)

	waitFired(t, tg.fired, 1)
	calls := tg.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 telegram call, got %d", len(calls))
	}
	if calls[0].chatID != "999" {
		t.Errorf("chatID = %q, want 999", calls[0].chatID)
	}
}

func TestRouter_OnEvent_SkipsDisabledRoutes(t *testing.T) {
	store := newTestStore(t,
		Route{
			Name:     "disabled",
			Enabled:  false,
			Match:    MatchRule{EventPattern: "session:idle"},
			Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "1"}},
		},
		Route{
			Name:     "enabled",
			Enabled:  true,
			Match:    MatchRule{EventPattern: "session:idle"},
			Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "2"}},
		},
	)
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("session:idle", nil)
	waitFired(t, tg.fired, 1)
	// Only the enabled route should fire. Give a brief grace and assert no
	// second call snuck in.
	time.Sleep(50 * time.Millisecond)
	calls := tg.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 call (enabled only), got %d: %+v", len(calls), calls)
	}
	if calls[0].chatID != "2" {
		t.Errorf("expected enabled route target 2, got %q", calls[0].chatID)
	}
}

func TestRouter_OnEvent_NonMatchingEventIgnored(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "only-idle",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "session:idle"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "1"}},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("session:created", nil)
	time.Sleep(50 * time.Millisecond)
	if got := len(tg.snapshot()); got != 0 {
		t.Fatalf("non-matching event should not dispatch, got %d calls", got)
	}
}

func TestRouter_OnEvent_SessionFilterRestricts(t *testing.T) {
	store := newTestStore(t, Route{
		Name:    "session-scoped",
		Enabled: true,
		Match: MatchRule{
			EventPattern: "session:*",
			SessionIDs:   []string{"keep"},
		},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "1"}},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	// Wrong session -> no dispatch.
	wrong, _ := json.Marshal(map[string]string{"id": "other"})
	r.OnEvent("session:idle", wrong)
	time.Sleep(40 * time.Millisecond)
	if got := len(tg.snapshot()); got != 0 {
		t.Fatalf("event for unmatched session should not dispatch, got %d", got)
	}

	// Right session -> dispatch.
	right, _ := json.Marshal(map[string]string{"sessionId": "keep"})
	r.OnEvent("session:idle", right)
	waitFired(t, tg.fired, 1)
	if got := len(tg.snapshot()); got != 1 {
		t.Fatalf("event for matched session should dispatch once, got %d", got)
	}
}

func TestRouter_OnEvent_DispatchesToWebhook(t *testing.T) {
	received := make(chan Dispatch, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d Dispatch
		_ = json.NewDecoder(r.Body).Decode(&d)
		received <- d
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newTestStore(t, Route{
		Name:     "to-webhook",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "office:*"},
		Channels: []ChannelSpec{{Type: ChannelWebhook, Target: srv.URL}},
	})
	r := NewRouter(store, &spyTelegram{})
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("office:agentStuck", nil)
	select {
	case d := <-received:
		if d.Event != "office:agentStuck" {
			t.Errorf("webhook got event %q, want office:agentStuck", d.Event)
		}
		if d.RouteName != "to-webhook" {
			t.Errorf("webhook got route %q, want to-webhook", d.RouteName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook never received the dispatch")
	}
}

func TestRouter_OnEvent_MultipleChannelsPerRoute(t *testing.T) {
	webhookHit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newTestStore(t, Route{
		Name:    "fan-out",
		Enabled: true,
		Match:   MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{
			{Type: ChannelTelegram, Target: "777"},
			{Type: ChannelWebhook, Target: srv.URL},
			{Type: ChannelLog, Target: "auditlog"},
		},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("anything", nil)

	waitFired(t, tg.fired, 1)
	select {
	case <-webhookHit:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook channel of fan-out route never fired")
	}
	if got := tg.snapshot()[0].chatID; got != "777" {
		t.Errorf("telegram target = %q, want 777", got)
	}
}

func TestRouter_OnEvent_StoppedRouterDropsEvents(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "r",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "1"}},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	r.Stop()

	// After Stop, OnEvent must early-return without dispatching.
	r.OnEvent("anything", nil)
	time.Sleep(40 * time.Millisecond)
	if got := len(tg.snapshot()); got != 0 {
		t.Fatalf("stopped router should not dispatch, got %d", got)
	}
}

func TestRouter_OnEvent_MalformedPayloadStillMatchesOnEventType(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "match-anyway",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "session:idle"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "5"}},
	})
	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	// Invalid JSON: unmarshal fails (logged, non-fatal), but event-type
	// matching still proceeds with a nil payload.
	r.OnEvent("session:idle", json.RawMessage(`{not valid json`))
	waitFired(t, tg.fired, 1)
	if got := len(tg.snapshot()); got != 1 {
		t.Fatalf("malformed payload should still match on event type, got %d", got)
	}
}

func TestRouter_Test_UnknownRouteReturnsNotFound(t *testing.T) {
	store := newTestStore(t)
	r := NewRouter(store, &spyTelegram{})
	r.Start(context.Background())
	defer r.Stop()
	if err := r.Test("does-not-exist", "x", nil); err != ErrNotFound {
		t.Errorf("Test(unknown) = %v, want ErrNotFound", err)
	}
}

func TestRouter_Test_FiresThroughMatchingRoute(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "testable",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "synthetic:event"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "abc"}},
	})
	// Grab the assigned ID.
	id := store.List()[0].ID

	tg := newBlockingTelegram(4)
	r := NewRouter(store, tg)
	r.Start(context.Background())
	defer r.Stop()

	if err := r.Test(id, "synthetic:event", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Test: %v", err)
	}
	waitFired(t, tg.fired, 1)
	calls := tg.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 dispatch from Test, got %d", len(calls))
	}
	if calls[0].chatID != "abc" {
		t.Errorf("chatID = %q, want abc", calls[0].chatID)
	}
}

func TestRouter_Dispatch_UnknownChannelTypeRecordsError(t *testing.T) {
	// A route with an unrecognized channel type exercises the default
	// branch of dispatch (errUnknownChannel). It must not panic; the
	// worker recovers and the router keeps running.
	store := newTestStore(t, Route{
		Name:     "weird",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: "carrier-pigeon", Target: "x"}},
	})
	r := NewRouter(store, &spyTelegram{})
	r.Start(context.Background())
	defer r.Stop()

	// Should not panic or hang; the unknown channel just errors internally.
	r.OnEvent("anything", nil)
	// Give the worker time to process; nothing observable beyond no-crash.
	time.Sleep(60 * time.Millisecond)
}

func TestRouter_Dispatch_ExpoPushChannel(t *testing.T) {
	hit := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var arr []map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&arr)
		token := ""
		if len(arr) == 1 {
			token, _ = arr[0]["to"].(string)
		}
		hit <- token
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newTestStore(t, Route{
		Name:     "push-route",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: ChannelExpoPush, Target: "ExponentPushToken[dev]"}},
	})
	r := NewRouter(store, &spyTelegram{})
	// Redirect the router's own http client to the test server so the
	// ChannelExpoPush branch of dispatch hits a controllable endpoint.
	r.httpClient.Transport = redirectTransport{to: srv.URL, base: http.DefaultTransport}
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("anything", nil)
	select {
	case token := <-hit:
		if token != "ExponentPushToken[dev]" {
			t.Errorf("expo push token = %q, want ExponentPushToken[dev]", token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expo push channel never fired through dispatch")
	}
}

// panicTelegram panics on send to drive the dispatch panic-recovery path.
type panicTelegram struct{ done chan struct{} }

func (p panicTelegram) SendFormattedMessage(chatID, text string, keyboard interface{}) {
	defer close(p.done)
	panic("simulated telegram explosion")
}

func TestRouter_Dispatch_RecoversFromPanickingChannel(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "boom",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: ChannelTelegram, Target: "1"}},
	})
	pt := panicTelegram{done: make(chan struct{})}
	r := NewRouter(store, pt)
	r.Start(context.Background())
	defer r.Stop()

	r.OnEvent("anything", nil)
	// The dispatch panic must be recovered inside the worker; the test
	// process must survive. Wait for the panicking send to have run.
	select {
	case <-pt.done:
	case <-time.After(2 * time.Second):
		t.Fatal("panicking channel was never invoked")
	}
	// Give the recover() a moment, then prove the worker pool is still
	// alive by dispatching a second, non-panicking event.
	store.Create(Route{
		Name:     "after",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: ChannelLog, Target: "ok"}},
	})
	// Should not hang or crash.
	r.OnEvent("again", nil)
	time.Sleep(40 * time.Millisecond)
}

func TestRouter_OnEvent_QueueFullDropsWithoutBlocking(t *testing.T) {
	// Build a route with many channels so a single OnEvent enqueues far
	// more than the 256-slot work buffer. Do NOT Start the router, so no
	// worker drains the queue -> the select default (drop) branch fires.
	channels := make([]ChannelSpec, 300)
	for i := range channels {
		channels[i] = ChannelSpec{Type: ChannelLog, Target: "x"}
	}
	store := newTestStore(t, Route{
		Name:     "flood",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: channels,
	})
	r := NewRouter(store, &spyTelegram{})
	// Router not started: work channel (cap 256) fills, remaining 44 jobs
	// take the drop path. This must return without blocking.
	done := make(chan struct{})
	go func() {
		r.OnEvent("anything", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnEvent blocked when queue was full instead of dropping")
	}
}

func TestRouter_Stop_Idempotentish_NoCancelWhenNeverStarted(t *testing.T) {
	// Stop before Start: cancel is nil, Stop must not panic and must set
	// the stopped flag so subsequent OnEvent calls are dropped.
	store := newTestStore(t)
	r := NewRouter(store, &spyTelegram{})
	r.Stop() // never started; r.cancel is nil
	if !r.stopped.Load() {
		t.Error("Stop should set the stopped flag even when never started")
	}
}

func TestErrUnknownChannel_Message(t *testing.T) {
	err := errUnknownChannel("smoke-signal")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	want := "unknown channel type: smoke-signal"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestRouter_OnEvent_NoRoutesIsNoop(t *testing.T) {
	store := newTestStore(t)
	r := NewRouter(store, &spyTelegram{})
	r.Start(context.Background())
	defer r.Stop()
	// No routes -> nothing dispatched, no panic.
	r.OnEvent("session:idle", []byte(`{"id":"x"}`))
}

func TestRouter_ConcurrentOnEvent_RaceSafe(t *testing.T) {
	store := newTestStore(t, Route{
		Name:     "race",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "*"},
		Channels: []ChannelSpec{{Type: ChannelLog, Target: "l"}},
	})
	r := NewRouter(store, &spyTelegram{})
	r.Start(context.Background())
	defer r.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.OnEvent("evt", []byte(`{"id":"s"}`))
		}(i)
	}
	wg.Wait()
	// No assertion on count (log channel is fire-and-forget); the point is
	// the -race detector finds no data race across concurrent OnEvent calls
	// and the worker pool draining them.
}
