package handler

// Happy-path coverage for office.go agent-scoped handlers that need at least
// one registered agent: messaging, the ask/answer rotation, self-added tasks,
// standup with an active session, and task start. Agents are registered
// directly on the OfficeManager (no subprocess) via stubSessions.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// registerTwoAgents wires two agents into the manager so cross-agent message /
// ask flows have valid endpoints.
func registerTwoAgents(ctx *Context) (string, string) {
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("sess-a", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Role: "lead"})
	mgr.RegisterAgent("sess-b", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "worker"})
	return "sess-a", "sess-b"
}

func TestOfficePostMessage_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)

	rec := postJSON(t, ctx.OfficePostMessage, "/api/office/messages", map[string]interface{}{
		"fromSessionId": a, "toSessionId": b, "content": "hello bolt",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("post message: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The message should appear in the message log.
	resp := getJSON(t, ctx.OfficeGetMessages, "/api/office/messages")
	msgs, _ := resp["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Error("expected at least one message after send")
	}
}

func TestOfficePostMessage_TooLong(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'x'
	}
	rec := postJSON(t, ctx.OfficePostMessage, "/api/office/messages", map[string]interface{}{
		"fromSessionId": a, "toSessionId": b, "content": string(long),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-long message: expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentMessage_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)
	rec := postJSON(t, ctx.OfficeAgentMessage, "/api/office/agent/message", map[string]interface{}{
		"fromSessionId": a, "toSessionId": b, "content": "agent to agent",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent message: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentMessage_TooLong(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)
	long := make([]byte, 8001)
	for i := range long {
		long[i] = 'y'
	}
	rec := postJSON(t, ctx.OfficeAgentMessage, "/api/office/agent/message", map[string]interface{}{
		"fromSessionId": a, "toSessionId": b, "content": string(long),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-long agent message: expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentAskAnswer_FullFlow(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)

	// Agent A asks (short wait so the blocking call returns quickly without an
	// answer). The handler should 200 with a status field.
	recAsk := postJSON(t, ctx.OfficeAgentAsk, "/api/office/agent/ask", map[string]interface{}{
		"fromSessionId": a, "content": "where is the config?", "waitSeconds": 1,
	})
	if recAsk.Code != http.StatusOK {
		t.Fatalf("ask: expected 200, got %d: %s", recAsk.Code, recAsk.Body.String())
	}

	// Agent B lists pending questions addressed to it.
	reqList := httptest.NewRequest("GET", "/api/office/agent/ask/list?sessionId="+b, nil)
	recList := httptest.NewRecorder()
	ctx.OfficeAgentListQuestions(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("list questions: expected 200, got %d", recList.Code)
	}
}

func TestOfficeAgentListQuestions_MissingSession400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("GET", "/api/office/agent/ask/list", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentListQuestions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sessionId: expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskSelfAdd_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, _ := registerTwoAgents(ctx)
	rec := postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": a, "title": "Investigate flaky test", "priority": "high",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("self-add task: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentGetTasks_RegisteredAgent(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, _ := registerTwoAgents(ctx)
	// Add a task so the response carries it.
	postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": a, "title": "do thing",
	})
	req := httptest.NewRequest("GET", "/api/office/agent/tasks?sessionId="+a, nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tasks: expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) == 0 {
		t.Error("expected at least one task for the registered agent")
	}
}

func TestOfficeGetStandup_ActiveAfterStart(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, b := registerTwoAgents(ctx)

	// Start a standup with both participants.
	recStart := postJSON(t, ctx.OfficePostStandup, "/api/office/standup", map[string]interface{}{
		"participantIds": []string{a, b},
	})
	if recStart.Code != http.StatusOK {
		t.Fatalf("start standup: expected 200, got %d: %s", recStart.Code, recStart.Body.String())
	}

	// GetStandup now returns a non-nil active standup with participants.
	resp := getJSON(t, ctx.OfficeGetStandup, "/api/office/standup")
	su, _ := resp["standup"].(map[string]interface{})
	if su == nil {
		t.Fatal("expected non-nil standup after start")
	}
	parts, _ := su["participants"].([]interface{})
	if len(parts) != 2 {
		t.Errorf("expected 2 participants, got %d", len(parts))
	}

	// A user message during the active standup -> 200.
	recMsg := postJSON(t, ctx.OfficePostStandupUserMessage, "/api/office/standup/message", map[string]interface{}{
		"content": "carry on",
	})
	if recMsg.Code != http.StatusOK {
		t.Fatalf("standup user message: expected 200, got %d", recMsg.Code)
	}

	// Stop it.
	recStop := postJSON(t, ctx.OfficePostStandup, "/api/office/standup", map[string]interface{}{"action": "stop"})
	if recStop.Code != http.StatusOK {
		t.Fatalf("stop standup: expected 200, got %d", recStop.Code)
	}
}

func TestOfficeAgentTaskStart_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a, _ := registerTwoAgents(ctx)
	// Self-add a task, then start it.
	postJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId": a, "title": "startable",
	})
	// Fetch the task id.
	req := httptest.NewRequest("GET", "/api/office/agent/tasks?sessionId="+a, nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetTasks(rec, req)
	resp := decodeMap(t, rec)
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) == 0 {
		t.Fatal("no task to start")
	}
	task0, _ := tasks[0].(map[string]interface{})
	taskID, _ := task0["id"].(string)

	recStart := postJSON(t, ctx.OfficeAgentTaskStart, "/api/office/agent/task/start", map[string]interface{}{
		"sessionId": a, "taskId": taskID,
	})
	if recStart.Code != http.StatusOK {
		t.Fatalf("task start: expected 200, got %d: %s", recStart.Code, recStart.Body.String())
	}
}
