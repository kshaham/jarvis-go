package handler

// Round 5 -- residual office.go arms:
//   - decodeBody failure (400) for the handlers whose malformed-body arm was
//     still uncovered: OfficeUpdateTeamTemplate, ConciergeSpawnTeam,
//     OfficeAgentTaskStart, OfficePutMemberVoice, OfficeAgentSkillCreate.
//   - OfficeAgentGetSkills: the skills==nil normalisation arm when the skills
//     dir exists but holds no sub-directories.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/teamtemplates"
)

// malformedReq builds a POST/PUT carrying a body that fails JSON decoding.
func malformedReq(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestOfficeDecodeBodyFailures_Return400(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.TeamTemplateStore = teamtemplates.NewStore(t.TempDir())

	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
		path    string
		urlKey  string
		urlVal  string
	}{
		{"ConciergeSpawnTeam", ctx.ConciergeSpawnTeam, "POST", "/api/office/concierge/spawn", "", ""},
		{"OfficeAgentTaskStart", ctx.OfficeAgentTaskStart, "POST", "/api/office/agent/task/start", "", ""},
		{"OfficePutMemberVoice", ctx.OfficePutMemberVoice, "PUT", "/api/office/members/voice", "", ""},
		{"OfficeAgentSkillCreate", ctx.OfficeAgentSkillCreate, "POST", "/api/office/agent/skill", "", ""},
		{"OfficeAgentTaskComplete", ctx.OfficeAgentTaskComplete, "POST", "/api/office/agent/task/complete", "", ""},
		{"OfficeAgentTaskProgress", ctx.OfficeAgentTaskProgress, "POST", "/api/office/agent/task/progress", "", ""},
		{"OfficeAgentTaskTriage", ctx.OfficeAgentTaskTriage, "POST", "/api/office/agent/task/triage", "", ""},
		{"OfficeAgentTaskCriteria", ctx.OfficeAgentTaskCriteria, "POST", "/api/office/agent/task/criteria", "", ""},
		{"OfficeAgentFileLock", ctx.OfficeAgentFileLock, "POST", "/api/office/agent/file-lock", "", ""},
		// A custom (non-built-in) template id so the built-in 403 guard is skipped.
		{"OfficeUpdateTeamTemplate", ctx.OfficeUpdateTeamTemplate, "PUT", "/api/office/templates/custom-xyz", "id", "custom-xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := malformedReq(tc.method, tc.path)
			if tc.urlKey != "" {
				req = withURLParam(req, tc.urlKey, tc.urlVal)
			}
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 on malformed body, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestOfficeAgentGetSkills_EmptyDirYieldsEmptyArray(t *testing.T) {
	// newTimelineContext sets HOME to its own temp dir; override it AFTER so
	// our crafted skills dir is the one OfficeAgentGetSkills reads.
	ctx := newTimelineContext(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A skills dir that exists but contains only files (no sub-directories)
	// drives the skills==nil -> empty-slice normalisation arm.
	skillsDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A loose file (not a directory) must be ignored by the dir filter.
	if err := os.WriteFile(filepath.Join(skillsDir, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetSkills(rec, httptest.NewRequest("GET", "/api/office/agent/skills", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	skills, ok := resp["skills"].([]interface{})
	if !ok {
		t.Fatalf("expected skills array, got %T", resp["skills"])
	}
	if len(skills) != 0 {
		t.Errorf("expected empty skills (only a loose file present), got %d", len(skills))
	}
}
