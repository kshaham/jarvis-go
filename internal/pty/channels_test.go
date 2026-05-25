package pty

import (
	"reflect"
	"testing"
)

func TestSanitizeChannels_DropsMalformed(t *testing.T) {
	in := []string{
		"plugin:office-events@local",   // valid
		"plugin:my-plugin@reg.io",      // valid
		"",                             // empty -> dropped
		"PLUGIN:upper@bad",             // leading uppercase -> dropped
		"plugin:with spaces@bad",       // whitespace -> dropped
		"plugin:semis;injected@bad",    // shell meta -> dropped
		"plugin:another-valid@nested.example.org",
	}
	got := sanitizeChannels(in, "test")
	want := []string{
		"plugin:office-events@local",
		"plugin:my-plugin@reg.io",
		"plugin:another-valid@nested.example.org",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sanitizeChannels = %v, want %v", got, want)
	}
}

func TestSanitizeChannels_NilInputReturnsNil(t *testing.T) {
	if got := sanitizeChannels(nil, "test"); got != nil {
		t.Errorf("nil input should produce nil, got %v", got)
	}
}

func TestSanitizeChannels_AllValidPassthrough(t *testing.T) {
	in := []string{"plugin:a@b", "plugin:c@d"}
	got := sanitizeChannels(in, "test")
	if !reflect.DeepEqual(got, in) {
		t.Errorf("all-valid input mutated: %v", got)
	}
}

func TestSanitizeChannels_AcceptsSingleCharName(t *testing.T) {
	// P7.3.10: regression for the original `+` quantifier that
	// silently dropped any spec whose grammar reduced to a single
	// character past the leading lowercase letter.
	in := []string{"a", "x:y@z"}
	got := sanitizeChannels(in, "test")
	if len(got) != 2 {
		t.Errorf("expected both 1-char-ish specs preserved, got %v", got)
	}
}

func TestSanitizeChannels_AllInvalidProducesEmpty(t *testing.T) {
	in := []string{"", "BAD", "with spaces"}
	got := sanitizeChannels(in, "test")
	if len(got) != 0 {
		t.Errorf("all-invalid should produce empty slice, got %v", got)
	}
}
