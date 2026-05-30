package tts

import "testing"

func TestVoicesListHas13(t *testing.T) {
	if len(Voices) != 13 {
		t.Fatalf("expected 13 voices, got %d", len(Voices))
	}
}

func TestIsValidVoice(t *testing.T) {
	if !IsValidVoice("marin") {
		t.Errorf("marin should be valid")
	}
	if IsValidVoice("definitely-not-a-voice") {
		t.Errorf("bogus voice should be invalid")
	}
	if IsValidVoice("") {
		t.Errorf("empty should be invalid")
	}
}

func TestDefaultVoiceForIndex(t *testing.T) {
	if got := DefaultVoiceForIndex(0); got != "marin" {
		t.Errorf("index 0 = %q, want marin", got)
	}
	if got := DefaultVoiceForIndex(1); got != "cedar" {
		t.Errorf("index 1 = %q, want cedar", got)
	}
	if got := DefaultVoiceForIndex(13); got != DefaultVoiceForIndex(0) {
		t.Errorf("index should wrap modulo len")
	}
	if !IsValidVoice(DefaultVoiceForIndex(99)) {
		t.Errorf("wrapped voice must be in allowlist")
	}
}
