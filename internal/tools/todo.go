package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type TodoItem struct {
	ID     int    `json:"id"`
	Desc   string `json:"desc"`
	Done   bool   `json:"done"`
	Source string `json:"source,omitempty"`
}

var (
	todoMu    sync.Mutex
	todoItems []TodoItem
	nextTodo  int
	todoPath  string
)

func initTodoPath() {
	if todoPath == "" {
		todoPath = filepath.Join(MemoryDir, "todos.json")
	}
}

func loadTodos() {
	initTodoPath()
	data, err := os.ReadFile(todoPath)
	if err != nil {
		todoItems = nil
		nextTodo = 0
		return
	}
	var stored struct {
		Items  []TodoItem `json:"items"`
		NextID int        `json:"next_id"`
	}
	json.Unmarshal(data, &stored)
	todoItems = stored.Items
	nextTodo = stored.NextID
}

func saveTodos() {
	initTodoPath()
	os.MkdirAll(filepath.Dir(todoPath), 0755)
	data, _ := json.MarshalIndent(map[string]interface{}{
		"items":   todoItems,
		"next_id": nextTodo,
	}, "", "  ")
	os.WriteFile(todoPath, data, 0644)
}

func registerTodoTools() {
	Register("todo_list", "List all todo items (persisted to disk).",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		todoListHandler,
	)

	Register("todo_add", "Add tasks to the todo list (persisted to disk).",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"todos"},
			"properties": map[string]interface{}{
				"todos": map[string]interface{}{
					"type":        "array",
					"description": "List of task descriptions",
					"items":       map[string]interface{}{"type": "string"},
					"minItems":    1,
				},
				"source": stringParam("Optional source memory/knowledge ID to link back to"),
			},
		},
		todoAddHandler,
	)

	Register("todo_done", "Mark a todo item as done by ID.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": intParam("Task ID to mark as done"),
			},
		},
		todoDoneHandler,
	)

	Register("todo_clear", "Clear all completed tasks.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		todoClearHandler,
	)

	Register("todo_by_source", "List todos linked to a specific knowledge/memory source ID.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"source"},
			"properties": map[string]interface{}{
				"source": stringParam("Source memory/knowledge ID"),
			},
		},
		todoBySourceHandler,
	)
}

func todoListHandler(args map[string]interface{}) *ToolResult {
	todoMu.Lock()
	defer todoMu.Unlock()

	loadTodos()
	if len(todoItems) == 0 {
		return successResult("No tasks.")
	}

	var sb strings.Builder
	for _, t := range todoItems {
		mark := "[ ]"
		if t.Done {
			mark = "[x]"
		}
		sb.WriteString(fmt.Sprintf("  %s #%d: %s", mark, t.ID, t.Desc))
		if t.Source != "" {
			sb.WriteString(fmt.Sprintf(" [from: %s]", t.Source))
		}
		sb.WriteString("\n")
	}
	return successResult(sb.String())
}

func todoAddHandler(args map[string]interface{}) *ToolResult {
	raw, ok := args["todos"].([]interface{})
	if !ok || len(raw) == 0 {
		return errorResult("todos must be a non-empty array of strings")
	}
	source, _ := args["source"].(string)

	todoMu.Lock()
	defer todoMu.Unlock()

	loadTodos()

	count := 0
	for _, r := range raw {
		if s, ok := r.(string); ok {
			nextTodo++
			todoItems = append(todoItems, TodoItem{
				ID:     nextTodo,
				Desc:   s,
				Done:   false,
				Source: source,
			})
			count++
		}
	}

	saveTodos()
	return successResult(fmt.Sprintf("Added %d tasks (persisted).", count))
}

func todoDoneHandler(args map[string]interface{}) *ToolResult {
	id, _ := args["id"].(float64)
	todoMu.Lock()
	defer todoMu.Unlock()

	loadTodos()
	for i := range todoItems {
		if todoItems[i].ID == int(id) {
			todoItems[i].Done = true
			saveTodos()
			return successResult(fmt.Sprintf("Task #%d marked done.", int(id)))
		}
	}
	return errorResult(fmt.Sprintf("Task #%d not found.", int(id)))
}

func todoClearHandler(args map[string]interface{}) *ToolResult {
	todoMu.Lock()
	defer todoMu.Unlock()

	loadTodos()
	var kept []TodoItem
	for _, t := range todoItems {
		if !t.Done {
			kept = append(kept, t)
		}
	}
	todoItems = kept
	saveTodos()

	return successResult("Cleared completed tasks.")
}

func todoBySourceHandler(args map[string]interface{}) *ToolResult {
	source, _ := args["source"].(string)
	if source == "" {
		return errorResult("source is required")
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	loadTodos()
	var sb strings.Builder
	for _, t := range todoItems {
		if t.Source == source {
			mark := "[ ]"
			if t.Done {
				mark = "[x]"
			}
			sb.WriteString(fmt.Sprintf("  %s #%d: %s [%s]\n", mark, t.ID, t.Desc, source))
		}
	}
	if sb.Len() == 0 {
		return successResult(fmt.Sprintf("No tasks found for source %s.", source))
	}
	return successResult(sb.String())
}
