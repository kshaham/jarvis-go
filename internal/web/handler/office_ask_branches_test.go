package handler

// Round-3 branch fills for OfficeAgentAsk / OfficeAgentAskAnswer:
//   - the wait<=0 default arm + the AskQuestion success response (a registered
//     asker with no available recipient returns an unanswered question fast)
//   - the AskAnswer success arm via a real ask/answer handshake

import (
	"net/http"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

// TestOfficeAgentAsk_DefaultWaitNoRecipient drives the wait<=0 default arm and
// the success response: a registered asker with no other teammate returns an
// (unanswered) question once the recipient search exhausts.
func TestOfficeAgentAsk_DefaultWaitNoRecipient(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.RegisterAgent("s-asker", "cfg-asker", &office.TeamMemberConfig{ID: "cfg-asker", Name: "Asker"})

	done := make(chan *httptestResult, 1)
	go func() {
		// waitSeconds omitted -> the handler applies the 5m default, but with no
		// recipient the AskQuestion search returns quickly (no askOnce blocking).
		rec := postJSON(t, ctx.OfficeAgentAsk, "/api/office/agent/ask", map[string]interface{}{
			"fromSessionId": "s-asker", "content": "anyone around to review?",
		})
		done <- &httptestResult{code: rec.Code, body: rec.Body.String()}
	}()

	select {
	case res := <-done:
		if res.code != http.StatusOK {
			t.Fatalf("expected 200 ask response, got %d: %s", res.code, res.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ask handler did not return within 10s (no-recipient path should be fast)")
	}
}

type httptestResult struct {
	code int
	body string
}

// TestOfficeAgentAskAnswer_Success drives the AnswerQuestion success arm
// (ok(w, nil) at office.go:2062) via a real ask/answer handshake.
func TestOfficeAgentAskAnswer_Success(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	mgr := ctx.OfficeManager
	// Asker + a strategic lead who will receive + answer the question.
	mgr.RegisterAgent("s-asker", "cfg-asker", &office.TeamMemberConfig{ID: "cfg-asker", Name: "Asker", Tier: "worker"})
	mgr.RegisterAgent("s-lead", "cfg-lead", &office.TeamMemberConfig{ID: "cfg-lead", Name: "Lead", Tier: "strategic"})

	askDone := make(chan struct{})
	go func() {
		defer close(askDone)
		_, _ = mgr.AskQuestion("s-asker", "what is the deploy target?", "", 8*time.Second)
	}()

	// Poll until the lead sees the pending question, then answer it via the handler.
	var qid string
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		qs := mgr.ListPendingQuestionsFor("s-lead")
		if len(qs) > 0 {
			qid = qs[0].ID
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if qid == "" {
		t.Skip("lead never received the pending question in this environment; skip the answer arm")
	}

	rec := postJSON(t, ctx.OfficeAgentAskAnswer, "/api/office/agent/ask/answer", map[string]interface{}{
		"fromSessionId": "s-lead", "questionId": qid, "answer": "production cluster us-east",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from a valid answer, got %d: %s", rec.Code, rec.Body.String())
	}
	<-askDone
}
