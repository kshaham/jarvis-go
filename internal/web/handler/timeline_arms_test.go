package handler

// Round-4 fills for office_timeline.go:
//   - collectTimelineEntries: a message from an UNregistered sender (empty
//     FromName -> nameByID fallback, also empty) and a >280-char body that gets
//     truncated (195-197, 207-209).
//   - listPersistedStandups: the os.UserHomeDir error arm (252-254) via an empty
//     HOME.

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeTimeline_UnregisteredSenderAndLongMessageTruncated(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager

	// Message from an UNregistered session id -> FromName stays empty and the
	// nameByID lookup also misses, exercising the senderName fallback arm. The
	// body is > 280 chars so the summary-truncation arm fires too.
	longBody := strings.Repeat("z", 400)
	mgr.SendMessage("ghost-sender", "", longBody)

	req := httptest.NewRequest("GET", "/api/office/timeline?type=message&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)

	resp := decodeTimeline(t, rec)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 message entry, got %d", len(entries))
	}
	m := entries[0].(map[string]interface{})
	summary, _ := m["summary"].(string)
	// 280 chars + the "..." ellipsis.
	if !strings.HasSuffix(summary, "...") {
		t.Errorf("expected a truncated summary ending in '...', got len=%d", len(summary))
	}
	if len(summary) != 283 {
		t.Errorf("expected truncated summary length 283 (280 + '...'), got %d", len(summary))
	}
	// Title routes to "<sender> → team" for an empty recipient.
	if title, _ := m["title"].(string); !strings.HasSuffix(title, "→ team") {
		t.Errorf("expected title ending in '→ team', got %q", title)
	}
}

func TestListPersistedStandups_NoHomeReturnsNil(t *testing.T) {
	t.Setenv("HOME", "") // os.UserHomeDir errors -> nil
	if got := listPersistedStandups(); got != nil {
		t.Errorf("expected nil when HOME is unset, got %v", got)
	}
}
