package tools

import (
	"fmt"
	"sort"
)

// Toolset defines a named group of tools for a specific capability.
// Toolsets can compose from other toolsets via Includes.
// Absorbed from FangLab internal/tools/toolsets.go (2026-05-26).
type Toolset struct {
	Name        string
	Description string
	Tools       []string // direct tool names
	Includes    []string // other toolset names to compose
}

// DefaultToolsets defines the preset capability groups for beishan-core.
var DefaultToolsets = map[string]*Toolset{
	"web": {
		Name:        "web",
		Description: "Web search and content extraction",
		Tools:       []string{"web_search", "web_fetch", "web_extract", "web_render"},
	},
	"browser": {
		Name:        "browser",
		Description: "Browser automation for interactive web tasks",
		Tools:       []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_scroll", "browser_back"},
	},
	"file": {
		Name:        "file",
		Description: "File read/write/search/patch operations",
		Tools:       []string{"read_file", "write_file", "search_files", "patch", "file_parse"},
	},
	"code": {
		Name:        "code",
		Description: "Code analysis, security check, and generation",
		Tools:       []string{"code_read", "code_diff", "code_apply", "code_rollback", "code_security_check", "code_tree", "code_stats", "go_struct_scan"},
	},
	"knowledge": {
		Name:        "knowledge",
		Description: "Knowledge base storage, search, and management",
		Tools:       []string{"knowledge_search", "knowledge_add", "knowledge_list", "knowledge_get", "knowledge_delete", "knowledge_embed", "knowledge_semantic_search"},
	},
	"terminal": {
		Name:        "terminal",
		Description: "Terminal command execution and management",
		Tools:       []string{"terminal_exec", "terminal_list", "terminal_poll", "terminal_kill"},
	},
	"memory": {
		Name:        "memory",
		Description: "Session memory and conversational context management",
		Tools:       []string{"session_add", "session_get", "session_search", "session_list", "session_summarize"},
	},
	"todo": {
		Name:        "todo",
		Description: "Task and todo management",
		Tools:       []string{"todo_list", "todo_add", "todo_done", "todo_clear"},
	},
	"legal": {
		Name:        "legal",
		Description: "Legal research and document review",
		Tools:       []string{"legal_search", "clause_analysis", "legal_generate_report"},
	},
	"stock": {
		Name:        "stock",
		Description: "Stock quotes and market data",
		Tools:       []string{"stock_quote", "stock_multi_quote"},
	},
	"research": {
		Name:        "research",
		Description: "Full research mode: web + knowledge + browser",
		Includes:    []string{"web", "knowledge", "browser"},
	},
}

// ResolveToolset resolves a toolset name into a flat list of tool names,
// recursively following Includes with cycle detection.
func ResolveToolset(name string, toolsets map[string]*Toolset) ([]string, error) {
	if toolsets == nil {
		toolsets = DefaultToolsets
	}
	visited := make(map[string]bool)
	var result []string
	var resolve func(name string) error

	resolve = func(name string) error {
		if visited[name] {
			return fmt.Errorf("cycle detected in toolset includes: %s", name)
		}
		visited[name] = true
		defer delete(visited, name)

		ts, ok := toolsets[name]
		if !ok {
			return fmt.Errorf("toolset not found: %s", name)
		}

		result = append(result, ts.Tools...)

		for _, inc := range ts.Includes {
			if err := resolve(inc); err != nil {
				return err
			}
		}
		return nil
	}

	if err := resolve(name); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var deduped []string
	for _, t := range result {
		if !seen[t] {
			seen[t] = true
			deduped = append(deduped, t)
		}
	}
	sort.Strings(deduped)
	return deduped, nil
}

// ListToolsetNames returns sorted toolset names from the given map.
func ListToolsetNames(toolsets map[string]*Toolset) []string {
	names := make([]string, 0, len(toolsets))
	for name := range toolsets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
