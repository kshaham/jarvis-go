package pty

import "testing"

func TestStripANSI_Colors(t *testing.T) {
	input := "\x1b[32mGreen text\x1b[0m"
	expected := "Green text"
	if got := StripANSI(input); got != expected {
		t.Errorf("StripANSI(%q) = %q, want %q", input, got, expected)
	}
}

func TestStripANSI_CursorMovement(t *testing.T) {
	input := "\x1b[2J\x1b[HHello"
	expected := "Hello"
	if got := StripANSI(input); got != expected {
		t.Errorf("StripANSI(%q) = %q, want %q", input, got, expected)
	}
}

func TestStripANSI_OSC(t *testing.T) {
	input := "\x1b]0;Window Title\x07Normal text"
	expected := "Normal text"
	if got := StripANSI(input); got != expected {
		t.Errorf("StripANSI(%q) = %q, want %q", input, got, expected)
	}
}

func TestStripANSI_Empty(t *testing.T) {
	if got := StripANSI(""); got != "" {
		t.Errorf("StripANSI empty = %q", got)
	}
}

func TestStripANSI_NoEscapes(t *testing.T) {
	input := "Plain text"
	if got := StripANSI(input); got != input {
		t.Errorf("StripANSI(%q) = %q", input, got)
	}
}

func TestFilterFocusSequences(t *testing.T) {
	input := "\x1b[?1004hSome text\x1b[I\x1b[O"
	expected := "Some text"
	if got := FilterFocusSequences(input); got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestFilterFocusSequences_NoMatch(t *testing.T) {
	input := "Normal text"
	if got := FilterFocusSequences(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestFilterCtrlL(t *testing.T) {
	input := "before\x0cafter"
	expected := "beforeafter"
	if got := FilterCtrlL(input); got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func BenchmarkStripANSI(b *testing.B) {
	input := "\x1b[32mColored \x1b[1mbold\x1b[0m text with \x1b]0;Title\x07 sequences"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StripANSI(input)
	}
}
