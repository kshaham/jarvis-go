package sse

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kshaham/jarvis/internal/metrics"
)

// clientSlicePool reduces per-broadcast allocations for the fan-out slice
// built in matchingClients. Each pool entry is a *[]*Client whose backing
// array is reused across broadcasts. Callers borrow via matchingClients and
// must call putClientSlice to return the slice when fan-out is complete.
var clientSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]*Client, 0, 32)
		return &s
	},
}

// replayBufferSize bounds how many recent events are kept for replay to
// reconnecting clients. 200 covers ~60s of typical event throughput.
const replayBufferSize = 200

// EventMessage is a pre-serialized SSE event ready for delivery. The ID
// field is stamped by Broadcast (and BroadcastPreSerialized) and emitted
// as an `id:` line in the SSE frame so browsers include it on reconnect
// via the Last-Event-ID header.
type EventMessage struct {
	ID    uint64
	Event string
	Data  []byte // Pre-serialized JSON
}

// InitDataFunc returns the initial state to send on SSE connect.
type InitDataFunc func() interface{}

// Broker manages SSE client connections and broadcasts events. A small
// ring buffer of recent events enables replay on reconnect.
//
// The subscription index (exactIdx + prefixIdx + wildcardClients) lets
// Broadcast fan out in O(matching clients) instead of iterating every
// connected client and calling MatchesSubscription per event. At 100+ open
// tabs and 1000+ events/sec the difference is substantial.
type Broker struct {
	mu             sync.RWMutex
	clients        map[*Client]struct{}
	maxClients     int
	sessionClients map[string]int // sessionID -> count
	maxPerSession  int
	getInitData    InitDataFunc

	nextEventID atomic.Uint64

	// Circular replay buffer. replayHead is the index of the NEXT write.
	replayBuf   []EventMessage
	replayHead  int
	replayCount int

	// Subscription index maintained on client add/remove.
	//   exactIdx:        "session:created"  -> {clients subscribed exactly}
	//   prefixIdx:       "session:"         -> {clients subscribed to session:*}
	//   wildcardClients: clients with nil subscriptions (receive everything)
	exactIdx        map[string]map[*Client]struct{}
	prefixIdx       map[string]map[*Client]struct{}
	wildcardClients map[*Client]struct{}

	// postBroadcastHooks stores a []func(EventMessage) in an atomic.Value.
	// Hooks are registered at startup and never removed, so the Value is
	// written once (or a small number of times); reads on the broadcast hot
	// path are lock-free and allocation-free (no per-broadcast copy needed).
	// Fix 2b: replaced the per-broadcast slice copy with an atomic.Value load.
	postBroadcastHooks atomic.Value // stores []func(EventMessage)
}

// NewBroker creates a new SSE broker.
func NewBroker(maxClients, maxPerSession int) *Broker {
	return &Broker{
		clients:         make(map[*Client]struct{}),
		maxClients:      maxClients,
		sessionClients:  make(map[string]int),
		maxPerSession:   maxPerSession,
		replayBuf:       make([]EventMessage, replayBufferSize),
		exactIdx:        make(map[string]map[*Client]struct{}),
		prefixIdx:       make(map[string]map[*Client]struct{}),
		wildcardClients: make(map[*Client]struct{}),
	}
}

// indexClient registers a client in the subscription index based on its
// subscription patterns. Caller must hold b.mu.
func (b *Broker) indexClient(c *Client) {
	if c.subscriptions == nil {
		b.wildcardClients[c] = struct{}{}
		return
	}
	for _, sub := range c.subscriptions {
		if strings.HasSuffix(sub, "*") {
			prefix := strings.TrimSuffix(sub, "*")
			set, ok := b.prefixIdx[prefix]
			if !ok {
				set = make(map[*Client]struct{})
				b.prefixIdx[prefix] = set
			}
			set[c] = struct{}{}
		} else {
			set, ok := b.exactIdx[sub]
			if !ok {
				set = make(map[*Client]struct{})
				b.exactIdx[sub] = set
			}
			set[c] = struct{}{}
		}
	}
}

// unindexClient removes all entries for this client. Caller must hold b.mu.
func (b *Broker) unindexClient(c *Client) {
	delete(b.wildcardClients, c)
	for sub, set := range b.exactIdx {
		if _, ok := set[c]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(b.exactIdx, sub)
			}
		}
	}
	for prefix, set := range b.prefixIdx {
		if _, ok := set[c]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(b.prefixIdx, prefix)
			}
		}
	}
}

