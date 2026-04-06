package pty

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansi.Strip(s)
}

var focusFilter = regexp.MustCompile(`\x1b\[\?1004[hl]|\x1b\[[IO]`)

// FilterFocusSequences removes terminal focus escape sequences.
func FilterFocusSequences(s string) string {
	return focusFilter.ReplaceAllString(s, "")
}

var ctrlLReplacer = strings.NewReplacer("\x0c", "")

// FilterCtrlL removes Ctrl+L (form feed) characters.
func FilterCtrlL(s string) string {
	return ctrlLReplacer.Replace(s)
}
