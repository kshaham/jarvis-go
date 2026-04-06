package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "jarvis-mcp"
	serverVersion   = "0.1.0"
)

func main() {
	log.SetOutput(os.Stderr) // MCP uses stdout for protocol messages

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		handleRequest(req)
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("stdin read error: %v", err)
	}
}

func handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		handleInitialize(req)

	case "initialized":
		// Notification, no response needed
		return

	case "notifications/initialized":
		// Alternative notification path, no response needed
		return

	case "tools/list":
		handleToolsList(req)

	case "tools/call":
		handleToolsCall(req)

	case "resources/list":
		handleResourcesList(req)

	case "resources/read":
		handleResourcesRead(req)

	case "ping":
		writeResponse(req.ID, map[string]interface{}{})

	default:
		writeError(req.ID, -32601, "Method not found: "+req.Method)
	}
}

func handleInitialize(req JSONRPCRequest) {
	result := InitializeResult{
		ProtocolVersion: protocolVersion,
		ServerInfo: ServerInfo{
			Name:    serverName,
			Version: serverVersion,
		},
		Capabilities: ServerCapabilities{
			Tools:     &ToolsCapability{},
			Resources: &ResourcesCapability{},
		},
	}
	writeResponse(req.ID, result)
}

func handleToolsList(req JSONRPCRequest) {
	result := ToolsListResult{
		Tools: getAllToolDefs(),
	}
	writeResponse(req.ID, result)
}

func handleToolsCall(req JSONRPCRequest) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(req.ID, -32602, "Invalid params: "+err.Error())
		return
	}

	if params.Arguments == nil {
		params.Arguments = make(map[string]interface{})
	}

	log.Printf("tools/call: %s", params.Name)
	result := dispatchToolCall(params)
	writeResponse(req.ID, result)
}

func handleResourcesList(req JSONRPCRequest) {
	result := ResourcesListResult{
		Resources: allResources,
	}
	writeResponse(req.ID, result)
}

func handleResourcesRead(req JSONRPCRequest) {
	var params ResourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(req.ID, -32602, "Invalid params: "+err.Error())
		return
	}

	contents, err := handleResourceRead(params.URI)
	if err != nil {
		writeError(req.ID, -32603, "Resource read error: "+err.Error())
		return
	}

	if contents == nil {
		writeError(req.ID, -32602, "Unknown resource: "+params.URI)
		return
	}

	result := ResourceReadResult{
		Contents: contents,
	}
	writeResponse(req.ID, result)
}
