// Package tts wraps the OpenAI Audio (text-to-speech) and Chat Completions
// APIs to build multi-voice audio summaries. It has no dependency on the
// office or web packages so it can be unit-tested in isolation.
package tts

// Voices is the OpenAI gpt-4o-mini-tts built-in voice allowlist, ordered so
// the default rotation leads with marin and cedar (OpenAI's recommended
// best-quality voices). This list is the single Go authority for default
// assignment and PUT validation; it is mirrored in
// internal/web/spa/src/lib/voices.ts and kept in sync by a drift test.
var Voices = []string{
	"marin", "cedar", "alloy", "ash", "ballad", "coral", "echo",
	"fable", "nova", "onyx", "sage", "shimmer", "verse",
}

// IsValidVoice reports whether name is one of the built-in voices.
func IsValidVoice(name string) bool {
	for _, v := range Voices {
		if v == name {
			return true
		}
	}
	return false
}

// DefaultVoiceForIndex returns a stable voice for the i-th participant,
// wrapping if a team exceeds the allowlist size.
func DefaultVoiceForIndex(i int) string {
	if i < 0 {
		i = 0
	}
	return Voices[i%len(Voices)]
}
