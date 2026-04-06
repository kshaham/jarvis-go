package pty

import "testing"

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

func BenchmarkParseTokenCount(b *testing.B) {
	input := "IDLE  123.4k tokens  $0.42"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseTokenCount(input)
	}
}
