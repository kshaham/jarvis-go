package office

// Q&A primitive used by `cm ask` (see ~/.claude/skills/office-ask-for-help).
//
// When an agent is stuck mid-task it runs:
//
//	cm ask "<question>" --about <taskId> --wait 5m
//
// The CLI POSTs to /api/office/agent/ask, which calls AskQuestion below.
// AskQuestion picks the team lead first, falls back to a non-busy
// teammate by role-affinity if the lead doesn't answer in time, and
// blocks the asker's HTTP request until either an answer arrives or
// the overall wait elapses. The recipient sees a `[QUESTION FROM <name>]:`
// prompt and replies with `cm ask answer <qid> "..."`.
//
// State lives on Manager (pendingQuestions, lastAskAt) and on AgentState
// (QuestionSlot — a waitSlot mirroring StandupSlot/BrainstormSlot).
//
// The whole thing is intentionally in-memory: like standups and
// brainstorms, in-flight questions don't survive a daemon restart.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// QuestionStatus enumerates the terminal states a Question can land in.
// "pending" appears only while the question is in flight.
type QuestionStatus string

const (
	QuestionPending     QuestionStatus = "pending"
	QuestionAnswered    QuestionStatus = "answered"
	QuestionTimeout     QuestionStatus = "timeout"
	QuestionNoRecipient QuestionStatus = "no-recipient"
)

// Question is one help request from an asker to a (rotating) recipient.
// Tried preserves the rotation history so the CLI can render
// "asked: lead, qa-lead" on a timeout exit.
type Question struct {
	ID            string         `json:"id"`
	FromSessionID string         `json:"fromSessionId"`
	FromName      string         `json:"fromName,omitempty"`
	ToSessionID   string         `json:"toSessionId,omitempty"`
	ToName        string         `json:"toName,omitempty"`
	Content       string         `json:"content"`
	AboutTaskID   string         `json:"aboutTaskId,omitempty"`
	AboutTitle    string         `json:"aboutTitle,omitempty"`
	AskedAt       time.Time      `json:"askedAt"`
	AnsweredAt    time.Time      `json:"answeredAt,omitempty"`
	Answer        string         `json:"answer,omitempty"`
	Status        QuestionStatus `json:"status"`
	Tried         []string       `json:"tried,omitempty"`
}

const (
	// Per-asker / per-content cooldown for duplicate questions. Protects
	// recipients from a stuck agent re-asking the same thing on every turn
	// while the asker chews through their nudge budget. Keyed by
	// (fromSession, sha256(sanitized content)).
	askDuplicateCooldown = 10 * time.Minute
	// Floor / ceiling on the per-call --wait. The floor exists to defend
	// against malformed CLI input (e.g. --wait 0); 1s is well below any
	// useful production wait but still leaves room for the rotation
	// scheduler to fire deterministically. The ceiling caps in-memory
	// state growth -- past 30 min the agent is better served by
	// `cm task blocked` than a longer wait.
	askMinWait = 1 * time.Second
	askMaxWait = 30 * time.Minute
	// Hard ceiling on question text. Mirrors the 8 KB cap on `cm message`
	// in OfficeAgentMessage so a runaway prompt can't fill the recipient's
	// context.
	askMaxContentLen = 8000
)

