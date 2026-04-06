package sse

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	b := NewBroker(100, 10)
	if b.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", b.ClientCount())
	}
}

func TestBroker_Broadcast(t *testing.T) {
	b := NewBroker(100, 10)
	// Broadcast with no clients should not panic
	b.Broadcast("test:event", map[string]string{"key": "value"})
}

func TestParseSubscriptions(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"session:*", 1},
		{"session:*,office:*", 2},
		{"  session:*  ,  office:*  ", 2},
	}
	for _, tc := range tests {
		result := parseSubscriptions(tc.input)
		if tc.expected == 0 && result != nil {
			t.Errorf("parseSubscriptions(%q) = %v, want nil", tc.input, result)
		}
		if tc.expected > 0 && len(result) != tc.expected {
			t.Errorf("parseSubscriptions(%q) len=%d, want %d", tc.input, len(result), tc.expected)
		}
	}
}

func TestIsCriticalEvent(t *testing.T) {
	if !IsCriticalEvent("session:created") {
		t.Error("session:created should be critical")
	}
	if IsCriticalEvent("session:output") {
		t.Error("session:output should not be critical")
	}
}

func TestBroker_Subscribe_MaxClients(t *testing.T) {
	b := NewBroker(1, 10)

	// Create a fake SSE client
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events", nil)

	// Subscribe in background (it blocks)
	done := make(chan struct{})
	go func() {
		b.Subscribe(recorder, req)
		close(done)
	}()

	// Give it a moment to register
	time.Sleep(50 * time.Millisecond)

	// Second client should be rejected
	recorder2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/events", nil)

	// This will return immediately with 503
	b.Subscribe(recorder2, req2)
	if recorder2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", recorder2.Code)
	}
}
