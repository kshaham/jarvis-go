package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
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

	// bufio.Reader.ReadBytes reads a full newline-delimited line of ANY length
	// (it accumulates across buffer fills), unlike bufio.Scanner whose 1MB token
	// cap made any oversized request abort the read loop and kill the whole
	// server. A too-big or malformed line now fails as a per-request parse error
	// (-32700) and the loop continues.
	reader := bufio.NewReaderSize(os.Stdin, 1024*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var req JSONRPCRequest
			if uerr := json.Unmarshal(trimmed, &req); uerr != nil {
				writeError(nil, -32700, "Parse error: "+uerr.Error())
			} else {
				handleRequest(req)
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Fatalf("stdin read error: %v", err)
		}
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
