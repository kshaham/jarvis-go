package sse

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// EventMessage is a pre-serialized SSE event ready for delivery.
type EventMessage struct {
	Event string
	Data  []byte // Pre-serialized JSON
}

// InitDataFunc returns the initial state to send on SSE connect.
type InitDataFunc func() interface{}

// Broker manages SSE client connections and broadcasts events.
type Broker struct {
	mu             sync.RWMutex
	clients        map[*Client]struct{}
	maxClients     int
	sessionClients map[string]int // sessionID -> count
	maxPerSession  int
	getInitData    InitDataFunc
}

// NewBroker creates a new SSE broker.
func NewBroker(maxClients, maxPerSession int) *Broker {
	return &Broker{
		clients:        make(map[*Client]struct{}),
		maxClients:     maxClients,
		sessionClients: make(map[string]int),
		maxPerSession:  maxPerSession,
	}
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

	client := NewClient(w, flusher, r.Context(), subscriptions)
	b.clients[client] = struct{}{}
	if sessionID != "" {
		b.sessionClients[sessionID]++
	}
	clientCount := len(b.clients)
	b.mu.Unlock()

	slog.Info("SSE client connected", "clients", clientCount, "subscriptions", subscriptions)

	// Send init event so frontend knows connection is established
	var initPayload interface{}
	if b.getInitData != nil {
		initPayload = b.getInitData()
	} else {
		initPayload = map[string]interface{}{"type": "init", "sessions": []interface{}{}, "version": "0.2.0-go"}
	}
	initData, _ := json.Marshal(initPayload)
	client.Send(EventMessage{Event: "init", Data: initData})

	// Block until client disconnects
	<-client.Done()

	b.mu.Lock()
	delete(b.clients, client)
	if sessionID != "" {
		b.sessionClients[sessionID]--
		if b.sessionClients[sessionID] <= 0 {
			delete(b.sessionClients, sessionID)
		}
	}
	b.mu.Unlock()
}

// Broadcast sends an event to all matching clients.
// Serializes data once, then fans out via non-blocking channel sends.
func (b *Broker) Broadcast(event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		slog.Error("SSE: failed to serialize event", "event", event, "error", err)
		return
	}

	msg := EventMessage{Event: event, Data: jsonData}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for client := range b.clients {
		if client.MatchesSubscription(event) {
			client.Send(msg)
		}
	}
}

// BroadcastPreSerialized sends a pre-serialized event to all matching clients.
func (b *Broker) BroadcastPreSerialized(msg EventMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for client := range b.clients {
		if client.MatchesSubscription(msg.Event) {
			client.Send(msg)
		}
	}
}

// ClientCount returns the number of connected clients.
// SetInitDataFunc sets the function called to get initial state for new SSE clients.
func (b *Broker) SetInitDataFunc(fn InitDataFunc) {
	b.getInitData = fn
}

func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
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
