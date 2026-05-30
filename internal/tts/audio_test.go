package tts

import (
	"strings"
	"testing"
)

func TestBuildSpeechInputsFiltersAndGroups(t *testing.T) {
	entries := []TranscriptEntry{
		{SessionID: "system", SpeakerName: "Standup", Type: "opening", Content: "Daily standup started."},
		{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: "Shipped the parser. Fixed two bugs."},
		{SessionID: "b", SpeakerName: "Bob", Type: "status_report", Content: "Bob did not respond in time."},
		{SessionID: "c", SpeakerName: "Cara", Type: "status_report", Content: "(offline - skipped)"},
		{SessionID: "a", SpeakerName: "Alice", Type: "takeaway", Content: "Next: integration tests."},
		{SessionID: "system", SpeakerName: "Standup", Type: "closing", Content: "That wraps it up."},
	}
	got := BuildSpeechInputs(entries, 1000)
	if len(got) != 1 {
		t.Fatalf("expected 1 speaker group (Alice), got %d: %+v", len(got), got)
	}
	if got[0].SessionID != "a" || got[0].SpeakerName != "Alice" {
		t.Fatalf("unexpected speaker: %+v", got[0])
	}
	if !strings.Contains(got[0].Text, "Shipped the parser") || !strings.Contains(got[0].Text, "integration tests") {
		t.Fatalf("text not merged in order: %q", got[0].Text)
	}
}

func TestBuildSpeechInputsBudget(t *testing.T) {
	long := strings.Repeat("word ", 500)
	entries := []TranscriptEntry{
		{SessionID: "a", SpeakerName: "Alice", Type: "status_report", Content: long},
		{SessionID: "b", SpeakerName: "Bob", Type: "status_report", Content: long},
	}
	got := BuildSpeechInputs(entries, 40)
	total := 0
	for _, s := range got {
		total += len(strings.Fields(s.Text))
	}
	if total > 40 {
		t.Fatalf("total words %d exceeds budget 40", total)
	}
}

func TestConcatMP3(t *testing.T) {
	a := []byte{0xFF, 0xFB, 0x01, 0x02}
	b := []byte{0xFF, 0xFB, 0x03, 0x04}
	out := ConcatMP3([][]byte{a, b})
	if len(out) != len(a)+len(b) {
		t.Fatalf("len = %d, want %d", len(out), len(a)+len(b))
	}
	frames := 0
	for i := 0; i+1 < len(out); i++ {
		if out[i] == 0xFF && (out[i+1]&0xE0) == 0xE0 {
			frames++
		}
	}
	if frames < 2 {
		t.Fatalf("expected >= 2 frame headers, found %d", frames)
	}
}
