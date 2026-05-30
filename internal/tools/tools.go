// Package tools provides Go-native implementations of all Hermes Agent tools.
// Port of tools/*.py from Python Hermes Agent (54,294 lines).
// Each tool is self-registered and implements its core logic in Go.
package tools

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"beishan/internal/registry"
)

// Global state accessible by tools
var (
	// HermesHome is the Hermes home directory
	HermesHome string
	// SessionStore provides access to the SQLite session store for search tools
	SessionStore SessionStoreInterface
	// ProcessManager tracks background processes
	ProcessManager ProcessManagerInterface
	// CronStore provides access to cron job storage
	CronStorePath string
	// MemoryDir is the directory for memory files
	MemoryDir string
)

// SessionStoreInterface abstracts the session store for tools.
type SessionStoreInterface interface {
	SearchMessages(query string, limit int) ([]SessionSearchResult, error)
	RecentSessions(limit int) ([]SessionInfo, error)
}

// SessionSearchResult is a search result from the session store.
type SessionSearchResult struct {
	SessionID string
	MessageID int64
	Content   string
	Role      string
	Timestamp float64
	Rank      float64
}

// SessionInfo is basic session info for listing.
type SessionInfo struct {
	ID           string
	Source       string
	Model        string
	Title        string
	StartedAt    float64
	MessageCount int
}

// ProcessManagerInterface manages background processes.
type ProcessManagerInterface interface {
	List() []ProcessInfo
	Poll(sessionID string) string
	Kill(sessionID string) error
	Send(sessionID, data string) error
}

// ProcessInfo describes a background process.
type ProcessInfo struct {
	SessionID string
	Command   string
	Running   bool
	Age       string
}

func init() {
	// Set defaults
	if HermesHome == "" {
		HermesHome = os.Getenv("HERMES_HOME")
		if HermesHome == "" {
			home, _ := os.UserHomeDir()
			HermesHome = filepath.Join(home, ".hermes")
		}
	}
	if MemoryDir == "" {
		MemoryDir = filepath.Join(HermesHome, "memory")
	}
	if CronStorePath == "" {
		CronStorePath = filepath.Join(HermesHome, "cron", "jobs.json")
	}
}

var toolsMu sync.Mutex

// AgentSpawn is set by main.go to avoid import cycle.
// Calls agent.RunSubagent with the given agent ID and task prompt.
var AgentSpawn func(agentID, prompt string, timeout time.Duration) *ToolResult

// AgentParallel is set by main.go to avoid import cycle.
// Calls agent.RunParallel with the given task list.
var AgentParallel func(tasksJSON string) *ToolResult

// RegisterAgentTools registers spawn_subagent and spawn_parallel tools.
// Called from main.go after setting AgentSpawn and AgentParallel callbacks.
func RegisterAgentTools() {
	Register("spawn_subagent", "Delegate a task to a specialised sub-agent. Use when a task requires a different expertise or tool set.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"agent_id", "prompt"},
			"properties": map[string]interface{}{
				"agent_id": stringParam("Agent ID to delegate to (e.g. researcher, coder)"),
				"prompt":   stringParam("Clear task description with all necessary context"),
				"timeout":  stringParam("Optional timeout in seconds (default 120)"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			if AgentSpawn == nil {
				return errorResult("agent system not initialized")
			}
			return AgentSpawn(strArg(args, "agent_id"), strArg(args, "prompt"), 120*time.Second)
		},
	)

	Register("spawn_parallel", "Run multiple sub-agent tasks concurrently and collect results. Use for independent parallel work.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"tasks"},
			"properties": map[string]interface{}{
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "List of sub-agent tasks",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"agent_id", "prompt"},
						"properties": map[string]interface{}{
							"agent_id": stringParam("Agent ID"),
							"prompt":   stringParam("Task description"),
						},
					},
				},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			if AgentParallel == nil {
				return errorResult("agent system not initialized")
			}
			tasksRaw, _ := args["tasks"].([]interface{})
			if len(tasksRaw) == 0 {
				return errorResult("'tasks' is required")
			}
			var list []map[string]interface{}
			for _, t := range tasksRaw {
				if m, ok := t.(map[string]interface{}); ok {
					list = append(list, m)
				}
			}
			data, _ := json.Marshal(list)
			return AgentParallel(string(data))
		},
	)
}

