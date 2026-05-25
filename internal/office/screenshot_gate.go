package office

import "regexp"

// Screenshot-gate logic.
//
// When an agent marks a UI-flavoured task complete, jarvis soft-checks
// that the summary cites browser-verification evidence (screenshot
// path, `expect-cli`, "verified visually", etc.). When the title
// matches a UI keyword but the summary doesn't, the operator sees a
// warning in the SSE feed + dashboard. Completion itself always
// lands — this is a coaching signal, not a hard gate.
//
// Extracted from handler/office.go so the regexes + predicate can be
// tested + reused without standing up an HTTP context. The
// per-task screenshot-attachment check (HasTaskScreenshot) already
// lives in the office package.

// UIKeywordRe matches task titles that strongly suggest user-visible
// work. Matching is what enables the soft screenshot gate.
//
// Word-boundary anchored so "ui" matches as a standalone token but
// not inside "build" or "guidance". The full keyword set covers the
// vocabulary an operator types into task titles in practice.
var UIKeywordRe = regexp.MustCompile(`(?i)\b(ui|frontend|front[- ]end|page|button|modal|dialog|layout|styling|css|design|screen(?:shot)?|visual|expo|react[- ]native)\b`)

// VerificationEvidenceRe matches phrases that would show up in a
// task-completion summary that ran a browser check — screenshot file
// paths, tool names, "verified visually", etc. Any match suppresses
// the gate warning because the agent demonstrated they exercised the
// UI in a browser, not just in their head.
var VerificationEvidenceRe = regexp.MustCompile(`(?i)(screenshot|browser[- ]screenshot|verified visually|expect-cli|puppeteer|\.png\b|confirmed\s+(?:in|via)\s+browser)`)

// ShouldWarnMissingScreenshot returns true when the operator should
// be warned that this task completion lacks visual-verification
// evidence. The combined check is:
//
//  1. The task title hits UIKeywordRe (user-visible work signal).
//  2. The summary does NOT hit VerificationEvidenceRe (no browser
//     citation in the prose).
//  3. The task has no attached screenshot on disk (an uploaded PNG
//     is stronger evidence than a citation).
//
// All three must be true for the warning to fire — operators who
// either typed a screenshot path into their summary OR uploaded a
// real PNG get a silent pass.
//
// Pure given HasTaskScreenshot's disk-read side effect; the rest is
// a couple of regex matches.
func ShouldWarnMissingScreenshot(taskTitle, summary, taskID string) bool {
	if taskTitle == "" {
		return false
	}
	if !UIKeywordRe.MatchString(taskTitle) {
		return false
	}
	if VerificationEvidenceRe.MatchString(summary) {
		return false
	}
	if HasTaskScreenshot(taskID) {
		return false
	}
	return true
}
