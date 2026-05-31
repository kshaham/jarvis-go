package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- arg helpers ---

func TestGetStringArg(t *testing.T) {
	args := map[string]interface{}{
		"present": "value",
		"empty":   "",
		"number":  42,
	}
	if got := getStringArg(args, "present", "def"); got != "value" {
		t.Fatalf("present: got %q", got)
	}
	if got := getStringArg(args, "empty", "def"); got != "def" {
		t.Fatalf("empty should fall back to default, got %q", got)
	}
	if got := getStringArg(args, "missing", "def"); got != "def" {
		t.Fatalf("missing should fall back to default, got %q", got)
	}
	if got := getStringArg(args, "number", "def"); got != "def" {
		t.Fatalf("non-string should fall back to default, got %q", got)
	}
}

func TestRequireStringArg(t *testing.T) {
	args := map[string]interface{}{
		"present": "value",
		"empty":   "",
		"number":  7,
	}
	if v, ok := requireStringArg(args, "present"); !ok || v != "value" {
		t.Fatalf("present: got %q ok=%v", v, ok)
	}
	if _, ok := requireStringArg(args, "empty"); ok {
		t.Fatal("empty string should not satisfy require")
	}
	if _, ok := requireStringArg(args, "missing"); ok {
		t.Fatal("missing key should not satisfy require")
	}
	if _, ok := requireStringArg(args, "number"); ok {
		t.Fatal("non-string should not satisfy require")
	}
}

// --- ToolResult constructors ---

func TestToolSuccess_MarshalsIndentedJSON(t *testing.T) {
	res := toolSuccess(map[string]interface{}{"a": 1})
	if res.IsError {
		t.Fatal("toolSuccess should not be an error")
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", res.Content)
	}
	if !strings.Contains(res.Content[0].Text, `"a": 1`) {
		t.Fatalf("expected indented JSON, got %q", res.Content[0].Text)
	}
}

func TestToolSuccess_MarshalErrorBecomesToolError(t *testing.T) {
	// A channel cannot be marshaled, exercising the error branch.
	res := toolSuccess(make(chan int))
	if !res.IsError {
		t.Fatal("expected IsError for unmarshalable data")
	}
	if !strings.Contains(resultText(res), "Failed to marshal result") {
		t.Fatalf("expected marshal error message, got %q", resultText(res))
	}
}

func TestToolSuccessText(t *testing.T) {
	res := toolSuccessText("hello")
	if res.IsError {
		t.Fatal("should not be error")
	}
	if resultText(res) != "hello" {
		t.Fatalf("unexpected text: %q", resultText(res))
	}
}

func TestToolError(t *testing.T) {
	res := toolError("boom")
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	if resultText(res) != "boom" {
		t.Fatalf("unexpected text: %q", resultText(res))
	}
}

// --- writeResponse / writeError go through os.Stdout ---