// ─── Registry ────────────────────────────────────────────────────────────────

// ToolDefinition describes a tool for LLM function calling.
type ToolDefinition struct {
	Type     string      `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function schema.
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolResult is the result of executing a tool.
type ToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ToolHandler executes a tool with given parameters.
type ToolHandler func(args map[string]interface{}) *ToolResult

// CheckFn returns true if the tool's backend is available.
// Matching Python registry.py check_fn mechanism.
type CheckFn func() bool

// RegisteredTool holds a tool's definition and handler.
type RegisteredTool struct {
	Definition        ToolDefinition
	Handler           ToolHandler
	CheckFn           CheckFn // nil = always available
	Deprecated        bool   // tool is deprecated
	DeprecationNotice string // shown when called
}

// Registry holds all registered tools.
var Registry = make(map[string]*RegisteredTool)

// agentRegistry holds tools that are only available to the internal agent framework.
// Right flowers and the general LLM routing path (think_plugin) cannot access these.
// Use RegisterAgentOnly to add tools here; use ExecuteAgentTool / HasAgentTool to call them.
var agentRegistry = make(map[string]*RegisteredTool)

// RegisterAgentOnly registers a tool that is exclusive to the internal agent framework.
// It will NOT appear in the global Registry, so ValidateAndExecute and think_plugin
// cannot see or invoke it.
func RegisterAgentOnly(name, description string, params interface{}, handler ToolHandler) {
	agentRegistry[name] = &RegisteredTool{
		Definition: ToolDefinition{
			Type: "function",
			Function: ToolFunction{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		},
		Handler: handler,
	}
}

// HasAgentTool returns true if a tool exists in the global registry OR the agent-only registry.
func HasAgentTool(name string) bool {
	_, ok := Registry[name]
	if ok {
		return true
	}
	_, ok = agentRegistry[name]
	return ok
}

// ExecuteAgentTool runs a tool available to the agent framework (checks both registries).
// Agent-only tools (e.g. MCP skills) are accessible here but not via ValidateAndExecute.
func ExecuteAgentTool(name string, rawArgs json.RawMessage) *ToolResult {
	// Check agent-only registry first, then fall through to global
	if tool, ok := agentRegistry[name]; ok {
		var args map[string]interface{}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorResult("invalid JSON arguments: " + err.Error())
		}
		return tool.Handler(args)
	}
	return ValidateAndExecute(name, rawArgs)
}

// GetAgentDefinitions returns tool definitions visible to the agent framework
// (global registry + agent-only registry), filtered by the given allowlist.
func GetAgentDefinitions(filter []string) []ToolDefinition {
	filterSet := make(map[string]bool, len(filter))
	for _, f := range filter {
		filterSet[f] = true
	}
	var defs []ToolDefinition
	collect := func(reg map[string]*RegisteredTool) {
		for name, tool := range reg {
			if len(filter) > 0 && !filterSet[name] {
				continue
			}
			if tool.CheckFn != nil && !tool.CheckFn() {
				continue
			}
			defs = append(defs, tool.Definition)
		}
	}
	collect(Registry)
	collect(agentRegistry)
	return SanitizeToolSchemas(defs)
}

// HasTool 返回指定名称的工具是否已注册。
func HasTool(name string) bool {
	_, ok := Registry[name]
	return ok
}

// Register adds a tool to the registry (always available).
func Register(name, description string, params interface{}, handler ToolHandler) {
	register(name, description, params, handler, "", false)
}

// RegisterDeprecated registers a deprecated tool. Calls to it will log a warning.
func RegisterDeprecated(name, description, deprecationNotice string, params interface{}, handler ToolHandler) {
	register(name, description, params, handler, deprecationNotice, true)
}

func register(name, description string, params interface{}, handler ToolHandler, deprecationNotice string, deprecated bool) {
	RegisterWithCheck(name, description, params, handler, nil)
}

// RegisterWithCheck adds a tool with a conditional availability check.
// Also auto-registers the params as Schema for L3 hardening validation.
func RegisterWithCheck(name, description string, params interface{}, handler ToolHandler, check CheckFn) {
	Registry[name] = &RegisteredTool{
		Definition: ToolDefinition{
			Type: "function",
			Function: ToolFunction{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		},
		Handler: handler,
		CheckFn: check,
	}

	// Auto-register schema for L3 hardening
	if schemaMap, ok := params.(map[string]interface{}); ok {
		RegisterToolSchema(name, ToolSchema{
			Name:        name,
			Description: description,
			Schema:      schemaMap,
		})
	}
}

// GetDefinitions returns all tool definitions for LLM function calling.
func GetDefinitions(filter []string) []ToolDefinition {
	filterSet := make(map[string]bool, len(filter))
	for _, f := range filter {
		filterSet[f] = true
	}

	var defs []ToolDefinition
	for name, tool := range Registry {
		if len(filter) > 0 && !filterSet[name] {
			continue
		}
		// Skip tools whose backend is unavailable (Python check_fn)
		if tool.CheckFn != nil && !tool.CheckFn() {
			continue
		}
		defs = append(defs, tool.Definition)
	}
	// Sanitize schemas for strict backends (Python schema_sanitizer.py)
	return SanitizeToolSchemas(defs)
}

// MaxResultSize returns the per-tool result size cap. Default 100K.
func MaxResultSize(name string) int {
	if tool, ok := Registry[name]; ok && tool.Definition.Function.Name != "" {
		if name == "read_file" {
			return 0 // no limit for read_file
		}
	}
	return 100000
}

// Execute runs a tool by name with given JSON arguments.
//
// Note: L4 plugins should use ValidateAndExecute (with schema validation).
// Execute assumes args are valid JSON (guaranteed by ValidateAndExecute),
// and no longer does the lenient "raw string" fallback.
func Execute(name string, argsJSON string) *ToolResult {
	tool, ok := Registry[name]
	if !ok {
		return errorResult("unknown tool: " + name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return errorResult("invalid JSON arguments: " + err.Error())
	}

	return tool.Handler(args)
}

// Init registers all built-in tools.
func Init() {
	registerFileTools()
	registerWebTools()
	registerMemoryTools()
	registerJudicialTools()
	registerTerminalTools()
	registerCodeExecTool()
	registerBrowserTools()
	registerDeepseekWebTools()
	registerSessionSearchTools()
	registerTodoTools()
	registerClarifyTools()
	registerVisionTool()
	registerImageGenTool()
	registerImageEditTool()
	registerPromptTools()
	registerTTSTool()
	registerKnowledgeTools()
	registerFileParseTools()
	registerFileSafeTools()
	registerNotifyTools()
	registerCodexTools()
	registerClaudeTools()
	registerEmbedTools()
	registerSkillEvalTools()
	registerSystemInfoTools()
	registerWebRenderTool()
	registerStockTools()
	registerRSSTools()
	registerProfileTools()
	registerGitHubTools()
	registerCodeSecurityTools()
	registerCodeReadTools()
	registerCodeApplyTools()
	registerCodeAnalysisTools()
	registerSyncTools()
	registerKBAuditTools()
	registerDesktopTools()
		registerDocumentTools()
		registerCSVTools()
		registerUsageTools()

	log.Printf("[tools] registered %d tools", len(Registry))
	registry.DefaultInstance.Lock()
}

func ErrorResult(msg string) *ToolResult {
	return &ToolResult{Success: false, Output: msg, Error: msg}
}

func SuccessResult(output string) *ToolResult {
	return &ToolResult{Success: true, Output: output}
}

func emptyObjParams() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func stringParam(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": desc,
	}
}

func intParam(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "integer",
		"description": desc,
	}
}

func boolParam(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": desc,
	}
}

// truncateStr truncates a string to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}


// errorResult is a backward-compatible alias for ErrorResult.
func errorResult(msg string) *ToolResult { return ErrorResult(msg) }

// successResult is a backward-compatible alias for SuccessResult.
func successResult(output string) *ToolResult { return SuccessResult(output) }

