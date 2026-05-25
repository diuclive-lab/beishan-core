package mcp

import (
	"fmt"
	"log"
	"strings"
)

// SkillRunner manages MCP-based skills as L3 tools.
type SkillRunner struct {
	clients map[string]*Client
}

func NewSkillRunner() *SkillRunner {
	return &SkillRunner{clients: make(map[string]*Client)}
}

// StartAll connects to all registered MCP servers.
func (sr *SkillRunner) StartAll() {
	for id, def := range globalRegistry.servers {
		client := &Client{Def: def}
		if err := client.Connect(); err != nil {
			log.Printf("[mcp] skill %s connect failed: %v", id, err)
			continue
		}
		sr.clients[id] = client
		log.Printf("[mcp] skill %s connected", id)
	}
}

// Call invokes a tool on the named MCP server.
func (sr *SkillRunner) Call(skillID, toolName string, args map[string]interface{}) (string, error) {
	client, ok := sr.clients[skillID]
	if !ok {
		return "", fmt.Errorf("skill %q not connected", skillID)
	}
	result, err := client.CallTool(toolName, args)
	if err != nil {
		return "", err
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// CloseAll shuts down all MCP servers.
func (sr *SkillRunner) CloseAll() {
	for id, client := range sr.clients {
		client.Close()
		log.Printf("[mcp] skill %s closed", id)
	}
}
