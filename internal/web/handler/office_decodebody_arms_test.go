package handler

// Round-4 fills for the remaining office.go decodeBody bad-JSON 400 arms that
// the earlier batches didn't cover:
//   OfficePostBugTask, OfficeAgentTaskAck, OfficePutAutoStandup,
//   OfficePutTelegramConfig, OfficePostMessage, OfficeCreateTeamTemplate,
//   OfficeAgentPostTelegramConfig, OfficePostWrapUp.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfficeDecodeBody_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	handlers := map[string]func(http.ResponseWriter, *http.Request){
		"OfficePostBugTask":             ctx.OfficePostBugTask,
		"OfficeAgentTaskAck":            ctx.OfficeAgentTaskAck,
		"OfficePutAutoStandup":          ctx.OfficePutAutoStandup,
		"OfficePutTelegramConfig":       ctx.OfficePutTelegramConfig,
		"OfficePostMessage":             ctx.OfficePostMessage,
		"OfficeCreateTeamTemplate":      ctx.OfficeCreateTeamTemplate,
		"OfficeAgentPostTelegramConfig": ctx.OfficeAgentPostTelegramConfig,
		"OfficePostWrapUp":              ctx.OfficePostWrapUp,
	}
	for name, h := range handlers {
		rec := httptest.NewRecorder()
		h(rec, badJSONReq("/api/office/x"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 on malformed body, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
}
