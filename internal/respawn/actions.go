package respawn

import (
	"fmt"
	"log/slog"
	"time"
)

// onEnterState is called after every successful state transition. It dispatches
// the appropriate action for the new state (send prompt, start timer, etc.).
//
// This method is called outside the main lock, so it is safe to call sendToSession
// or setState internally.
func (rc *Controller) onEnterState(newState RespawnState) {
	switch newState {
	case StateConfirmingIdle:
		rc.startConfirmationTimer()
	case StateSendingUpdate:
		rc.startSendUpdate()
	case StateWaitingUpdate:
		rc.startWaitingTimer("update")
	case StateSendingClear:
		rc.startSendClear()
	case StateWaitingClear:
		rc.startWaitingTimer("clear")
	case StateSendingInit:
		rc.startSendInit()
	case StateWaitingInit:
		rc.startWaitingTimer("init")
	case StateMonitoringInit:
		rc.startMonitoringInit()
	case StateSendingKickstart:
		rc.startSendKickstart()
	case StateWaitingKickstart:
		rc.startWaitingTimer("kickstart")
	case StateWatching:
		rc.signals.Reset()
	}
}

// startConfirmationTimer waits the confirmation delay before transitioning
// to SendingUpdate. If new activity arrives, OnWorkingDetected will cancel
// the flow by returning to Watching.
func (rc *Controller) startConfirmationTimer() {
	rc.mu.Lock()
	delayMs := rc.config.ConfirmationDelayMs
	rc.mu.Unlock()

	rc.logAction("detection", "confirming idle, waiting for silence")

	rc.timers.Start("confirm-idle", time.Duration(delayMs)*time.Millisecond, "confirming idle", func() {
		rc.mu.Lock()
		state := rc.state
		rc.mu.Unlock()

		if state != StateConfirmingIdle {
			return
		}

		rc.logAction("detection", "idle confirmed")
		_ = rc.setState(StateSendingUpdate)
	})
}

// startSendUpdate sends the update prompt to the session after the inter-step delay.
func (rc *Controller) startSendUpdate() {
	rc.mu.Lock()
	prompt := rc.config.UpdatePrompt
	delayMs := rc.config.StepDelayMs
	rc.mu.Unlock()

	rc.timers.Start("step-delay", time.Duration(delayMs)*time.Millisecond, "delay before update", func() {
		// Re-check state: a transition out of SendingUpdate (e.g. back to
		// Watching) during the delay must abort the send, or we'd inject a
		// stale command into a session that's moved on.
		rc.mu.Lock()
		state := rc.state
		rc.mu.Unlock()
		if state != StateSendingUpdate {
			return
		}

		rc.logAction("command", fmt.Sprintf("sending update: %s", truncate(prompt, 50)))

		if !rc.sendToSession(prompt+"\r", "update") {
			slog.Warn("respawn: update send failed, returning to watching")
			_ = rc.setState(StateWatching)
			return
		}

		rc.emitEvent(StepSentEvent{Step: "update", Input: prompt})
		_ = rc.setState(StateWaitingUpdate)
	})
}

// startSendClear sends /clear to the session after the inter-step delay.
func (rc *Controller) startSendClear() {
	rc.mu.Lock()
	delayMs := rc.config.StepDelayMs
	rc.mu.Unlock()

	rc.timers.Start("step-delay", time.Duration(delayMs)*time.Millisecond, "delay before /clear", func() {
		rc.mu.Lock()
		state := rc.state
		rc.mu.Unlock()
		if state != StateSendingClear {
			return
		}

		rc.logAction("command", "sending /clear")

		if !rc.sendToSession(ClearCommand+"\r", "clear") {
			slog.Warn("respawn: /clear send failed, returning to watching")
			_ = rc.setState(StateWatching)
			return
		}

		rc.emitEvent(StepSentEvent{Step: "clear", Input: ClearCommand})
		_ = rc.setState(StateWaitingClear)
	})
}

// startSendInit sends /init to the session after the inter-step delay.
func (rc *Controller) startSendInit() {
	rc.mu.Lock()
	delayMs := rc.config.StepDelayMs
	rc.mu.Unlock()

	rc.timers.Start("step-delay", time.Duration(delayMs)*time.Millisecond, "delay before /init", func() {
		rc.mu.Lock()
		state := rc.state
		rc.mu.Unlock()
		if state != StateSendingInit {
			return
		}

		rc.logAction("command", "sending /init")

		if !rc.sendToSession(InitCommand+"\r", "init") {
			slog.Warn("respawn: /init send failed, returning to watching")
			_ = rc.setState(StateWatching)
			return
		}

		rc.emitEvent(StepSentEvent{Step: "init", Input: InitCommand})
		_ = rc.setState(StateWaitingInit)
	})
}