// AskQuestion is the entry point for `cm ask`. It blocks until either a
// recipient answers, the overall wait elapses, or no recipient could be
// found. The returned Question always carries a non-pending Status.
//
// The split is half-and-half: the lead gets the first totalWait/2, the
// fallback teammate gets the rest. With totalWait=5m that's 2m30s each,
// well above the typical "I'm in the middle of a turn, let me finish"
// window.
func (m *Manager) AskQuestion(fromSession, content, aboutTaskID string, totalWait time.Duration) (*Question, error) {
	content = sanitizeForTerminal(strings.TrimSpace(content))
	if content == "" {
		return nil, fmt.Errorf("question content is required")
	}
	if len(content) > askMaxContentLen {
		return nil, fmt.Errorf("question too long (max %d chars)", askMaxContentLen)
	}
	if totalWait < askMinWait {
		totalWait = askMinWait
	}
	if totalWait > askMaxWait {
		totalWait = askMaxWait
	}

	// Asker must be a registered agent. We need their AgentState to
	// touch LastAgentAPIAt so the stuck-detector treats the long
	// HTTP wait as legitimate engagement.
	asker := m.GetAgent(fromSession)
	if asker == nil {
		return nil, fmt.Errorf("asker session %q is not a registered agent", fromSession)
	}
	asker.TouchAgentAPI()

	// Duplicate-question cooldown: same asker re-asking essentially the
	// same question within 10 min is rejected. Hash on the sanitized
	// content so capitalization/whitespace tweaks don't smuggle a
	// duplicate through.
	key := askCooldownKey(fromSession, content)
	m.mu.Lock()
	if last, ok := m.lastAskAt[key]; ok && time.Since(last) < askDuplicateCooldown {
		remaining := askDuplicateCooldown - time.Since(last)
		m.mu.Unlock()
		return nil, fmt.Errorf("duplicate question (cooldown %s remaining); revise the question or run `cm task blocked` instead",
			remaining.Round(time.Second))
	}
	m.lastAskAt[key] = time.Now()
	m.mu.Unlock()

	// Build the question record. ToSessionID gets filled in as we
	// rotate through recipients.
	askerName := fromSession
	if asker.Config != nil {
		askerName = asker.Config.Name
	}
	q := &Question{
		ID:            "q-" + uuid.New().String()[:8],
		FromSessionID: fromSession,
		FromName:      askerName,
		Content:       content,
		AboutTaskID:   aboutTaskID,
		AboutTitle:    m.lookupTaskTitle(fromSession, aboutTaskID),
		AskedAt:       time.Now(),
		Status:        QuestionPending,
	}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()
	defer m.cleanupQuestion(q.ID)

	// Pick the lead first. findStrategicAgent already excludes the asker.
	// findAvailableAgent does role-affinity + busy-skip, so we use it
	// for the fallback.
	deadline := time.Now().Add(totalWait)
	perRecipient := totalWait / 2
	if perRecipient < askMinWait {
		perRecipient = askMinWait
	}

	if lead := m.findStrategicAgent(fromSession); lead != nil {
		if answer, ok := m.askOnce(q, lead, perRecipient, deadline); ok {
			m.markAnswered(q, answer)
			return q, nil
		}
	}

	// Fallback: any non-busy, role-affinity-preferred teammate that
	// we haven't already tried.
	for {
		fallback := m.findAvailableAgentExcluding(fromSession, q.Tried)
		if fallback == nil {
			break
		}
		// Whatever's left of the deadline is the budget for this
		// teammate. If we're already past the deadline, give up.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if answer, ok := m.askOnce(q, fallback, remaining, deadline); ok {
			m.markAnswered(q, answer)
			return q, nil
		}
	}

	if len(q.Tried) == 0 {
		// q is still in m.pendingQuestions, so this terminal-status write must
		// hold m.mu (AnswerQuestion/ListPendingQuestionsFor read q.Status under
		// m.mu; markAnswered writes it under m.mu). Release before the notify,
		// which itself locks.
		m.mu.Lock()
		q.Status = QuestionNoRecipient
		m.mu.Unlock()
		m.NotifyOfficeEvent("Agent stuck-asking",
			fmt.Sprintf("%s asked the team a question and nobody was available (about: %q)", askerName, q.AboutTitle))
		slog.Warn("cm ask: no recipient available", "asker", askerName, "qid", q.ID)
		return q, nil
	}
	m.mu.Lock()
	q.Status = QuestionTimeout
	m.mu.Unlock()
	m.NotifyOfficeEvent("Agent stuck-asking",
		fmt.Sprintf("%s asked %d teammates (%s) and got no answer in %s (about: %q)",
			askerName, len(q.Tried), strings.Join(q.triedNames(m), ", "),
			totalWait.Round(time.Second), q.AboutTitle))
	slog.Warn("cm ask: timeout", "asker", askerName, "qid", q.ID, "tried", q.Tried)
	return q, nil
}

