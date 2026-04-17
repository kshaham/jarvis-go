package office

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// roleAffinity maps roles to preferred brainstorm partner roles.
var roleAffinity = map[string][]string{
	"Frontend Engineer":    {"UI/UX Designer", "Design Lead", "Frontend Architect"},
	"UI/UX Designer":       {"Frontend Engineer", "Frontend Architect", "Design Lead"},
	"Backend Engineer":     {"DevOps Engineer", "Senior Architect", "Backend Architect"},
	"DevOps Engineer":      {"Backend Engineer", "QA Engineer", "Site Reliability Engineer"},
	"QA Engineer":          {"DevOps Engineer", "Frontend Engineer", "Backend Engineer"},
	"Senior Architect":     {"Backend Engineer", "Frontend Engineer", "Engineering Manager"},
	"Engineering Manager":  {"Senior Architect", "Tech Lead", "Product Manager"},
	"Tech Lead":            {"Senior Architect", "Engineering Manager", "Backend Engineer"},
	"Frontend Architect":   {"Frontend Engineer", "UI/UX Designer", "Senior Architect"},
	"Backend Architect":    {"Backend Engineer", "Senior Architect", "DevOps Engineer"},
	"Design Lead":          {"UI/UX Designer", "Frontend Engineer", "Product Manager"},
}

// RoleAffinityPartners returns the preferred partner roles for a given role.
func RoleAffinityPartners(role string) []string {
	return roleAffinity[role]
}

// BrainstormResponseEvent is emitted when a brainstorm response is received.
type BrainstormResponseEvent struct {
	SessionID string
	MeetingID string
	Proposals string
}

func (BrainstormResponseEvent) officeEventType() string { return "brainstormResponse" }

// MeetingStartedEvent is emitted when a meeting begins.
type MeetingStartedEvent struct {
	MeetingID    string
	Type         string
	Participants []string
}

func (MeetingStartedEvent) officeEventType() string { return "meetingStarted" }

// MeetingEndedEvent is emitted when a meeting ends.
type MeetingEndedEvent struct {
	MeetingID string
}

func (MeetingEndedEvent) officeEventType() string { return "meetingEnded" }

// StartBrainstorm begins a brainstorm meeting. Returns the meeting or nil
// if the brainstorm was blocked (cooldown, wrap-up, insufficient participants).
func (m *Manager) StartBrainstorm(participantIDs []string) *Meeting {
	m.mu.Lock()
	wrappingUp := m.wrappingUp
	lastBrainstorm := m.lastBrainstormAt
	cooldown := m.config.BrainstormCooldown

	// Block during wrap-up.
	if wrappingUp {
		m.mu.Unlock()
		slog.Info("Brainstorm blocked: team is wrapping up")
		return nil
	}

	// Respect cooldown (atomic check+update to prevent double brainstorms).
	if !lastBrainstorm.IsZero() && time.Since(lastBrainstorm) < cooldown {
		m.mu.Unlock()
		slog.Info("Brainstorm cooldown active, skipping",
			"remaining", cooldown-time.Since(lastBrainstorm))
		return nil
	}

	// Record brainstorm time atomically with cooldown check.
	m.lastBrainstormAt = time.Now()

	// Resolve participants (still under lock -- agents map accessed).
	participants := participantIDs
	if len(participants) == 0 {
		for sessionID := range m.agents {
			participants = append(participants, sessionID)
		}
	}

	// Filter to registered agents.
	active := make([]string, 0, len(participants))
	for _, id := range participants {
		if _, ok := m.agents[id]; ok {
			active = append(active, id)
		}
	}
	m.mu.Unlock()

	if len(active) < 2 {
		slog.Info("Not enough participants for brainstorm", "count", len(active))
		return nil
	}

	// Create the meeting.
	meeting := m.StartMeeting("brainstorm", active)

	m.broadcast("office:meetingStarted", map[string]interface{}{
		"id":           meeting.ID,
		"type":         "brainstorm",
		"participants": active,
		"startedAt":    meeting.StartedAt.UnixMilli(),
	})
	m.emitEvent(MeetingStartedEvent{
		MeetingID:    meeting.ID,
		Type:         "brainstorm",
		Participants: active,
	})

	slog.Info("Brainstorm meeting started", "id", meeting.ID, "participants", len(active))

	// Run brainstorm flow async.
	go m.runBrainstormFlow(meeting, active)

	return meeting
}

