package tts

import "strings"

// TranscriptEntry mirrors office.StandupMessage but keeps tts free of an
// office import. The handler maps StandupMessage -> TranscriptEntry.
type TranscriptEntry struct {
	SessionID   string
	SpeakerName string
	Type        string // opening | status_report | closing | takeaway
	Content     string
}

// SpeechInput is one speaker's merged, budgeted text to be voiced.
type SpeechInput struct {
	SessionID   string
	SpeakerName string
	Text        string
}

// nonResponsePlaceholder reports transcript content that is a timed-out or
// offline placeholder rather than a real status report.
func nonResponsePlaceholder(content string) bool {
	return strings.Contains(content, "did not respond in time") ||
		strings.TrimSpace(content) == "(offline - skipped)"
}

// BuildSpeechInputs filters the ordered transcript to real agent reports,
// groups entries by speaker (preserving first-seen order), and trims the
// merged text so the total word count stays within totalBudget.
func BuildSpeechInputs(entries []TranscriptEntry, totalBudget int) []SpeechInput {
	order := make([]string, 0)
	byID := make(map[string]*SpeechInput)
	for _, e := range entries {
		if e.Type != "status_report" && e.Type != "takeaway" {
			continue
		}
		if e.SessionID == "" || e.SessionID == "system" {
			continue
		}
		if nonResponsePlaceholder(e.Content) {
			continue
		}
		text := strings.TrimSpace(e.Content)
		if text == "" {
			continue
		}
		si, ok := byID[e.SessionID]
		if !ok {
			si = &SpeechInput{SessionID: e.SessionID, SpeakerName: e.SpeakerName}
			byID[e.SessionID] = si
			order = append(order, e.SessionID)
		}
		if si.Text == "" {
			si.Text = text
		} else {
			si.Text += " " + text
		}
	}

	out := make([]SpeechInput, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return trimToBudget(out, totalBudget)
}

// trimToBudget caps the total word count across all speakers, distributing a
// per-speaker share and truncating on a word boundary. Speakers left with
// zero words are dropped.
func trimToBudget(in []SpeechInput, totalBudget int) []SpeechInput {
	if totalBudget <= 0 || len(in) == 0 {
		return in
	}
	perSpeaker := totalBudget / len(in)
	if perSpeaker < 1 {
		perSpeaker = 1
	}
	out := make([]SpeechInput, 0, len(in))
	used := 0
	for _, s := range in {
		words := strings.Fields(s.Text)
		limit := perSpeaker
		if remaining := totalBudget - used; remaining < limit {
			limit = remaining
		}
		if limit <= 0 {
			break
		}
		if len(words) > limit {
			words = words[:limit]
		}
		s.Text = strings.Join(words, " ")
		used += len(words)
		if s.Text != "" {
			out = append(out, s)
		}
	}
	return out
}

// ConcatMP3 joins MP3 byte streams produced by the same model/format. All
// segments share encoder params, so frame-stream concatenation plays in every
// browser.
func ConcatMP3(segments [][]byte) []byte {
	total := 0
	for _, s := range segments {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range segments {
		out = append(out, s...)
	}
	return out
}
