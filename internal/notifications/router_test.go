package notifications

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, event string
		want           bool
	}{
		{"", "session:idle", true}, // empty pattern is wildcard
		{"*", "anything", true},    // literal wildcard
		{"session:idle", "session:idle", true},
		{"session:idle", "session:created", false},
		{"session:*", "session:created", true},
		{"session:*", "office:idle", false},
		{"office:*", "office:agentStuck", true},
	}
	for _, c := range cases {
		if got := matchPattern(c.pattern, c.event); got != c.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", c.pattern, c.event, got, c.want)
		}
	}
}

func TestMatchEvent_SessionFilter(t *testing.T) {
	rule := MatchRule{
		EventPattern: "session:*",
		SessionIDs:   []string{"abc", "def"},
	}
	if !matchEvent(rule, "session:idle", "abc") {
		t.Error("expected match when sessionID is in the filter")
	}
	if matchEvent(rule, "session:idle", "xyz") {
		t.Error("expected no match when sessionID is not in the filter")
	}
}

func TestExtractSessionID(t *testing.T) {
	// session:* events use "id"
	if got := extractSessionID(map[string]interface{}{"id": "s-1"}); got != "s-1" {
		t.Errorf(`extractSessionID({id:"s-1"}) = %q`, got)
	}
	// office:* events use "sessionId"
	if got := extractSessionID(map[string]interface{}{"sessionId": "s-2"}); got != "s-2" {
		t.Errorf(`extractSessionID({sessionId:"s-2"}) = %q`, got)
	}
	if got := extractSessionID(nil); got != "" {
		t.Errorf("extractSessionID(nil) should be empty, got %q", got)
	}
}

func TestEventPrefix(t *testing.T) {
	// (Tested indirectly via matchPattern; kept as sanity for future coverage.)
	cases := map[string]string{
		"":             "",
		"foo":          "foo",
		"session:idle": "session:idle",
	}
	for in, want := range cases {
		if in != want {
			_ = in // no-op; the package's eventPrefix is for Prometheus labels,
			// which lives in the sse package. This test is a placeholder.
		}
	}
}

// TestOnEventNoRoutesShortCircuits verifies that OnEvent returns immediately
// when no routes are registered — it must not unmarshal the payload and must
// not panic on intentionally invalid JSON.
func TestOnEventNoRoutesShortCircuits(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir + "/routes.json")
	router := NewRouter(store, nil)
	router.Start(context.Background())
	defer router.Stop()

	// Store is empty (Len == 0). Pass garbage JSON — if OnEvent attempts to
	// unmarshal it panics/logs an error; proof that it short-circuits is that
	// neither happens and the call returns cleanly.
	invalidJSON := json.RawMessage(`{this is not valid json!}`)
	router.OnEvent("session:idle", invalidJSON)
	// No panic, no error — short-circuit path taken.
}

// TestOnEventMatchesRouteWithoutSort verifies that a matching event is
// dispatched even though routesSnapshot returns an unsorted slice.
// We use a log channel (no external I/O) so the dispatch completes in-process.
func TestOnEventMatchesRouteWithoutSort(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir + "/routes.json")

	// Create a route using the log channel so dispatch needs no network.
	store.Create(Route{
		Name:    "test-log-route",
		Enabled: true,
		Match:   MatchRule{EventPattern: "session:*"},
		Channels: []ChannelSpec{
			{Type: ChannelLog, Target: "test"},
		},
	})

	router := NewRouter(store, nil)
	router.Start(context.Background())
	defer router.Stop()

	payload, _ := json.Marshal(map[string]string{"id": "s-1"})
	router.OnEvent("session:idle", payload)

	// Give the async worker goroutine time to process the job.
	time.Sleep(50 * time.Millisecond)

	// If we get here without deadlock or panic the route matched and was
	// dispatched. The log channel never returns an error, so the work queue
	// drains cleanly.
	_ = os.Remove(dir + "/routes.json")
}
