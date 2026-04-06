package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// JSON-RPC 2.0 types

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP protocol types

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Capabilities    ServerCapabilities `json:"capabilities"`
}

type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
}

type ToolsCapability struct{}

type ResourcesCapability struct{}

type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
}

type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type ResourceReadParams struct {
	URI string `json:"uri"`
}

type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

type ResourcesListResult struct {
	Resources []ResourceDefinition `json:"resources"`
}

type ResourceReadResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ToolHandler is the function signature for tool implementations.
type ToolHandler func(args map[string]interface{}) ToolResult

// stdout writer with mutex for thread safety
var writeMu sync.Mutex

func writeResponse(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		writeError(id, -32603, "Internal error: "+err.Error())
		return
	}
	writeMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", data)
	os.Stdout.Sync()
	writeMu.Unlock()
}

func writeError(id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	writeMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", data)
	os.Stdout.Sync()
	writeMu.Unlock()
}

func toolSuccess(data interface{}) ToolResult {
	text, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return toolError("Failed to marshal result: " + err.Error())
	}
	return ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: string(text)},
		},
	}
}

func toolSuccessText(text string) ToolResult {
	return ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

func toolError(message string) ToolResult {
	return ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: message},
		},
		IsError: true,
	}
}

// Helper to get a string argument with a default value.
func getStringArg(args map[string]interface{}, key, defaultVal string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return defaultVal
}

// Helper to get a required string argument.
func requireStringArg(args map[string]interface{}, key string) (string, bool) {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s, true
		}
	}
	return "", false
}
