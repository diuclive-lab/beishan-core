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
	"context"
	"fmt"
	"sync"
)

// MAX_SPAWN_DEPTH is the maximum nesting level for sub-agents.
// Prevents infinite recursion when a sub-agent spawns another sub-agent.
const MAX_SPAWN_DEPTH = 10

// currentSpawnDepth tracks nesting depth via task-local storage.
// Implemented as a simple counter, incremented on each spawn.
// Zero means we're in the root agent, not a sub-agent.
type spawnDepthKey struct{}

// CtxWithSpawnDepth returns a child context with incremented spawn depth.
// Each goroutine gets its own context, so parallel spawns don't interfere.
func CtxWithSpawnDepth(ctx context.Context) (context.Context, error) {
	depth := 0
	if d, ok := ctx.Value(spawnDepthKey{}).(int); ok {
		depth = d
	}
	depth++
	if depth > MAX_SPAWN_DEPTH {
		return ctx, fmt.Errorf("spawn depth exceeded: max %d", MAX_SPAWN_DEPTH)
	}
	return context.WithValue(ctx, spawnDepthKey{}, depth), nil
}

// ModelSpec controls which model a sub-agent uses.
type ModelSpec struct {
	// Provider overrides the global LLM provider for this agent.
	// Empty string means inherit the parent's provider.
	// Examples: "deepseek", "local", "xiaomi"
	Provider string `json:"provider,omitempty"`
	// Model overrides the model name.
	// Empty string means use the provider's default model.
	Model string `json:"model,omitempty"`
	// Temperature overrides the sampling temperature.
	// Zero means use the agent's default (0.7).
	Temperature float64 `json:"temperature,omitempty"`
}

// Definition specifies a sub-agent archetype: its identity, prompt, and tool scope.
type Definition struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name,omitempty"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Tools        []string `json:"tools"`          // allowed tool names (empty = all)
	ModelSpec    `json:"model_spec,omitempty"`    // model/provider override
	MaxTokens    int      `json:"max_tokens,omitempty"`
	MaxIterations int     `json:"max_iterations,omitempty"` // default 10
}

// ToolName returns the L3 tool name for delegating to this agent.
func (d *Definition) ToolName() string { return "delegate_to_" + d.ID }

// ToolDescription returns the description for the delegation tool.
func (d *Definition) ToolDescription() string {
	desc := d.Description
	if desc == "" {
		desc = "Delegate a task to " + d.ID
	}
	desc += ". Pass a clear prompt with all necessary context."
	return desc
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
