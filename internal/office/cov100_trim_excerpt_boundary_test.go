package office

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTrimExcerpt_TruncationLandsMidRune drives the rune-boundary backup
// loop body (knowledge.go:414-416, the `limit--`). The prior
// TestTrimExcerpt_RuneBoundaryBackup happens to put a rune START at the
// naive cut index (maxExcerptChars-3 == 217), so its loop never runs.
//
// maxExcerptChars is 220, so the naive cut index is 217. We place
// exactly 215 ASCII bytes followed by 3-byte runes (世 = E4 B8 96):
//
//	bytes 0..214  : 'a'
//	byte  215     : 世 byte0 (rune start)
//	byte  216     : 世 byte1 (continuation)
//	byte  217     : 世 byte2 (continuation)  <-- the cut index
//
// So line[217] is a continuation byte, RuneStart is false, and the loop
// must back `limit` up from 217 to 215 (the rune start) before slicing.
func TestTrimExcerpt_TruncationLandsMidRune(t *testing.T) {
	const cut = maxExcerptChars - 3 // 217
	in := strings.Repeat("a", cut-2) + strings.Repeat("世", 20)

	// Sanity: the byte at the naive cut index is a continuation byte, so
	// the backup loop is guaranteed to execute at least once.
	if utf8.RuneStart(in[cut]) {
		t.Fatalf("test fixture invalid: byte at cut index %d is a rune start", cut)
	}

	got := trimExcerpt(in)

	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("excerpt is not valid UTF-8 (rune was split): %q", got)
	}
	// The body (sans "...") must end exactly at the rune boundary 215,
	// proving the loop backed up from 217 -> 215.
	body := strings.TrimSuffix(got, "...")
	if len(body) != cut-2 {
		t.Errorf("expected body length %d (backed up to rune boundary), got %d", cut-2, len(body))
	}
	if strings.ContainsRune(got, '�') {
		t.Errorf("excerpt contains replacement char: %q", got)
	}
}