// matchingClients collects every client interested in `event` using the
// index. Returns a *[]*Client borrowed from clientSlicePool; the caller MUST
// return it via putClientSlice after fan-out so the backing array is reused.
// Caller must hold b.mu (RLock is fine).
//
// Fix 2a: the backing array is recycled via clientSlicePool. The dedup map
// (seen) is only allocated when multiple non-empty buckets overlap; in the
// common case (all clients are wildcard-only or subscription-only) no map is
// allocated.
func (b *Broker) matchingClients(event string) *[]*Client {
	pp := clientSlicePool.Get().(*[]*Client)
	out := (*pp)[:0] // reset length, keep backing array

	exactSet := b.exactIdx[event]
	var prefixSets []map[*Client]struct{}
	for prefix, set := range b.prefixIdx {
		if strings.HasPrefix(event, prefix) {
			prefixSets = append(prefixSets, set)
		}
	}

	// Build dedup set only when buckets can overlap. A client can appear in
	// both wildcardClients and exactIdx/prefixIdx (a wildcard client can also
	// match an exact subscription); without dedup we would double-deliver.
	// Correctness is non-negotiable.
	needDedup := (len(b.wildcardClients) > 0 && (len(exactSet) > 0 || len(prefixSets) > 0)) ||
		(len(exactSet) > 0 && len(prefixSets) > 0)

	if needDedup {
		capHint := len(b.wildcardClients) + len(exactSet)
		for _, ps := range prefixSets {
			capHint += len(ps)
		}
		seen := make(map[*Client]struct{}, capHint)
		for c := range b.wildcardClients {
			seen[c] = struct{}{}
			out = append(out, c)
		}
		for c := range exactSet {
			if _, dup := seen[c]; !dup {
				seen[c] = struct{}{}
				out = append(out, c)
			}
		}
		for _, ps := range prefixSets {
			for c := range ps {
				if _, dup := seen[c]; !dup {
					seen[c] = struct{}{}
					out = append(out, c)
				}
			}
		}
	} else {
		// No overlap possible — skip the dedup map.
		for c := range b.wildcardClients {
			out = append(out, c)
		}
		for c := range exactSet {
			out = append(out, c)
		}
		for _, ps := range prefixSets {
			for c := range ps {
				out = append(out, c)
			}
		}
	}

	*pp = out
	return pp
}

// putClientSlice returns a pooled container to the pool.
// Nils out stored pointers so the pool does not retain *Client references.
func putClientSlice(pp *[]*Client) {
	s := *pp
	for i := range s {
		s[i] = nil
	}
	*pp = s[:0]
	clientSlicePool.Put(pp)
}

// storeInReplayBuffer adds an event to the circular replay buffer.
// Caller must hold b.mu.
func (b *Broker) storeInReplayBuffer(msg EventMessage) {
	b.replayBuf[b.replayHead] = msg
	b.replayHead = (b.replayHead + 1) % replayBufferSize
	if b.replayCount < replayBufferSize {
		b.replayCount++
	}
}

// collectReplayEvents returns events with ID > lastSeenID, in order.
// Caller must hold b.mu (or an RLock since we only read).
func (b *Broker) collectReplayEvents(lastSeenID uint64) []EventMessage {
	if b.replayCount == 0 {
		return nil
	}
	out := make([]EventMessage, 0, b.replayCount)
	// Iterate oldest-first. The oldest index is replayHead when buffer is full,
	// or 0 when it's still filling.
	start := 0
	if b.replayCount == replayBufferSize {
		start = b.replayHead
	}
	for i := 0; i < b.replayCount; i++ {
		idx := (start + i) % replayBufferSize
		msg := b.replayBuf[idx]
		if msg.ID > lastSeenID {
			out = append(out, msg)
		}
	}
	return out
}