// startMonitoringInit watches for 3 seconds to see if /init triggered work.
// If no work is detected and a kickstart prompt is configured, transitions to SendingKickstart.
func (rc *Controller) startMonitoringInit() {
	rc.logAction("step", "monitoring if /init triggered work")

	rc.timers.Start("init-monitor", 3*time.Second, "grace period for /init", func() {
		rc.mu.Lock()
		state := rc.state
		kickstartPrompt := rc.config.KickstartPrompt
		rc.mu.Unlock()

		if state != StateMonitoringInit {
			return
		}

		rc.emitEvent(StepCompletedEvent{Step: "init"})

		if kickstartPrompt != "" {
			rc.logAction("step", "/init did not trigger work, sending kickstart")
			_ = rc.setState(StateSendingKickstart)
		} else {
			rc.logAction("step", "/init complete, cycle done")
			rc.completeCycle()
		}
	})
}

// startSendKickstart sends the kickstart prompt to the session after the inter-step delay.
func (rc *Controller) startSendKickstart() {
	rc.mu.Lock()
	prompt := rc.config.KickstartPrompt
	delayMs := rc.config.StepDelayMs
	rc.mu.Unlock()

	if prompt == "" {
		rc.completeCycle()
		return
	}

	rc.timers.Start("step-delay", time.Duration(delayMs)*time.Millisecond, "delay before kickstart", func() {
		rc.mu.Lock()
		state := rc.state
		rc.mu.Unlock()
		if state != StateSendingKickstart {
			return
		}

		rc.logAction("command", fmt.Sprintf("sending kickstart: %s", truncate(prompt, 50)))

		if !rc.sendToSession(prompt+"\r", "kickstart") {
			slog.Warn("respawn: kickstart send failed, returning to watching")
			_ = rc.setState(StateWatching)
			return
		}

		rc.emitEvent(StepSentEvent{Step: "kickstart", Input: prompt})
		_ = rc.setState(StateWaitingKickstart)
	})
}

// startWaitingTimer starts a timer for waiting states. When the timer fires,
// it transitions to the next step in the cycle.
func (rc *Controller) startWaitingTimer(step string) {
	rc.mu.Lock()
	delayMs := rc.config.ConfirmationDelayMs
	rc.mu.Unlock()

	timerName := "wait-" + step
	rc.timers.Start(timerName, time.Duration(delayMs)*time.Millisecond, "waiting for "+step+" completion", func() {
		rc.mu.Lock()
		state := rc.state
		sendClear := rc.config.SendClear
		sendInit := rc.config.SendInit
		kickstartPrompt := rc.config.KickstartPrompt
		rc.mu.Unlock()

		switch state {
		case StateWaitingUpdate:
			rc.emitEvent(StepCompletedEvent{Step: "update"})
			rc.advanceAfterUpdate(sendClear, sendInit)
		case StateWaitingClear:
			rc.emitEvent(StepCompletedEvent{Step: "clear"})
			rc.advanceAfterClear(sendInit)
		case StateWaitingInit:
			rc.emitEvent(StepCompletedEvent{Step: "init"})
			if kickstartPrompt != "" {
				_ = rc.setState(StateMonitoringInit)
			} else {
				rc.completeCycle()
			}
		case StateWaitingKickstart:
			rc.emitEvent(StepCompletedEvent{Step: "kickstart"})
			rc.completeCycle()
		}
	})
}

// advanceAfterUpdate moves to the next step after update completes.
func (rc *Controller) advanceAfterUpdate(sendClear, sendInit bool) {
	if sendClear {
		_ = rc.setState(StateSendingClear)
	} else if sendInit {
		_ = rc.setState(StateSendingInit)
	} else {
		rc.completeCycle()
	}
}

// advanceAfterClear moves to the next step after /clear completes.
func (rc *Controller) advanceAfterClear(sendInit bool) {
	if sendInit {
		_ = rc.setState(StateSendingInit)
	} else {
		rc.completeCycle()
	}
}

// completeCycle finishes the current respawn cycle and returns to Watching.
func (rc *Controller) completeCycle() {
	rc.logAction("cycle", "respawn cycle complete")
	_ = rc.setState(StateWatching)
}

// logAction emits an ActionLogEvent for UI display.
func (rc *Controller) logAction(actionType, detail string) {
	rc.emitEvent(ActionLogEvent{Type: actionType, Detail: detail})
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
