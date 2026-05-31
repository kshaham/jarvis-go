package handler

import "testing"

// TestUIKeywordRe pins the behavior of the task-title classifier used by
// the soft screenshot gate in OfficeAgentTaskComplete. Matches fire the
// gate; misses pass through silently. "UI token rotation" is an accepted
// false-positive per the plan (we chose precision over recall).
func TestUIKeywordRe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		title string
		match bool
	}{
		// Positive: clear UI keywords.
		{"Sign-up page polish", true},
		{"Fix the modal close button", true},
		{"Update the layout for mobile", true},
		{"Rebrand the login styling", true},
		{"Tweak CSS for the hero", true},
		{"Expo screen tweak", true},
		{"React Native list perf", true},
		{"React-Native list perf", true},
		{"Add design-token sweep", true},
		{"Capture screenshot of dashboard", true},
		{"Visual regression in the sidebar", true},
		{"Frontend refactor", true},
		{"Front-end refactor", true},
		// Accepted false-positive: any word containing "UI" as a bare token.
		{"Update UI token rotation", true},
		// Negative: backend / infra / docs.
		{"Refactor backend error handling", false},
		{"Bump dependency versions", false},
		{"Add telemetry exporter", false},
		{"Fix rate limiter", false},
		{"Document the deploy pipeline", false},
		{"Optimize SQL query planner", false},
	}
	for _, tc := range cases {
		got := uiKeywordRe.MatchString(tc.title)
		if got != tc.match {
			t.Errorf("uiKeywordRe.MatchString(%q) = %v, want %v", tc.title, got, tc.match)
		}
	}
}

// TestVerificationEvidenceRe pins what summary text the gate accepts as
// "I ran the browser-check." Matching here suppresses the warning.
func TestVerificationEvidenceRe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		summary string
		match   bool
	}{
		// Positive: summaries that cite browser verification clearly.
		{"Screenshotted at /tmp/signup.png", true},
		{"Ran browser-screenshot /tmp/x.png", true},
		{"Captured screenshot after the fix", true},
		{"Ran expect-cli against the sign-up flow", true},
		{"Verified visually in Chrome", true},
		{"Confirmed via browser at /admin", true},
		{"Confirmed in browser, matches spec", true},
		{"See /tmp/after.png for proof", true},
		{"Wrote a puppeteer script to drive the flow", true},
		// Negative: summaries that don't cite browser verification.
		{"implemented X", false},
		{"Refactored the reducer", false},
		{"Updated docs", false},
		{"Passed unit tests", false},
		{"", false},
	}
	for _, tc := range cases {
		got := verificationEvidenceRe.MatchString(tc.summary)
		if got != tc.match {
			t.Errorf("verificationEvidenceRe.MatchString(%q) = %v, want %v", tc.summary, got, tc.match)
		}
	}
}
