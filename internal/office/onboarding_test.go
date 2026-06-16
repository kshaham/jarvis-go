package office

import (
	"strings"
	"testing"
)

func TestOnboarding_AllTopicsRenderForStrategic(t *testing.T) {
	cfg := &TeamMemberConfig{ID: "captain", Name: "Captain", Role: "Tech Lead", Tier: "strategic"}
	for _, topic := range OnboardingTopics("strategic") {
		body, ok := GetOnboardingTopic(topic, cfg, "/tmp/x", true)
		if !ok {
			t.Fatalf("%s: expected ok=true, got false", topic)
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s: body is empty", topic)
		}
	}
}

func TestOnboarding_DevLeadRequiresStrategic(t *testing.T) {
	worker := &TeamMemberConfig{ID: "dash", Name: "Dash", Role: "Frontend", Tier: "worker"}
	body, ok := GetOnboardingTopic(TopicDevLead, worker, "/tmp/x", true)
	if ok || body != "" {
		t.Errorf("worker calling dev-lead should 403; got ok=%v body_len=%d", ok, len(body))
	}
	// sanity: strategic does get it
	strat := &TeamMemberConfig{ID: "captain", Tier: "strategic"}
	body2, ok2 := GetOnboardingTopic(TopicDevLead, strat, "/tmp/x", true)
	if !ok2 || body2 == "" {
		t.Errorf("strategic should get dev-lead; ok=%v len=%d", ok2, len(body2))
	}
}

func TestOnboarding_UnknownTopic(t *testing.T) {
	cfg := &TeamMemberConfig{Tier: "strategic"}
	_, ok := GetOnboardingTopic(OnboardingTopic("bogus"), cfg, "/tmp/x", true)
	if ok {
		t.Error("unknown topic should return ok=false")
	}
}

func TestOnboarding_WorkerTopicListOmitsDevLead(t *testing.T) {
	worker := OnboardingTopics("worker")
	for _, t0 := range worker {
		if t0 == TopicDevLead {
			t.Fatalf("worker list should not contain dev-lead, got %v", worker)
		}
	}
	strat := OnboardingTopics("strategic")
	found := false
	for _, t0 := range strat {
		if t0 == TopicDevLead {
			found = true
		}
	}
	if !found {
		t.Error("strategic list should contain dev-lead")
	}
}

func TestOnboarding_RouterLinesTierAware(t *testing.T) {
	w := onboardingRouterLines("worker")
	s := onboardingRouterLines("strategic")
	if strings.Contains(w, "dev-lead") {
		t.Error("worker router should not mention dev-lead")
	}
	if !strings.Contains(s, "dev-lead") {
		t.Error("strategic router should mention dev-lead")
	}
	// sanity: core topics always present
	for _, core := range []string{"protocols", "browser", "dev-instance", "context-budget", "skills", "memory"} {
		if !strings.Contains(w, core) || !strings.Contains(s, core) {
			t.Errorf("router missing core topic %q", core)
		}
	}
}

func TestOnboarding_SkillsIncludesCustom(t *testing.T) {
	cfg := &TeamMemberConfig{
		ID:     "vex",
		Role:   "Frontend Engineer",
		Tier:   "worker",
		Skills: []string{"react-router-v7", "tailwind-v4"},
	}
	body, ok := GetOnboardingTopic(TopicSkills, cfg, "/tmp/x", true)
	if !ok {
		t.Fatal("skills topic should render")
	}
	if !strings.Contains(body, "react-router-v7") {
		t.Error("skills body should include custom skill names")
	}
}
