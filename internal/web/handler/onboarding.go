package handler

// R31 · Agent-auth onboarding endpoint. Backs `cm onboard <topic>`.
// The kickstart shrank ~80% by replacing inline boilerplate with
// pointers to this endpoint; the tradeoff is agents need to know to
// fetch on first use. The endpoint is deliberately minimal -- plain
// text, no JSON wrapper -- so the agent's terminal shows the answer
// directly.

import (
	"net/http"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

// OfficeAgentOnboarding returns the body of an onboarding topic.
// GET /api/office/agent/onboarding?sessionId=<id>&topic=<name>
//
// Topics: protocols, browser, dev-instance, dev-lead, context-budget,
// skills, memory. `dev-lead` is strategic-tier only; workers get 403.
func (ctx *Context) OfficeAgentOnboarding(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		request.WriteError(w, http.StatusBadRequest, "topic is required")
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(sessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	body, ok := office.GetOnboardingTopic(office.OnboardingTopic(topic), agent.Config, agent.WorktreePath)
	if !ok {
		// Ambiguity between "unknown topic" and "access denied" is
		// resolved here: if it's a known topic and the tier is wrong,
		// return 403 with a hint; else 404.
		known := map[office.OnboardingTopic]bool{}
		for _, t := range office.OnboardingTopics("strategic") {
			known[t] = true
		}
		if known[office.OnboardingTopic(topic)] {
			request.WriteError(w, http.StatusForbidden, "topic requires strategic tier")
			return
		}
		request.WriteError(w, http.StatusNotFound, "unknown topic")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
