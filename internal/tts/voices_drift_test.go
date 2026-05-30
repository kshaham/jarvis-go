package tts

import (
	"os"
	"regexp"
	"testing"
)

func TestVoicesMatchFrontend(t *testing.T) {
	data, err := os.ReadFile("../web/spa/src/lib/voices.ts")
	if err != nil {
		t.Fatalf("read voices.ts: %v", err)
	}
	// Extract only the single-quoted voice tokens from the VOICES array.
	re := regexp.MustCompile(`'([a-z]+)'`)
	var fe []string
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		fe = append(fe, m[1])
	}
	if len(fe) != len(Voices) {
		t.Fatalf("voice count mismatch: go=%d ts=%d (ts=%v)", len(Voices), len(fe), fe)
	}
	for i := range Voices {
		if fe[i] != Voices[i] {
			t.Fatalf("voice order mismatch at %d: go=%q ts=%q", i, Voices[i], fe[i])
		}
	}
}
