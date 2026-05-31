package main

import (
	"net/http"
	"testing"
)

// TestHandlers_UpstreamErrorPropagation drives every handler whose only
// remaining uncovered statement is its `return toolError(err.Error())` branch.
// Each handler is given valid args plus a server that always returns 500, so the
// error path is exercised. Office handlers need JARVIS_SESSION_ID; monitoring
// handlers resolve a session first, so the sessions list is served valid and the
// per-session endpoint is what 500s.
func TestHandlers_UpstreamErrorPropagation(t *testing.T) {
	setEnv(t, "JARVIS_SESSION_ID", "sid")

	// Simple handlers: a single 500 server is enough to trip their error branch.
	simple := []struct {
		name string
		fn   ToolHandler
		args map[string]interface{}
	}{
		{"get_session", handleGetSession, map[string]interface{}{"sessionId": "x"}},
		{"stop_session", handleStopSession, map[string]interface{}{"sessionId": "x"}},
		{"send_to_session", handleSendToSession, map[string]interface{}{"sessionId": "x", "input": "i"}},
		{"get_session_output", handleGetSessionOutput, map[string]interface{}{"sessionId": "x"}},
		{"get_agent_status", handleGetAgentStatus, map[string]interface{}{"agentId": "x"}},
		{"get_agent_transcript", handleGetAgentTranscript, map[string]interface{}{"agentId": "x"}},
		{"add_task", handleAddTask, map[string]interface{}{"prompt": "p"}},
		{"list_tasks", handleListTasks, map[string]interface{}{}},
		{"get_task", handleGetTask, map[string]interface{}{"taskId": "x"}},
		{"remove_task", handleRemoveTask, map[string]interface{}{"taskId": "x"}},
		{"list_skills", handleListSkills, map[string]interface{}{}},
		{"get_skill", handleGetSkill, map[string]interface{}{"skillName": "s"}},
		{"create_skill", handleCreateSkill, map[string]interface{}{"skillName": "s", "content": "c"}},
		{"search_skills", handleSearchSkills, map[string]interface{}{"query": "q"}},
		{"delete_skill", handleDeleteSkill, map[string]interface{}{"skillName": "s"}},
		{"office_task_self_add", handleOfficeTaskSelfAdd, map[string]interface{}{"title": "t", "description": "d"}},
		{"office_task_start", handleOfficeTaskStart, map[string]interface{}{"taskId": "x"}},
		{"office_task_complete", handleOfficeTaskComplete, map[string]interface{}{"taskId": "x"}},
		{"office_task_progress", handleOfficeTaskProgress, map[string]interface{}{"taskId": "x", "note": "n"}},
		{"office_task_blocked", handleOfficeTaskBlocked, map[string]interface{}{"taskId": "x", "reason": "r"}},
		{"office_task_set_criteria", handleOfficeTaskSetCriteria, map[string]interface{}{"taskId": "x", "op": "set", "criteria": []interface{}{"a"}}},
		{"office_standup_respond", handleOfficeStandupRespond, map[string]interface{}{"content": "c"}},
		{"office_reflect", handleOfficeReflect, map[string]interface{}{"learning": "l"}},
		{"office_team_roster", handleOfficeTeamRoster, map[string]interface{}{}},
		{"office_send_message", handleOfficeSendMessage, map[string]interface{}{"toSessionId": "o", "content": "c"}},
		{"office_memory_read", handleOfficeMemoryRead, map[string]interface{}{}},
		{"office_memory_write", handleOfficeMemoryWrite, map[string]interface{}{"content": "c"}},
		{"office_memory_append", handleOfficeMemoryAppend, map[string]interface{}{"entry": "e"}},
		{"office_project_invariants_set", handleOfficeProjectInvariantsSet, map[string]interface{}{"invariants": []interface{}{"a"}}},
	}
	for _, tc := range simple {
		t.Run(tc.name, func(t *testing.T) {
			withServer(t, recordingHandler(&captureRequest{}, 500, "upstream down"))
			res := tc.fn(tc.args)
			if !res.IsError {
				t.Fatalf("%s: expected error result when upstream returns 500", tc.name)
			}
		})
	}

	// Monitoring handlers: serve a valid sessions list so findSessionByWorkingDir
	// succeeds, then 500 the per-session endpoint to hit the second error branch.
	monitoring := []struct {
		name string
		fn   ToolHandler
	}{
		{"get_git_status", handleGetGitStatus},
		{"get_build_status", handleGetBuildStatus},
		{"get_test_status", handleGetTestStatus},
		{"get_integration_state", handleGetIntegrationState},
	}
	for _, tc := range monitoring {
		t.Run(tc.name, func(t *testing.T) {
			// Serve the sessions list so findSessionByWorkingDir succeeds, then
			// 500 the per-session endpoint to hit the second error branch.
			withServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/sessions" {
					w.WriteHeader(200)
					_, _ = w.Write([]byte(sessionsListJSON))
					return
				}
				w.WriteHeader(500)
				_, _ = w.Write([]byte("endpoint down"))
			})
			res := tc.fn(map[string]interface{}{"workingDir": "/proj/a"})
			if !res.IsError {
				t.Fatalf("%s: expected error when per-session endpoint 500s", tc.name)
			}
		})
	}
}
