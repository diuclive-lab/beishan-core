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

// SubagentFactory creates an in-process subagent. Set by the agent package during init.
var SubagentFactory func(goal, contextStr string, toolsets []string) (string, error)

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
	Definition ToolDefinition
	Handler    ToolHandler
	CheckFn    CheckFn // nil = always available
}

// Registry holds all registered tools.
var Registry = make(map[string]*RegisteredTool)

// Register adds a tool to the registry (always available).
func Register(name, description string, params interface{}, handler ToolHandler) {
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
	registerSessionSearchTools()
	registerTodoTools()
	registerClarifyTools()
	registerVisionTool()
	registerImageGenTool()
	registerTTSTool()
	registerKnowledgeTools()
	registerFileParseTools()
	registerNotifyTools()
	registerCodexTools()
	registerClaudeTools()
	registerEmbedTools()
	registerSkillEvalTools()
	registerSystemInfoTools()
	registerWebRenderTool()
	registerStockTools()
	registerKBAuditTools()

	log.Printf("[tools] registered %d tools", len(Registry))
}

func errorResult(msg string) *ToolResult {
	return &ToolResult{Success: false, Output: msg, Error: msg}
}

func successResult(output string) *ToolResult {
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
