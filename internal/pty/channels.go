package pty

import (
	"log/slog"
	"regexp"
)

// R4.3 hardening (P6.1) · channel spec filter. Channels are passed
// verbatim to Claude's `--channels` CLI flag; a malformed entry crashes
// the subprocess after the SDK is already mid-handshake, leaving the
// caller staring at a generic "session failed to start" error. We
// filter at session construction so the caller sees the drop in the
// log and the spawn proceeds with the valid subset.
//
// The grammar is intentionally lenient. Channels research preview spec
// at time of writing accepts forms like `plugin:office-events@local`
// and `plugin:my-plugin@registry.example.com`. The regex below admits
// any leading lowercase letter followed by URL-safe characters
// commonly used in plugin / marketplace specs.

// `*` (not `+`) on the trailing character class so 1-character names
// like `a:b@c` aren't silently rejected. P7.3.10 audit fix.
var channelSpecRe = regexp.MustCompile(`^[a-z][a-zA-Z0-9_:@./-]*$`)

// sanitizeChannels returns a copy of in with malformed entries dropped.
// Empty input → nil (callers treat nil + empty slice the same). The
// `source` argument is included in the log line so operators can tell
// which session dropped the entry without grep-correlating timestamps.
func sanitizeChannels(in []string, source string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, ch := range in {
		if channelSpecRe.MatchString(ch) {
			out = append(out, ch)
			continue
		}
		slog.Warn("dropping malformed channel spec",
			"source", source, "spec", ch)
	}
	return out
}
