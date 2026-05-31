package office

import (
	"testing"
)

// agentWithTasks builds an AgentState with the given task statuses.
func agentWithTasks(statuses ...TaskStatus) *AgentState {
	a := NewAgentState("sess-x", "cfg-x", &TeamMemberConfig{ID: "cfg-x", Name: "X"})
	for i, st := range statuses {
		a.AddTask(&Task{ID: "t" + itoa(i), Title: "t", Status: st})
	}
	return a
}

// TestCalculateAgentScoreWithData_Grades drives the B / C / D grade arms
// of CalculateAgentScoreWithData (scoring.go ~67-74). The grade follows
// from overall = completion*0.4 + speed*0.2 + quality*0.2 + collab*0.2,
// with quality==completion and speed==0.8 when there are no failures.
func TestCalculateAgentScoreWithData_Grades(t *testing.T) {
	// speedScore = 1 - 0.5*failRate, quality = completionRate,
	// collab = min(1, (learnings+skills)/10).
	//
	// Grade A: 2/2 completed, 10 learnings => collab=1.0
	//   1.0*0.4 + 1.0*0.2 + 1.0*0.2 + 1.0*0.2 = 1.00 (>=0.9)
	aAgent := agentWithTasks(TaskCompleted, TaskCompleted)
	aLearning := &AgentLearningData{Learnings: make([]AgentLearning, 10)}
	if got := CalculateAgentScoreWithData(aAgent, aLearning); got.Grade != "A" {
		t.Errorf("expected grade A, got %q (overall=%.3f)", got.Grade, got.OverallScore)
	}

	// Grade B: 2/2 completed, no learnings (collab=0), no fails (speed=1)
	//   1.0*0.4 + 1.0*0.2 + 1.0*0.2 + 0 = 0.80 (>=0.8, <0.9)
	bAgent := agentWithTasks(TaskCompleted, TaskCompleted)
	if got := CalculateAgentScoreWithData(bAgent, nil); got.Grade != "B" {
		t.Errorf("expected grade B, got %q (overall=%.3f)", got.Grade, got.OverallScore)
	}

	// Grade C: 1/2 completed, 10 learnings (collab=1), no fails (speed=1)
	//   0.5*0.4 + 1.0*0.2 + 0.5*0.2 + 1.0*0.2 = 0.70 (>=0.7, <0.8)
	cAgent := agentWithTasks(TaskCompleted, TaskPending)
	cLearning := &AgentLearningData{Learnings: make([]AgentLearning, 10)}
	if got := CalculateAgentScoreWithData(cAgent, cLearning); got.Grade != "C" {
		t.Errorf("expected grade C, got %q (overall=%.3f)", got.Grade, got.OverallScore)
	}

	// Grade D: 2/4 completed, 8 learnings (collab=0.8), no fails (speed=1)
	//   0.5*0.4 + 1.0*0.2 + 0.5*0.2 + 0.8*0.2 = 0.66 (>=0.6, <0.7)
	dAgent := agentWithTasks(TaskCompleted, TaskCompleted, TaskPending, TaskPending)
	dLearning := &AgentLearningData{Learnings: make([]AgentLearning, 8)}
	if got := CalculateAgentScoreWithData(dAgent, dLearning); got.Grade != "D" {
		t.Errorf("expected grade D, got %q (overall=%.3f)", got.Grade, got.OverallScore)
	}

	// Grade F: all pending => completion=0, speed=1 (no fails) => 0.2 (<0.6)
	fAgent := agentWithTasks(TaskPending, TaskPending)
	if got := CalculateAgentScoreWithData(fAgent, nil); got.Grade != "F" {
		t.Errorf("expected grade F, got %q (overall=%.3f)", got.Grade, got.OverallScore)
	}
}

// TestCalculateTeamScores_SkipsNilConfig drives the `a.Config == nil`
// continue in CalculateTeamScores (scoring.go ~99-100). A registered
// agent whose Config is nulled is skipped, so only the valid agent's
// score is returned.
func TestCalculateTeamScores_SkipsNilConfig(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-good", "good", &TeamMemberConfig{ID: "good", Name: "Good", Tier: "worker"})
	m.RegisterAgent("sess-bad", "bad", &TeamMemberConfig{ID: "bad", Name: "Bad", Tier: "worker"})

	// Null the bad agent's config so the team-score loop skips it.
	bad := m.GetAgent("sess-bad")
	bad.mu.Lock()
	bad.Config = nil
	bad.mu.Unlock()

	scores := m.CalculateTeamScores()
	if len(scores) != 1 {
		t.Fatalf("expected exactly one score (nil-config agent skipped), got %d", len(scores))
	}
}
