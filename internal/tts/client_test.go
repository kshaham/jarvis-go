package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateSpeechReturnsBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["voice"] != "marin" {
			t.Errorf("voice = %v", body["voice"])
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte{0xFF, 0xFB, 0x00})
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL)
	got, err := c.CreateSpeech(context.Background(), SpeechRequest{Voice: "marin", Input: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 bytes, got %d", len(got))
	}
}

func TestCondenseScriptParsesSegments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"intro":"Here is the standup recap.","segments":[{"sessionId":"a","text":"Alice shipped the parser."}]}`}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("k", srv.URL)
	script, err := c.CondenseScript(context.Background(), "Alice: shipped the parser", 280)
	if err != nil {
		t.Fatal(err)
	}
	if script.Intro == "" || len(script.Segments) != 1 || script.Segments[0].SessionID != "a" {
		t.Fatalf("bad script: %+v", script)
	}
}

func TestNoAPIKey(t *testing.T) {
	c := NewClient("", "http://unused")
	_, err := c.CreateSpeech(context.Background(), SpeechRequest{Voice: "marin", Input: "x"})
	if err != ErrNoAPIKey {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
	_ = io.Discard
}