// askOnce delivers q to one recipient, installs that recipient's
// QuestionSlot, and blocks for up to maxWait OR until deadline. Returns
// (answer, true) on success, ("", false) on timeout. On timeout it
// finishes the slot so a late answer is dropped instead of polluting
// the next round, and appends recipient to q.Tried.
func (m *Manager) askOnce(q *Question, recipient *AgentState, maxWait time.Duration, deadline time.Time) (string, bool) {
	// Bound maxWait by the overall deadline so we don't sit past it.
	if remaining := time.Until(deadline); remaining < maxWait {
		maxWait = remaining
	}
	if maxWait <= 0 {
		return "", false
	}

	// Install the slot under the recipient's lock and stash the routing
	// info on the question. Doing both under one critical section per
	// side keeps AnswerQuestion's "is fromSession the current recipient?"
	// check consistent with what we just wrote.
	slot := newWaitSlot()
	recipient.mu.Lock()
	// Drop a stranded slot from a previous rotation so its eventual
	// answer doesn't land here. waitSlot.finish() is idempotent.
	if recipient.QuestionSlot != nil {
		recipient.QuestionSlot.finish()
	}
	recipient.QuestionSlot = slot
	recipient.QuestionSlotQID = q.ID
	recipient.mu.Unlock()

	recipientName := recipient.SessionID()
	if recipient.Config != nil {
		recipientName = recipient.Config.Name
	}

	m.mu.Lock()
	q.ToSessionID = recipient.SessionID()
	q.ToName = recipientName
	q.Tried = append(q.Tried, recipient.SessionID())
	sessions := m.sessions
	m.mu.Unlock()

	if sessions != nil {
		sessions.WriteInkPrompt(recipient.SessionID(), formatQuestionPrompt(q))
		recipient.mu.Lock()
		recipient.LastPromptSentAt = time.Now()
		recipient.mu.Unlock()
	}

	m.broadcast("office:questionAsked", map[string]interface{}{
		"questionId": q.ID,
		"from":       q.FromSessionID,
		"fromName":   q.FromName,
		"to":         recipient.SessionID(),
		"toName":     recipientName,
		"about":      q.AboutTaskID,
	})

	// Block on the slot. The deliver path (AnswerQuestion) is the only
	// thing that can satisfy this; if the recipient stays silent we
	// time out and the caller rotates.
	defer func() {
		slot.finish()
		recipient.mu.Lock()
		// Only clear if it's still our slot — a late AskQuestion call
		// may have already replaced it.
		if recipient.QuestionSlot == slot {
			recipient.QuestionSlot = nil
			recipient.QuestionSlotQID = ""
		}
		recipient.mu.Unlock()
	}()

	select {
	case answer := <-slot.ch():
		return answer, true
	case <-time.After(maxWait):
		return "", false
	}
}