// Subscribe handles an SSE connection from an HTTP client.
// Blocks until the client disconnects.
func (b *Broker) Subscribe(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	subscriptions := parseSubscriptions(r.URL.Query().Get("subscribe"))
	sessionID := r.URL.Query().Get("session")

	// Snapshot the init-data function under a SHORT lock (fixes the data race
	// with SetInitDataFunc) and do the cap checks, but DON'T call getInitData or
	// add the client yet — getInitData is heavy (marshals settings, iterates all
	// PTY/SDK sessions + respawn controllers, each taking their own locks), so
	// running it under b.mu would block every Broadcast and create a
	// b.mu -> sessionManager.mu -> ... lock-ordering hazard.
	b.mu.Lock()
	if len(b.clients) >= b.maxClients {
		b.mu.Unlock()
		http.Error(w, "too many SSE clients", http.StatusServiceUnavailable)
		return
	}
	if sessionID != "" && b.sessionClients[sessionID] >= b.maxPerSession {
		b.mu.Unlock()
		http.Error(w, "too many SSE clients for this session", http.StatusServiceUnavailable)
		return
	}
	initFn := b.getInitData
	// Watermark captured BEFORE releasing the lock: any event broadcast during
	// the unlocked init-build window gets an ID greater than this, so replaying
	// from here (for a fresh client) recovers events that are neither in the
	// init snapshot nor delivered live (the client isn't broadcast-visible yet).
	// Captured early to guarantee no loss; a few events already reflected in
	// init may be replayed (harmless — frontend state updates are idempotent).
	snapshotID := b.nextEventID.Load()
	b.mu.Unlock()

	client := NewClient(w, flusher, r.Context(), subscriptions)

	// Build the init frame OUTSIDE b.mu.
	var initPayload interface{}
	if initFn != nil {
		initPayload = initFn()
	} else {
		initPayload = map[string]interface{}{"type": "init", "sessions": []interface{}{}, "version": "0.2.0-go"}
	}
	initData, _ := json.Marshal(initPayload)

	// Re-acquire b.mu to queue init + replay AND make the client broadcast-visible
	// atomically: client.Send is non-blocking (FIFO buffer), so queuing init +
	// replay before adding the client to b.clients/the index — all under the same
	// lock — guarantees the frontend sees init first, replayed ids in order, and
	// no live Broadcast interleaved ahead of init. Re-check caps since another
	// client may have connected during the unlock.
	b.mu.Lock()
	if len(b.clients) >= b.maxClients || (sessionID != "" && b.sessionClients[sessionID] >= b.maxPerSession) {
		b.mu.Unlock()
		http.Error(w, "too many SSE clients", http.StatusServiceUnavailable)
		return
	}
	client.Send(EventMessage{Event: "init", Data: initData})
	// Replay floor: a reconnecting client replays from its Last-Event-ID; a
	// fresh client replays from the init watermark so connect-window events
	// aren't lost. Either way collectReplayEvents only returns events still in
	// the bounded replay buffer, and the client isn't broadcast-visible yet, so
	// init/replay are queued strictly ahead of any live event.
	replayFloor := snapshotID
	if lastSeenID := parseLastEventID(r); lastSeenID > 0 {
		replayFloor = lastSeenID
	}
	var replayCount int
	for _, msg := range b.collectReplayEvents(replayFloor) { // b.mu held
		if client.MatchesSubscription(msg.Event) {
			client.Send(msg)
			replayCount++
		}
	}
	b.clients[client] = struct{}{}
	b.indexClient(client)
	if sessionID != "" {
		b.sessionClients[sessionID]++
	}
	clientCount := len(b.clients)
	b.mu.Unlock()

	metrics.SSEClients.Set(float64(clientCount))
	slog.Info("SSE client connected", "clients", clientCount, "subscriptions", subscriptions)
	if replayCount > 0 {
		slog.Info("SSE replayed missed events", "count", replayCount)
	}

	// Block until client disconnects
	<-client.Done()

	b.mu.Lock()
	delete(b.clients, client)
	b.unindexClient(client)
	if sessionID != "" {
		b.sessionClients[sessionID]--
		if b.sessionClients[sessionID] <= 0 {
			delete(b.sessionClients, sessionID)
		}
	}
	remaining := len(b.clients)
	b.mu.Unlock()
	metrics.SSEClients.Set(float64(remaining))
}

