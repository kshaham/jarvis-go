package office

// AgentScore holds computed performance metrics for a single agent.
type AgentScore struct {
	ConfigID           string  `json:"configId"`
	Name               string  `json:"name"`
	CompletionRate     float64 `json:"completionRate"`
	SpeedScore         float64 `json:"speedScore"`
	QualityScore       float64 `json:"qualityScore"`
	CollaborationScore float64 `json:"collaborationScore"`
	OverallScore       float64 `json:"overallScore"`
	Grade              string  `json:"grade"`
}

// CalculateAgentScore computes a score for an agent based on task history and learnings.
func CalculateAgentScore(agent *AgentState) AgentScore {
	tasks := agent.GetTasks()
	total := float64(len(tasks))
	completed := 0.0
	for _, t := range tasks {
		if t.Status == TaskCompleted {
			completed++
		}
	}

	completionRate := 0.0
	if total > 0 {
		completionRate = completed / total
	}

	// Speed score: default good unless there are many failed tasks.
	speedScore := 0.8
	if total > 0 {
		failed := 0.0
		for _, t := range tasks {
			if t.Status == TaskFailed {
				failed++
			}
		}
		failRate := failed / total
		speedScore = 1.0 - (failRate * 0.5)
		if speedScore < 0 {
			speedScore = 0
		}
	}

	// Quality: proxy for completion rate (fewer failures = higher quality).
	qualityScore := completionRate

	// Collaboration: based on learning count and skills discovered.
	ld := LoadLearningData(agent.ConfigID)
	collabScore := minFloat(1.0, float64(len(ld.Learnings)+len(ld.Skills))/10.0)

	overall := completionRate*0.4 + speedScore*0.2 + qualityScore*0.2 + collabScore*0.2

	grade := "F"
	switch {
	case overall >= 0.9:
		grade = "A"
	case overall >= 0.8:
		grade = "B"
	case overall >= 0.7:
		grade = "C"
	case overall >= 0.6:
		grade = "D"
	}

	name := agent.ConfigID
	if agent.Config != nil {
		name = agent.Config.Name
	}

	return AgentScore{
		ConfigID:           agent.ConfigID,
		Name:               name,
		CompletionRate:     completionRate,
		SpeedScore:         speedScore,
		QualityScore:       qualityScore,
		CollaborationScore: collabScore,
		OverallScore:       overall,
		Grade:              grade,
	}
}

// CalculateTeamScores returns scores for all registered agents.
func (m *Manager) CalculateTeamScores() []AgentScore {
	roster := m.GetRoster()
	scores := make([]AgentScore, 0, len(roster))
	for _, a := range roster {
		if a.Config == nil {
			continue
		}
		scores = append(scores, CalculateAgentScore(a))
	}
	return scores
}

// GetTopPerformers returns up to n agents sorted by OverallScore descending.
func GetTopPerformers(scores []AgentScore, n int) []AgentScore {
	sorted := sortScores(scores, true)
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// GetBottomPerformers returns up to n agents sorted by OverallScore ascending.
func GetBottomPerformers(scores []AgentScore, n int) []AgentScore {
	sorted := sortScores(scores, false)
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// sortScores returns a copy of scores sorted by OverallScore.
// If descending is true, highest score first; otherwise lowest first.
func sortScores(scores []AgentScore, descending bool) []AgentScore {
	result := make([]AgentScore, len(scores))
	copy(result, scores)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			swap := false
			if descending {
				swap = result[j].OverallScore > result[i].OverallScore
			} else {
				swap = result[j].OverallScore < result[i].OverallScore
			}
			if swap {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// minFloat returns the smaller of two float64 values.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
