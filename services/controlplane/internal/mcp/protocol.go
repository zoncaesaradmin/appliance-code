// Package mcp implements the pinned MCP Streamable HTTP transport at
// /mcp: JSON-RPC 2.0 framing, initialization/version negotiation, session
// management, and appliance API-token Bearer authorization mapped onto the
// same RBAC engine REST uses. Tool modules are capability-gated and
// permission-filtered; a valid tools/list always returns a non-nil list.
package mcp

import "encoding/json"

// ProtocolVersion is the pinned MCP specification revision this server
// implements.
const ProtocolVersion = "2025-11-25"

// JSONRPCVersion is the fixed JSON-RPC envelope version every message must
// declare.
const JSONRPCVersion = "2.0"

// Standard JSON-RPC 2.0 error codes used by this package.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// Request is one JSON-RPC 2.0 request or notification. A notification has
// no ID.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether r carries no ID and therefore expects no
// response body beyond HTTP 202 Accepted.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0
}

// ErrorObject is a JSON-RPC 2.0 error.
type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Response is one JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

func newResult(id json.RawMessage, result any) Response {
	return Response{JSONRPC: JSONRPCVersion, ID: id, Result: result}
}

func newError(id json.RawMessage, code int, message string) Response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return Response{JSONRPC: JSONRPCVersion, ID: id, Error: &ErrorObject{Code: code, Message: message}}
}

// ClientInfo identifies the connecting MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo identifies this server to the client.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeParams is the client's "initialize" request payload.
type InitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ClientInfo      ClientInfo      `json:"clientInfo"`
}

// ToolsCapability advertises tool-related support. An empty struct still
// declares the capability present, so tools/list is a valid method call even
// when no tools are enabled for the appliance profile or principal.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerCapabilities is the capability set returned in "initialize".
type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// InitializeResult is the server's "initialize" response payload.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// Tool describes one callable tool exposed to the authenticated principal.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolsListResult is the "tools/list" response payload. Tools is always a
// non-nil, possibly empty slice so it encodes as [] rather than null.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}