func TestWriteResponse_EmitsJSONRPC(t *testing.T) {
	out := captureStdout(t, func() {
		writeResponse("req-1", map[string]interface{}{"foo": "bar"})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", resp.JSONRPC)
	}
	if resp.ID != "req-1" {
		t.Fatalf("expected id req-1, got %v", resp.ID)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok || m["foo"] != "bar" {
		t.Fatalf("unexpected result: %v", resp.Result)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %v", resp.Error)
	}
}

func TestWriteResponse_MarshalFailureFallsBackToError(t *testing.T) {
	out := captureStdout(t, func() {
		// chan cannot be marshaled -> writeResponse falls back to writeError.
		writeResponse(99, make(chan int))
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil {
		t.Fatal("expected an error response on marshal failure")
	}
	if resp.Error.Code != -32603 {
		t.Fatalf("expected -32603, got %d", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "Internal error") {
		t.Fatalf("expected internal error message, got %q", resp.Error.Message)
	}
}

func TestWriteError_EmitsErrorObject(t *testing.T) {
	out := captureStdout(t, func() {
		writeError(7, -32601, "Method not found: foo")
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil {
		t.Fatal("expected error object")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Method not found: foo" {
		t.Fatalf("unexpected message: %q", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatalf("expected no result, got %v", resp.Result)
	}
}

// --- handleRequest routing (main.go) ---

func rawParams(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return b
}

func TestHandleRequest_Initialize(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	})
	resp := decodeResponse(t, firstLine(out))
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if m["protocolVersion"] != protocolVersion {
		t.Fatalf("expected protocolVersion %q, got %v", protocolVersion, m["protocolVersion"])
	}
	si, ok := m["serverInfo"].(map[string]interface{})
	if !ok || si["name"] != serverName {
		t.Fatalf("unexpected serverInfo: %v", m["serverInfo"])
	}
}

func TestHandleRequest_InitializedNotificationsProduceNoOutput(t *testing.T) {
	for _, method := range []string{"initialized", "notifications/initialized"} {
		out := captureStdout(t, func() {
			handleRequest(JSONRPCRequest{JSONRPC: "2.0", Method: method})
		})
		if strings.TrimSpace(out) != "" {
			t.Fatalf("method %s should produce no output, got %q", method, out)
		}
	}
}

func TestHandleRequest_Ping(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: "p", Method: "ping"})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.ID != "p" {
		t.Fatalf("expected id p, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("ping should not error, got %v", resp.Error)
	}
}

func TestHandleRequest_UnknownMethod(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 5, Method: "does/not/exist"})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected -32601 method not found, got %v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "does/not/exist") {
		t.Fatalf("expected method name in message, got %q", resp.Error.Message)
	}
}

func TestHandleRequest_ToolsList(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	})
	resp := decodeResponse(t, firstLine(out))
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	tools, ok := m["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %T", m["tools"])
	}
	if len(tools) < len(allToolDefs) {
		t.Fatalf("expected at least %d tools, got %d", len(allToolDefs), len(tools))
	}
}

func TestHandleRequest_ToolsCall_InvalidParams(t *testing.T) {
	out := captureStdout(t, func() {
		// params is a JSON array where an object is expected -> unmarshal error.
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: json.RawMessage(`["bad"]`)})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 invalid params, got %v", resp.Error)
	}
}

func TestHandleRequest_ToolsCall_NilArgumentsDefaulted(t *testing.T) {
	// list_dynamic_tools ignores args, so we can call without a server.
	// Omitting "arguments" exercises the nil-arguments default path.
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{
			JSONRPC: "2.0", ID: 4, Method: "tools/call",
			Params: rawParams(t, map[string]interface{}{"name": "list_dynamic_tools"}),
		})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected ToolResult map, got %T", resp.Result)
	}
	if _, ok := m["content"]; !ok {
		t.Fatalf("expected content key in tool result, got %v", m)
	}
}

func TestHandleRequest_ResourcesList(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 6, Method: "resources/list"})
	})
	resp := decodeResponse(t, firstLine(out))
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	resources, ok := m["resources"].([]interface{})
	if !ok || len(resources) != len(allResources) {
		t.Fatalf("expected %d resources, got %v", len(allResources), m["resources"])
	}
}

func TestHandleRequest_ResourcesRead_InvalidParams(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 7, Method: "resources/read", Params: json.RawMessage(`12345`)})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602, got %v", resp.Error)
	}
}

func TestHandleRequest_ResourcesRead_UnknownURI(t *testing.T) {
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{
			JSONRPC: "2.0", ID: 8, Method: "resources/read",
			Params: rawParams(t, map[string]interface{}{"uri": "jarvis://nope"}),
		})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 unknown resource, got %v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "Unknown resource") {
		t.Fatalf("expected unknown resource message, got %q", resp.Error.Message)
	}
}

func TestHandleRequest_ResourcesRead_Success(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[{"id":"s1"}]`))
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{
			JSONRPC: "2.0", ID: 9, Method: "resources/read",
			Params: rawParams(t, map[string]interface{}{"uri": "jarvis://sessions"}),
		})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	contents, ok := m["contents"].([]interface{})
	if !ok || len(contents) != 1 {
		t.Fatalf("expected 1 content entry, got %v", m["contents"])
	}
}

func TestHandleRequest_ResourcesRead_UpstreamError(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "upstream boom"))
	out := captureStdout(t, func() {
		handleRequest(JSONRPCRequest{
			JSONRPC: "2.0", ID: 10, Method: "resources/read",
			Params: rawParams(t, map[string]interface{}{"uri": "jarvis://agents"}),
		})
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Fatalf("expected -32603 resource read error, got %v", resp.Error)
	}
}
