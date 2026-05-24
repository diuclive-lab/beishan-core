// Package agent provides sub-agent delegation — spawn_subagent + spawn_parallel.
// Absorbed from OpenHuman's AgentDefinitionRegistry + subagent_runner pattern.
//
// Usage:
//
//	agent.Register(agent.Definition{
//	    ID:          "researcher",
//	    DisplayName: "研究员",
//	    SystemPrompt: "你是一个研究员。使用搜索工具查找信息。",
//	    Tools:       []string{"web_search", "web_fetch"},
//	})
//
// Then from any L3 tool or workflow step:
//
//	spawn_subagent("researcher", "请调研 Go 语言并发模型")
//	spawn_parallel([{agent_id:"researcher", prompt:"..."}, {agent_id:"coder", prompt:"..."}])
package agent

import (
	"fmt"
	"sync"
)

// Definition specifies a sub-agent archetype: its identity, prompt, and tool scope.
type Definition struct {
	ID           string   `json:"id"`           // unique identifier, e.g. "researcher"
	DisplayName  string   `json:"display_name"` // human-readable name
	Description  string   `json:"description"`  // when to use this agent (shown to delegator LLM)
	SystemPrompt string   `json:"system_prompt"` // base system prompt
	Tools        []string `json:"tools"`         // allowed tool names (empty = all tools)
	Model        string   `json:"model,omitempty"` // optional model override
	Temperature  float64  `json:"temperature,omitempty"` // 0.0-1.0
	MaxTokens    int      `json:"max_tokens,omitempty"` // max response tokens
	MaxIterations int     `json:"max_iterations,omitempty"` // max tool call rounds (default 10)
}

// Registry holds all registered agent definitions.
type Registry struct {
	mu    sync.RWMutex
	agents map[string]Definition
}

// globalRegistry is the package-level singleton registry.
var globalRegistry = &Registry{agents: make(map[string]Definition)}

// Global returns the global agent registry.
func Global() *Registry { return globalRegistry }

// Register adds or replaces an agent definition.
func Register(def Definition) { globalRegistry.Register(def) }

// Get looks up an agent by ID.
func Get(id string) (Definition, bool) { return globalRegistry.Get(id) }

// List returns all registered agent IDs.
func List() []string { return globalRegistry.List() }

// Register adds or replaces an agent definition.
func (r *Registry) Register(def Definition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[def.ID] = def
}

// Get looks up an agent by ID.
func (r *Registry) Get(id string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.agents[id]
	return d, ok
}

// List returns all registered agent IDs.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	return ids
}

// String returns a human-readable summary of all registered agents.
func (r *Registry) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var s string
	for id, d := range r.agents {
		s += fmt.Sprintf("  - %s: %s (%d tools)\n", id, d.Description, len(d.Tools))
	}
	return s
}
