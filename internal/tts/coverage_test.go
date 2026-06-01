package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClientDefaultBaseURL covers the empty-baseURL default branch.
func TestNewClientDefaultBaseURL(t *testing.T) {
	c := NewClient("k", "")
	if c.baseURL != defaultBaseURL {
		t.Fatalf("expected default base URL, got %q", c.baseURL)
	}
}

// TestCreateSpeechWithInstructions covers the optional-instructions branch.
func TestCreateSpeechWithInstructions(t *testing.T) {
	var gotInstructions any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotInstructions = body["instructions"]
		_, _ = w.Write([]byte{0xFF})
	}))
	defer srv.Close()
	c := NewClient("k", srv.URL)
	_, err := c.CreateSpeech(context.Background(), SpeechRequest{Voice: "marin", Input: "hi", Instructions: "cheerful"})
	if err != nil {
		t.Fatal(err)
	}
	if gotInstructions != "cheerful" {
		t.Fatalf("expected instructions forwarded, got %v", gotInstructions)
	}
}

// TestCreateSpeechHTTPError covers post's non-2xx error path (and that the
// returned error is surfaced through CreateSpeech).
func TestCreateSpeechHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// A realistically-shaped OpenAI key (sk- + 40 chars) so logscrub matches.
		_, _ = w.Write([]byte("Incorrect API key provided: sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD"))
	}))
	defer srv.Close()
	c := NewClient("k", srv.URL)
	_, err := c.CreateSpeech(context.Background(), SpeechRequest{Voice: "marin", Input: "hi"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
	// The echoed key must be scrubbed out of the surfaced error.
	if strings.Contains(err.Error(), "sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD") {
		t.Fatalf("error leaked the API key: %v", err)
	}
	if !strings.Contains(err.Error(), "sk-[REDACTED]") {
		t.Fatalf("expected key to be redacted, got %v", err)
	}
}

// TestPostDoError covers the http.Do failure path (connection refused).
func TestPostDoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // close so the next request is refused
	c := NewClient("k", url)
	if _, err := c.CreateSpeech(context.Background(), SpeechRequest{Voice: "marin", Input: "hi"}); err == nil {
		t.Fatal("expected a transport error")
	}
}

// TestCondenseScriptNoAPIKey covers the missing-key guard for CondenseScript.
func TestCondenseScriptNoAPIKey(t *testing.T) {
	c := NewClient("", "http://unused")
	if _, err := c.CondenseScript(context.Background(), "x", 100); err != ErrNoAPIKey {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

// TestCondenseScriptDecodeErrors covers the three decode/empty failure paths.
func TestCondenseScriptDecodeErrors(t *testing.T) {
	cases := []struct {
		name, body, wantSub string
	}{
		{"bad envelope", "not json", "decode chat response"},
		{"no choices", `{"choices":[]}`, "no choices"},
		{"bad inner json", `{"choices":[{"message":{"content":"not-json"}}]}`, "decode script JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := NewClient("k", srv.URL)
			_, err := c.CondenseScript(context.Background(), "t", 100)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}

// TestDefaultVoiceForIndexNegative covers the i<0 clamp.
func TestDefaultVoiceForIndexNegative(t *testing.T) {
	if got := DefaultVoiceForIndex(-3); got != Voices[0] {
		t.Fatalf("expected first voice for negative index, got %q", got)
	}
}

// TestTrimToBudgetEdges covers the zero-budget short-circuit, the per-speaker
// floor of 1, and the running-remaining truncation.
func TestTrimToBudgetEdges(t *testing.T) {
	in := []SpeechInput{
		{SessionID: "a", Text: "one two three"},
		{SessionID: "b", Text: "four five six"},
		{SessionID: "c", Text: "seven eight nine"},
	}
	// Zero budget returns the input untouched.
	if got := trimToBudget(in, 0); len(got) != 3 {
		t.Fatalf("zero budget should pass through, got %d", len(got))
	}
	// Tiny budget (< number of speakers) forces perSpeaker=1 and exhausts the
	// remaining budget, dropping later speakers.
	got := trimToBudget(in, 2)
	words := 0
	for _, s := range got {
		words += len(strings.Fields(s.Text))
	}
	if words > 2 {
		t.Fatalf("expected total words capped at 2, got %d (%v)", words, got)
	}
}

// TestBuildSpeechInputsSkips covers the empty/system sessionID skip and the
// whitespace-only content skip.
func TestBuildSpeechInputsSkips(t *testing.T) {
	entries := []TranscriptEntry{
		{SessionID: "", Type: "status_report", Content: "no id"},
		{SessionID: "system", Type: "status_report", Content: "system noise"},
		{SessionID: "a", Type: "status_report", Content: "   "}, // whitespace only
		{SessionID: "b", Type: "status_report", Content: "real report here"},
	}
	out := BuildSpeechInputs(entries, 100)
	if len(out) != 1 || out[0].SessionID != "b" {
		t.Fatalf("expected only speaker b, got %#v", out)
	}
}
