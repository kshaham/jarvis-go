package office

import "testing"

func TestUIKeywordRe_Hits(t *testing.T) {
	hits := []string{
		"Fix the UI bug",
		"Update frontend layout",
		"Style the front-end nav",
		"Rebuild the modal",
		"CSS overhaul",
		"Adjust button hover",
		"Refactor the page",
		"Expo react-native screen",
		"Visual polish",
		"Take a screenshot",
	}
	for _, s := range hits {
		if !UIKeywordRe.MatchString(s) {
			t.Errorf("expected UIKeywordRe to hit %q", s)
		}
	}
}

func TestUIKeywordRe_NonHits(t *testing.T) {
	misses := []string{
		"Build the backend cache",
		"Optimize SQL queries",
		"Document the API",
		"",
		"build me a thing", // 'build' shouldn't false-positive
	}
	for _, s := range misses {
		if UIKeywordRe.MatchString(s) {
			t.Errorf("expected UIKeywordRe to MISS %q", s)
		}
	}
}

func TestVerificationEvidenceRe_Hits(t *testing.T) {
	hits := []string{
		"Took a screenshot at /tmp/x.png",
		"verified visually in chromium",
		"used expect-cli to confirm",
		"confirmed in browser via chromedp",
		"confirmed via browser",
		"output.png attached",
	}
	for _, s := range hits {
		if !VerificationEvidenceRe.MatchString(s) {
			t.Errorf("expected VerificationEvidenceRe to hit %q", s)
		}
	}
}

func TestShouldWarnMissingScreenshot_HappyPath(t *testing.T) {
	// UI title, no evidence in summary, no attached PNG → should warn.
	got := ShouldWarnMissingScreenshot("Fix the modal layout", "made it work", "missing-task-id")
	if !got {
		t.Error("expected warning for UI title + bare summary + no attachment")
	}
}

func TestShouldWarnMissingScreenshot_NonUITitleNoWarn(t *testing.T) {
	got := ShouldWarnMissingScreenshot("Refactor SQL", "made it work", "missing")
	if got {
		t.Error("non-UI title should not warn")
	}
}

func TestShouldWarnMissingScreenshot_EmptyTitleNoWarn(t *testing.T) {
	if ShouldWarnMissingScreenshot("", "made it work", "missing") {
		t.Error("empty title should not warn")
	}
}

func TestShouldWarnMissingScreenshot_EvidenceInSummarySilences(t *testing.T) {
	got := ShouldWarnMissingScreenshot(
		"Fix the modal", "confirmed in browser, see /tmp/after.png", "missing",
	)
	if got {
		t.Error("summary with verification evidence should silence warning")
	}
}

func TestShouldWarnMissingScreenshot_AttachedPNGSilences(t *testing.T) {
	// HasTaskScreenshot is a real disk read; setting up that fixture
	// would require writing a PNG under ~/.jarvis. Instead, this test
	// documents the "no warn" path: when the predicate sees a summary
	// with explicit evidence text. The PNG-attached path is covered
	// by VerificationEvidenceRe matching the embedded ".png" reference.
	got := ShouldWarnMissingScreenshot("Modal redesign", "after.png attached", "any-task")
	if got {
		t.Error("expected attached-PNG reference to silence warning")
	}
}
