package registry

import (
	"fmt"
	"sort"
)

// ── Toolset ──────────────────────────────────────────────────────────────────

// Toolset is a named group of tools with optional include-based composition.
type Toolset struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`   // direct tool names
	Includes    []string `json:"includes"` // other toolset names to compose
}

// Resolve flattens a toolset name into a sorted, deduplicated list of tool names.
// Recursively follows Includes with cycle detection.
func Resolve(name string, toolsets map[string]Toolset) ([]string, error) {
	if _, ok := toolsets[name]; !ok {
		return nil, fmt.Errorf("toolset %q not found", name)
	}

	visited := map[string]bool{}
	var result []string
	seen := map[string]bool{}

	var walk func(string) error
	walk = func(current string) error {
		if visited[current] {
			return fmt.Errorf("cycle detected in toolset includes: %s", current)
		}
		visited[current] = true

		ts, ok := toolsets[current]
		if !ok {
			return fmt.Errorf("included toolset %q not found", current)
		}

		for _, t := range ts.Tools {
			if !seen[t] {
				seen[t] = true
				result = append(result, t)
			}
		}
		for _, inc := range ts.Includes {
			if err := walk(inc); err != nil {
				return err
			}
		}

		visited[current] = false
		return nil
	}

	if err := walk(name); err != nil {
		return nil, err
	}

	sort.Strings(result)
	return result, nil
}

// ── Default Toolsets ─────────────────────────────────────────────────────────

// DefaultToolsets returns the standard TwinFlower toolset definitions.
func DefaultToolsets() map[string]Toolset {
	return map[string]Toolset{
		"business": {
			Name: "business", Description: "Business data tools",
			Tools: []string{"weather", "stock", "currency", "translate"},
		},
		"web": {
			Name: "web", Description: "Web search and information retrieval",
			Tools: []string{"search"},
		},
		"filesystem": {
			Name: "filesystem", Description: "File and directory operations",
			Tools: []string{"filesystem_list", "filesystem_read", "filesystem_search"},
		},
		"general": {
			Name: "general", Description: "General-purpose toolset",
			Includes: []string{"business", "web", "filesystem"},
		},
	}
}
