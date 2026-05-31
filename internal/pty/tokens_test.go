package pty

import (
	"strings"
	"testing"
)

func TestParseTokenCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		ok       bool
	}{
		{"123.4k tokens", 123400, true},
		{"5234 tokens", 5234, true},
		{"1.2M tokens", 1200000, true},
		{"1.2m tokens", 1200000, true},
		{"42K tokens", 42000, true},
		{"no tokens here", 0, false},
		{"0 tokens", 0, false},
		{"", 0, false},
		{"3000000 tokens", 0, false}, // exceeds MaxSessionTokens
	}

	for _, tc := range tests {
		count, ok := ParseTokenCount(tc.input)
		if ok != tc.ok {
			t.Errorf("ParseTokenCount(%q): ok=%v, want %v", tc.input, ok, tc.ok)
		}
		if ok && count != tc.expected {
			t.Errorf("ParseTokenCount(%q) = %d, want %d", tc.input, count, tc.expected)
		}
	}
}

// TestParseTokenCount_ParseFloatOverflow covers the ParseFloat error branch
// (tokens.go:23-25): the regex matches an arbitrarily long run of digits, and a
// number with hundreds of digits overflows float64 so strconv.ParseFloat
// returns ErrRange. ParseTokenCount must report (0, false) without panicking.
func TestParseTokenCount_ParseFloatOverflow(t *testing.T) {
	// 400-digit integer followed by " tokens" -> regex matches, ParseFloat
	// returns "value out of range".
	input := strings.Repeat("9", 400) + " tokens"
	count, ok := ParseTokenCount(input)
	if ok {
		t.Errorf("ParseTokenCount(<400 nines> tokens) = (%d, %v), want (0, false) on ParseFloat overflow", count, ok)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 when ParseFloat errors", count)
	}
}

func BenchmarkParseTokenCount(b *testing.B) {
	input := "IDLE  123.4k tokens  $0.42"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseTokenCount(input)
	}
}