// CloseAllClients unblocks every connected SSE client so their in-flight
// Subscribe handlers return. Call this at the START of server shutdown, BEFORE
// http.Server.Shutdown -- Shutdown waits for handlers to return, and an SSE
// handler blocks on client.Done() until its connection drops, so without this
// Shutdown stalls for the entire shutdown-timeout whenever a dashboard is open.
// The client is removed from b.clients by its own Subscribe goroutine on return.
func (b *Broker) CloseAllClients() {
	b.mu.Lock()
	clients := make([]*Client, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

// Broadcast sends an event to all matching clients.
// Serializes data once, stamps with a unique ID, stores in the replay buffer,
// then fans out via non-blocking channel sends.
func (b *Broker) Broadcast(event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		slog.Error("SSE: failed to serialize event", "event", event, "error", err)
		return
	}

	msg := EventMessage{
		ID:    b.nextEventID.Add(1),
		Event: event,
		Data:  jsonData,
	}

	b.mu.Lock()
	b.storeInReplayBuffer(msg)
	pp := b.matchingClients(event) // returns pooled *[]*Client
	b.mu.Unlock()

	metrics.SSEEventsBroadcast.WithLabelValues(eventPrefix(event)).Inc()

	for _, c := range *pp {
		c.Send(msg)
	}
	putClientSlice(pp) // return backing array to pool

	// Fix 2b: read hooks via atomic.Value — no allocation, no lock.
	if v := b.postBroadcastHooks.Load(); v != nil {
		for _, h := range v.([]func(EventMessage)) {
			h(msg)
		}
	}
}

// eventPrefix extracts the colon-prefixed event family ("session", "office")
// so Prometheus labels stay bounded. Unknown events are bucketed as "other".
func eventPrefix(event string) string {
	if idx := strings.Index(event, ":"); idx > 0 {
		return event[:idx]
	}
	return "other"
}

// BroadcastPreSerialized sends a pre-serialized event to all matching clients.
// Stamps the event with a fresh ID and stores it in the replay buffer.
func (b *Broker) BroadcastPreSerialized(msg EventMessage) {
	if msg.ID == 0 {
		msg.ID = b.nextEventID.Add(1)
	}

	b.mu.Lock()
	b.storeInReplayBuffer(msg)
	pp := b.matchingClients(msg.Event) // returns pooled *[]*Client
	b.mu.Unlock()

	metrics.SSEEventsBroadcast.WithLabelValues(eventPrefix(msg.Event)).Inc()

	for _, c := range *pp {
		c.Send(msg)
	}
	putClientSlice(pp) // return backing array to pool

	// Fix 2b: read hooks via atomic.Value — no allocation, no lock.
	if v := b.postBroadcastHooks.Load(); v != nil {
		for _, h := range v.([]func(EventMessage)) {
			h(msg)
		}
	}
}

// AddPostBroadcastHook registers a function that fires after every broadcast
// (via either Broadcast or BroadcastPreSerialized). Hooks run outside the
// broker mutex so they can do arbitrary work without blocking other
// broadcasts, but they must NOT query broker state -- they get the event
// message as their input and nothing else.
//
// Fix 2b: hooks are stored in an atomic.Value so Broadcast reads them with a
// single load instead of copying the slice under b.mu on every broadcast.
// AddPostBroadcastHook is called only at startup (typically once) so the
// write path (append + Store) is not performance-sensitive.
func (b *Broker) AddPostBroadcastHook(fn func(EventMessage)) {
	b.mu.Lock()
	var prev []func(EventMessage)
	if v := b.postBroadcastHooks.Load(); v != nil {
		prev = v.([]func(EventMessage))
	}
	next := make([]func(EventMessage), len(prev)+1)
	copy(next, prev)
	next[len(prev)] = fn
	b.postBroadcastHooks.Store(next)
	b.mu.Unlock()
}

// ClientCount returns the number of connected clients.
// SetInitDataFunc sets the function called to get initial state for new SSE clients.
func (b *Broker) SetInitDataFunc(fn InitDataFunc) {
	// Under b.mu so the assignment is synchronized with Subscribe's read of
	// b.getInitData (both touch the same non-atomic interface field).
	b.mu.Lock()
	b.getInitData = fn
	b.mu.Unlock()
}

func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// parseLastEventID reads the Last-Event-ID header (set automatically by
// EventSource reconnects) or the ?lastEventId= query param fallback.
// Returns 0 if absent or malformed.
func parseLastEventID(r *http.Request) uint64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("lastEventId")
	}
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseSubscriptions(s string) []string {
	if s == "" {
		return nil // nil means all events
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Critical events that must be delivered (disconnect client if buffer full).
var criticalEvents = map[string]bool{
	"session:created": true, "session:deleted": true, "session:stopped": true,
	"respawn:started": true, "respawn:stopped": true, "respawn:blocked": true,
	"hook:received": true, "init": true,
	"office:memberJoined": true, "office:memberLeft": true,
	"office:standupStarted": true, "office:standupEnded": true,
}

// IsCriticalEvent returns true if the event must be delivered.
func IsCriticalEvent(event string) bool {
	return criticalEvents[event]
}
