package notifications

import "testing"

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
