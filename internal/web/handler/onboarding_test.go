package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claudeman/jarvis-go/internal/office"
)

func TestOfficeAgentOnboarding_HappyPath(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-vex&topic=protocols", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cm task ack") {
		t.Error("protocol body should mention cm task ack")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("expected text/plain, got %s", ct)
	}
}

func TestOfficeAgentOnboarding_WorkerForbiddenFromDevLead(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-vex&topic=dev-lead", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentOnboarding_StrategicCanReadDevLead(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-cap", "captain", &office.TeamMemberConfig{ID: "captain", Name: "Captain", Tier: "strategic"})
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-cap&topic=dev-lead", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cm dev announce") {
		t.Error("dev-lead body should reference cm dev announce")
	}
}

func TestOfficeAgentOnboarding_UnknownTopic(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-vex&topic=bogus", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOfficeAgentOnboarding_MissingTopic(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-vex", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentOnboarding_UnknownAgent(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/onboarding?sessionId=s-nobody&topic=protocols", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentOnboarding(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