// runBrainstormFlow picks a facilitator, sends the brainstorm prompt,
// waits for proposals, then distributes tasks to other participants.
func (m *Manager) runBrainstormFlow(meeting *Meeting, participants []string) {
	defer m.endMeeting(meeting.ID)

	// Pick facilitator: first strategic-tier agent, or first participant.
	facilitatorID := participants[0]
	for _, id := range participants {
		agent := m.GetAgent(id)
		if agent != nil && agent.Config != nil && agent.Config.Tier == "strategic" {
			facilitatorID = id
			break
		}
	}

	facilitatorAgent := m.GetAgent(facilitatorID)
	facilitatorName := facilitatorID
	if facilitatorAgent != nil && facilitatorAgent.Config != nil {
		facilitatorName = facilitatorAgent.Config.Name
	}

	// Build participant names.
	names := make([]string, 0, len(participants))
	for _, id := range participants {
		agent := m.GetAgent(id)
		if agent != nil && agent.Config != nil {
			names = append(names, agent.Config.Name)
		} else {
			names = append(names, id)
		}
	}
	participantNames := strings.Join(names, ", ")

	// Build brainstorm prompt.
	brainstormPrompt := strings.Join([]string{
		fmt.Sprintf("[BRAINSTORM] You and %s are idle and need new tasks.", participantNames),
		"",
		"Look at the project, identify improvements matching your roles, and propose 3-5 tasks.",
		"After brainstorming, assign yourself a task and get started.",
		"",
		"Reply with your proposals by running:",
		`cm brainstorm respond "<your task proposals>"`,
	}, "\n")

	// Send to facilitator.
	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions == nil {
		slog.Warn("Brainstorm: sessions unavailable", "agent", facilitatorName)
		return
	}
	sent := sessions.WriteInkPrompt(facilitatorID, brainstormPrompt)
	if !sent {
		slog.Warn("Brainstorm prompt delivery failed", "agent", facilitatorName)
		return
	}

	if facilitatorAgent != nil {
		facilitatorAgent.mu.Lock()
		facilitatorAgent.LastPromptSentAt = time.Now()
		facilitatorAgent.mu.Unlock()
	}

	// Wait for facilitator response.
	proposals := m.waitForBrainstormResponse(facilitatorID, facilitatorName)

	// Check meeting still exists.
	m.mu.RLock()
	_, meetingExists := m.meetings[meeting.ID]
	m.mu.RUnlock()
	if !meetingExists {
		return
	}

	// Distribute proposals to other participants.
	safeProposals := sanitizeForTerminal(proposals)
	for _, sid := range participants {
		if sid == facilitatorID {
			continue
		}

		agent := m.GetAgent(sid)
		roleName := "team member"
		if agent != nil && agent.Config != nil {
			roleName = agent.Config.Role
		}

		taskPrompt := strings.Join([]string{
			fmt.Sprintf("[BRAINSTORM TASKS from %s]", facilitatorName),
			"The team brainstormed these tasks:",
			safeProposals,
			"",
			fmt.Sprintf("Pick a task that matches your role as %s and start working on it.", roleName),
		}, "\n")

		if sessions == nil {
			continue
		}
		taskSent := sessions.WriteInkPrompt(sid, taskPrompt)
		if taskSent {
			if agent != nil {
				agent.mu.Lock()
				agent.LastPromptSentAt = time.Now()
				agent.mu.Unlock()
			}
		} else {
			agentName := sid
			if agent != nil && agent.Config != nil {
				agentName = agent.Config.Name
			}
			slog.Warn("Brainstorm task delivery failed", "agent", agentName)
		}
	}

	slog.Info("Brainstorm completed", "meeting", meeting.ID, "facilitator", facilitatorName)
}

// waitForBrainstormResponse blocks until the facilitator responds or timeout.
func (m *Manager) waitForBrainstormResponse(sessionID, speakerName string) string {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return fmt.Sprintf("%s was unreachable during brainstorm.", speakerName)
	}

	// Always create a fresh channel. If a prior brainstorm timed out
	// and the facilitator's late response landed in the old buffer,
	// reusing that channel would replay the previous round's message
	// into this one. Matches the pattern in standup.go:281.
	agent.mu.Lock()
	agent.BrainstormCh = make(chan string, 1)
	ch := agent.BrainstormCh
	agent.mu.Unlock()

	m.mu.RLock()
	timeout := m.config.BrainstormTimeout
	m.mu.RUnlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case response := <-ch:
		return response
	case <-timer.C:
		slog.Warn("Brainstorm response timeout", "agent", speakerName)
		return fmt.Sprintf("%s did not respond in time. Each team member should look for improvements matching their role.", speakerName)
	}
}

// SubmitBrainstormResponse records a brainstorm response from an agent.
// Returns true if the response was accepted.
func (m *Manager) SubmitBrainstormResponse(sessionID, proposals string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}

	sanitized := sanitizeForTerminal(proposals)

	agent.mu.Lock()
	ch := agent.BrainstormCh
	agent.mu.Unlock()

	if ch == nil {
		slog.Warn("Brainstorm response with no pending request", "session", sessionID)
		return false
	}

	select {
	case ch <- sanitized:
		slog.Info("Brainstorm response received", "session", sessionID, "len", len(sanitized))
		return true
	default:
		return false
	}
}

// endMeeting marks a meeting as inactive.
func (m *Manager) endMeeting(meetingID string) {
	m.mu.Lock()
	mtg, ok := m.meetings[meetingID]
	if ok {
		mtg.Active = false
	}
	m.mu.Unlock()

	if ok {
		m.broadcast("office:meetingEnded", map[string]interface{}{
			"id": meetingID,
		})
		m.emitEvent(MeetingEndedEvent{MeetingID: meetingID})
	}
}

// GetLastBrainstormAt returns when the last brainstorm occurred.
func (m *Manager) GetLastBrainstormAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastBrainstormAt
}

// EndMeeting ends a meeting by ID. Returns true if found.
func (m *Manager) EndMeeting(meetingID string) bool {
	m.mu.Lock()
	mtg, ok := m.meetings[meetingID]
	if ok {
		mtg.Active = false
	}
	m.mu.Unlock()
	if ok {
		m.emitEvent(MeetingEndedEvent{MeetingID: meetingID})
	}
	return ok
}

// startMeetingByID creates a meeting with a specific ID (for testing).
func (m *Manager) startMeetingByID(id, meetingType string, participants []string) *Meeting {
	m.mu.Lock()
	defer m.mu.Unlock()
	meeting := &Meeting{
		ID:           id,
		Type:         meetingType,
		Participants: participants,
		StartedAt:    time.Now(),
		Active:       true,
	}
	m.meetings[meeting.ID] = meeting
	return meeting
}