// AnswerQuestion is invoked by the recipient via `cm ask answer`.
// Validates that fromSession is the question's current recipient
// (rotation-safe) and delivers the answer to the asker's wait slot.
func (m *Manager) AnswerQuestion(qID, fromSession, answer string) error {
	answer = sanitizeForTerminal(strings.TrimSpace(answer))
	if answer == "" {
		return fmt.Errorf("answer is required")
	}
	if len(answer) > askMaxContentLen {
		return fmt.Errorf("answer too long (max %d chars)", askMaxContentLen)
	}

	m.mu.Lock()
	q, ok := m.pendingQuestions[qID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown question %q (timed out or already answered)", qID)
	}
	if q.Status != QuestionPending {
		m.mu.Unlock()
		return fmt.Errorf("question %q is %s", qID, q.Status)
	}
	if q.ToSessionID != fromSession {
		m.mu.Unlock()
		return fmt.Errorf("you are not the current recipient of %q", qID)
	}
	m.mu.Unlock()

	responder := m.GetAgent(fromSession)
	if responder == nil {
		return fmt.Errorf("responder session %q is not a registered agent", fromSession)
	}
	responder.TouchAgentAPI()

	// Hand to the slot. If the asker has already given up (slot finished
	// during a rotation or after the overall timeout) deliver returns
	// false and we count it as a late response — same accounting as
	// standup/brainstorm late replies.
	responder.mu.Lock()
	slot := responder.QuestionSlot
	slotQID := responder.QuestionSlotQID
	responder.mu.Unlock()
	// Only deliver if the recipient's CURRENT slot serves THIS question. If a
	// newer question re-targeted the same recipient, its slot replaced ours, so
	// answering the older question must NOT deliver to the newer one's asker.
	if slot == nil || slotQID != qID || !slot.deliver(answer) {
		recordLateResponse("question", fromSession)
		return fmt.Errorf("asker stopped waiting; answer dropped")
	}

	// Mirror the answer to the asker's terminal so a slow HTTP race
	// doesn't lose the content. The asker's HTTP handler will also
	// receive it via the slot and print it; the prompt is a belt-and-
	// suspenders so the agent sees `[ANSWER FROM <name>]:` on its
	// next turn no matter what.
	if asker := m.GetAgent(q.FromSessionID); asker != nil {
		m.mu.RLock()
		sessions := m.sessions
		m.mu.RUnlock()
		responderName := fromSession
		if responder.Config != nil {
			responderName = responder.Config.Name
		}
		if sessions != nil {
			sessions.WriteInkPrompt(q.FromSessionID,
				fmt.Sprintf("[ANSWER FROM %s] (qid: %s): %s", responderName, q.ID, answer))
		}
	}

	return nil
}

// ListPendingQuestionsFor returns the questions currently addressed to
// the given session. Used by `cm ask list`.
func (m *Manager) ListPendingQuestionsFor(sessionID string) []*Question {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Question, 0)
	for _, q := range m.pendingQuestions {
		if q.Status == QuestionPending && q.ToSessionID == sessionID {
			out = append(out, q)
		}
	}
	return out
}

// markAnswered fills in the terminal fields on a successful Q&A round.
// Called from AskQuestion after askOnce returns ok.
func (m *Manager) markAnswered(q *Question, answer string) {
	now := time.Now()
	m.mu.Lock()
	q.Status = QuestionAnswered
	q.Answer = answer
	q.AnsweredAt = now
	m.mu.Unlock()

	m.broadcast("office:questionAnswered", map[string]interface{}{
		"questionId": q.ID,
		"from":       q.FromSessionID,
		"to":         q.ToSessionID,
		"answer":     answer,
	})
	slog.Info("cm ask: answered", "qid", q.ID, "asker", q.FromName, "responder", q.ToName)
}

// cleanupQuestion is the deferred cleanup from AskQuestion. It removes
// the question from the in-memory registry once the asker's HTTP call
// has returned. Late answers from a recipient who finally got around
// to typing land on a finished slot in askOnce's defer and are
// dropped + counted by recordLateResponse — they don't need the
// registry entry.
func (m *Manager) cleanupQuestion(qID string) {
	m.mu.Lock()
	delete(m.pendingQuestions, qID)
	m.mu.Unlock()
}

// formatQuestionPrompt builds the `[QUESTION FROM ...]:` prompt
// delivered to the recipient's session. Mirrors the
// `[Team Message from <name>]:` format SendMessage uses (manager.go:665)
// so agents can pattern-match on the same family of bracketed prefixes.
func formatQuestionPrompt(q *Question) string {
	about := q.AboutTitle
	if about == "" && q.AboutTaskID != "" {
		about = q.AboutTaskID
	}
	if about == "" {
		about = "general"
	}
	return fmt.Sprintf("[QUESTION FROM %s (qid: %s, about: %q)]: %s\n\nReply: cm ask answer %s \"<your answer>\"",
		q.FromName, q.ID, about, q.Content, q.ID)
}

