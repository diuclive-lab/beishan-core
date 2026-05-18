package tools

import (
	"fmt"
	"strings"
	"sync"
)

var (
	todoMu    sync.Mutex
	todoItems []map[string]interface{}
	nextTodo  int
)

func registerTodoTools() {
	Register("todo_list", "List all todo items.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		todoListHandler,
	)

	Register("todo_add", "Add tasks to the todo list.",
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
}

func todoListHandler(args map[string]interface{}) *ToolResult {
	todoMu.Lock()
	defer todoMu.Unlock()

	if len(todoItems) == 0 {
		return successResult("No tasks.")
	}

	var sb strings.Builder
	for _, t := range todoItems {
		id, _ := t["id"].(float64)
		desc, _ := t["desc"].(string)
		done, _ := t["done"].(bool)
		mark := "[ ]"
		if done {
			mark = "[x]"
		}
		sb.WriteString(fmt.Sprintf("  %s #%d: %s\n", mark, int(id), desc))
	}
	return successResult(sb.String())
}

func todoAddHandler(args map[string]interface{}) *ToolResult {
	raw, ok := args["todos"].([]interface{})
	if !ok || len(raw) == 0 {
		return errorResult("todos must be a non-empty array of strings")
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	count := 0
	for _, r := range raw {
		if s, ok := r.(string); ok {
			nextTodo++
			todoItems = append(todoItems, map[string]interface{}{
				"id":   nextTodo,
				"desc": s,
				"done": false,
			})
			count++
		}
	}

	return successResult(fmt.Sprintf("Added %d tasks.", count))
}

func todoDoneHandler(args map[string]interface{}) *ToolResult {
	id, _ := args["id"].(float64)
	todoMu.Lock()
	defer todoMu.Unlock()

	for _, t := range todoItems {
		tid, _ := t["id"].(float64)
		if int(tid) == int(id) {
			t["done"] = true
			return successResult(fmt.Sprintf("Task #%d marked done.", int(id)))
		}
	}
	return errorResult(fmt.Sprintf("Task #%d not found.", int(id)))
}

func todoClearHandler(args map[string]interface{}) *ToolResult {
	todoMu.Lock()
	defer todoMu.Unlock()

	var kept []map[string]interface{}
	for _, t := range todoItems {
		done, _ := t["done"].(bool)
		if !done {
			kept = append(kept, t)
		}
	}
	todoItems = kept

	return successResult("Cleared completed tasks.")
}
