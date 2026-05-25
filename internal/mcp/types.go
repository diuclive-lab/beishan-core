// Package mcp implements the Model Context Protocol client.
// MCP servers provide specialized skills (finance, legal, math, etc.)
// as independently defined tool + prompt combinations.
package mcp

import (
	"encoding/json"
	"fmt"
)

// ToolDefinition describes a tool exposed by an MCP server.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolResult is the result of calling an MCP tool.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is a piece of content returned by a tool.
type ContentItem struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// ServerDef defines an MCP server configuration.
type ServerDef struct {
	ID          string   `json:"id"`          // e.g. "finance-research"
	Name        string   `json:"name"`        // e.g. "金融研究"
	Command     string   `json:"command"`     // e.g. "python3"
	Args        []string `json:"args"`        // e.g. ["server.py"]
	Description string   `json:"description"` // when to use this skill
}

// Registry holds all registered MCP servers.
type Registry struct {
	servers map[string]ServerDef
}

var globalRegistry = &Registry{servers: make(map[string]ServerDef)}

func Register(def ServerDef) {
	globalRegistry.servers[def.ID] = def
}

func Get(id string) (ServerDef, bool) {
	s, ok := globalRegistry.servers[id]
	return s, ok
}

func List() []ServerDef {
	var list []ServerDef
	for _, s := range globalRegistry.servers {
		list = append(list, s)
	}
	return list
}

func (d ServerDef) String() string {
	return fmt.Sprintf("  - %s: %s (%s)\n", d.ID, d.Name, d.Description)
}
