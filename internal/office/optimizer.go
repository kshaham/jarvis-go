package office

import (
	"fmt"
	"log/slog"
	"strings"
)

// PromptOptimization holds a performance suggestion for an underperforming agent.
type PromptOptimization struct {
	TargetConfigID string `json:"targetConfigId"`
	Suggestion     string `json:"suggestion"`
	BasedOn        string `json:"basedOn"`
}

// AnalyzeAndOptimize compares top and bottom performers and generates improvement
// suggestions. Returns nil when the team is too small to compare (fewer than 3 agents).
func (m *Manager) AnalyzeAndOptimize() []PromptOptimization {
	scores := m.CalculateTeamScores()
	if len(scores) < 3 {
		return nil
	}

	top := GetTopPerformers(scores, 2)
	bottom := GetBottomPerformers(scores, 2)

	var opts []PromptOptimization
	for _, b := range bottom {
		// Skip if the bottom performer is also in the top performers list
		// (can happen with ties in very small teams).
		isTopPerformer := false
		for _, t := range top {
			if t.ConfigID == b.ConfigID {
				isTopPerformer = true
				break
			}
		}
		if isTopPerformer {
			continue
		}

		topLearnings := LoadLearningData(top[0].ConfigID)
		bottomLearnings := LoadLearningData(b.ConfigID)

		suggestion := buildOptimizationSuggestion(top[0], b, topLearnings, bottomLearnings)
		opts = append(opts, PromptOptimization{
			TargetConfigID: b.ConfigID,
			Suggestion:     suggestion,
			BasedOn:        fmt.Sprintf("%s (score: %.0f%%)", top[0].Name, top[0].OverallScore*100),
		})
	}
	return opts
}

// InjectOptimizationSuggestions sends optimization hints to bottom-performing agents
// via WriteInkPrompt. Called after each standup cycle.
func (m *Manager) InjectOptimizationSuggestions(opts []PromptOptimization) {
	if len(opts) == 0 {
		return
	}

	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions == nil {
		return
	}

	for _, opt := range opts {
		agent := m.GetAgentByConfig(opt.TargetConfigID)
		if agent == nil {
			continue
		}
		sent := sessions.WriteInkPrompt(agent.SessionID(), opt.Suggestion)
		if sent {
			agentName := opt.TargetConfigID
			if agent.Config != nil {
				agentName = agent.Config.Name
			}
			slog.Info("Optimization suggestion injected", "agent", agentName, "basedOn", opt.BasedOn)
		}
	}
}

func buildOptimizationSuggestion(top, bottom AgentScore, topL, bottomL *AgentLearningData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[PERFORMANCE INSIGHT] Your current score: %.0f%% (%s). ", bottom.OverallScore*100, bottom.Grade)
	fmt.Fprintf(&sb, "Top performer %s scores %.0f%%.\n\n", top.Name, top.OverallScore*100)

	if top.CompletionRate > bottom.CompletionRate+0.1 {
		sb.WriteString("Focus on completing tasks before starting new ones.\n")
	}

	if top.CollaborationScore > bottom.CollaborationScore+0.1 {
		sb.WriteString("Reflect more often after completing tasks using `cm reflect`. Sharing learnings improves team performance.\n")
	}

	// Share the top performer's most recent learning as a tip.
	if len(topL.Learnings) > 0 {
		mostRecent := topL.Learnings[len(topL.Learnings)-1]
		fmt.Fprintf(&sb, "\nTip from %s: %s\n", top.Name, mostRecent.Content)
	}

	// Suggest a skill the top performer uses that the bottom performer lacks.
	if len(topL.Skills) > 0 {
		topSkill := topL.Skills[0]
		alreadyHas := false
		for _, s := range bottomL.Skills {
			if strings.EqualFold(s.Name, topSkill.Name) {
				alreadyHas = true
				break
			}
		}
		if !alreadyHas {
			fmt.Fprintf(&sb, "Consider trying the skill: %s - %s\n", topSkill.Name, topSkill.Description)
		}
	}

	_ = bottomL // suppress unused variable warning; used above

	return sb.String()
}