// askCooldownKey returns a stable map key for the duplicate-question
// cooldown. Hashes the content so the map doesn't store full question
// text and so casing/whitespace differences don't sneak past.
func askCooldownKey(fromSession, content string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.Join(strings.Fields(content), " "))))
	return fromSession + "|" + hex.EncodeToString(h[:8])
}

// findAvailableAgentExcluding is the "fallback" recipient picker. Same
// rules as findAvailableAgent (role-affinity preferred, busy/in-meeting
// skipped) but also filters out anyone we already asked in this
// rotation.
func (m *Manager) findAvailableAgentExcluding(askerSessionID string, alreadyTried []string) *AgentState {
	tried := make(map[string]struct{}, len(alreadyTried))
	for _, sid := range alreadyTried {
		tried[sid] = struct{}{}
	}
	tried[askerSessionID] = struct{}{}

	m.mu.RLock()
	defer m.mu.RUnlock()
	sessions := m.sessions

	// Look up the requesting agent's role for affinity matching. Mirrors
	// findAvailableAgent (recovery.go:1156-1162) so we keep partner-
	// preference parity with the brainstorm path.
	var requestingRole string
	if requester, ok := m.agents[askerSessionID]; ok && requester.Config != nil {
		requestingRole = requester.Config.Role
	}
	preferredRoles := RoleAffinityPartners(requestingRole)

	isEligible := func(a *AgentState) bool {
		if a == nil || a.Config == nil {
			return false
		}
		if _, skip := tried[a.SessionID()]; skip {
			return false
		}
		if a.ActiveTask() != nil || a.PendingTaskCount() > 1 {
			return false
		}
		if sessions != nil && sessions.IsSessionWorking(a.SessionID()) {
			return false
		}
		// Don't pull someone out of an active meeting just to answer a
		// question — they'll respond after the meeting ends, by which
		// time the asker's wait is likely up.
		if m.isInActiveMeetingLocked(a.SessionID()) {
			return false
		}
		return true
	}

	// First pass: role-affinity match.
	for _, preferredRole := range preferredRoles {
		for _, agent := range m.agents {
			if agent.Config != nil && agent.Config.Role == preferredRole && isEligible(agent) {
				return agent
			}
		}
	}
	// Fallback: any eligible teammate.
	for _, agent := range m.agents {
		if isEligible(agent) {
			return agent
		}
	}
	return nil
}

// isInActiveMeetingLocked is the read-locked twin of IsInActiveMeeting.
// Callers that already hold m.mu (RLock or Lock) must use this variant
// to avoid the double-RLock that would deadlock under writer contention.
func (m *Manager) isInActiveMeetingLocked(sessionID string) bool {
	for _, mtg := range m.meetings {
		if !mtg.Active {
			continue
		}
		for _, p := range mtg.Participants {
			if p == sessionID {
				return true
			}
		}
	}
	return false
}

// lookupTaskTitle returns the title of taskID belonging to sessionID,
// or "" if no such task. Used to prefix the question prompt with
// human-readable context so the recipient doesn't need to grep for it.
func (m *Manager) lookupTaskTitle(sessionID, taskID string) string {
	if taskID == "" {
		return ""
	}
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return ""
	}
	for _, t := range agent.GetTasks() {
		if t.ID == taskID {
			return t.Title
		}
	}
	return ""
}

// triedNames maps q.Tried (session IDs) to display names for the
// timeout notification. Best-effort: if an agent unregistered between
// the ask and the notify, the session ID is used as a fallback.
func (q *Question) triedNames(m *Manager) []string {
	out := make([]string, 0, len(q.Tried))
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sid := range q.Tried {
		if a, ok := m.agents[sid]; ok && a.Config != nil {
			out = append(out, a.Config.Name)
			continue
		}
		out = append(out, sid)
	}
	return out
}
