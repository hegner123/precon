// Package mcp provides minimal types for MCP server integration.
// This is a stub — full MCP client/server support will be added later.
package mcp

import (
	"context"
	"fmt"
)

// ContentBlock represents a content block in an MCP tool result.
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// ToolResult holds the result of a tool call.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ServerManager manages MCP server connections.
// This is a stub — methods return errors indicating MCP is not configured.
type ServerManager struct{}

// CallTool dispatches a tool call to an MCP server. Stub: always errors.
func (m *ServerManager) CallTool(ctx context.Context, server, tool string, input map[string]any) (*ToolResult, error) {
	return nil, fmt.Errorf("MCP not configured: cannot call %s/%s", server, tool)
}

// StopAll stops all managed servers. Stub: no-op.
func (m *ServerManager) StopAll() {}
