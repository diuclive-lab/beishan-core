package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/tools"
)

// RunOptions controls a single sub-agent execution.
type RunOptions struct {
	TaskPrompt  string
	Definition  Definition
	TaskTimeout time.Duration
}

// SubagentResult is the outcome of a sub-agent run.
type SubagentResult struct {
	TaskID     string `json:"task_id"`
	AgentID    string `json:"agent_id"`
	Output     string `json:"output"`
	Iterations int    `json:"iterations"`
	ElapsedMs  int64  `json:"elapsed_ms"`
	Error      string `json:"error,omitempty"`
}

// ParallelTask is one unit of work in a parallel spawn.
type ParallelTask struct {
	AgentID string `json:"agent_id"`
	Prompt  string `json:"prompt"`
}

// RunSubagent executes a sub-agent with the given definition and task prompt.
func RunSubagent(taskID, taskPrompt string, def Definition, timeout time.Duration) SubagentResult {
	start := time.Now()

	// Check spawn depth to prevent infinite recursion
	_, err := IncSpawnDepth()
	if err != nil {
		return SubagentResult{
			TaskID: taskID, AgentID: def.ID,
			Error:     fmt.Sprintf("spawn depth exceeded: possible infinite recursion"),
			ElapsedMs: time.Since(start).Milliseconds(),
		}
	}
	defer DecSpawnDepth()

	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	maxIter := def.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	// Build system prompt with tool descriptions
	sysPrompt := buildSubagentPrompt(def)
	toolDesc := buildToolDescriptions(def.Tools)

	messages := []llm.ChatMessage{
		{Role: "system", Content: sysPrompt + "\n\n" + toolDesc},
		{Role: "user", Content: taskPrompt},
	}

	// Use model/provider override if specified
	useProvider := ""
	if def.Provider != "" {
		useProvider = def.Provider
	}

	iterations := 0
	var lastOutput string

	for iterations < maxIter {
		iterations++

		var reply string
		var llmErr error
		if useProvider != "" {
			reply, _, llmErr = llm.ChatCompletionWithProvider(useProvider, messages, timeout)
		} else {
			reply, _, llmErr = llm.ChatCompletionWithUsage(messages, timeout)
		}
		if llmErr != nil {
			return SubagentResult{
				TaskID: taskID, AgentID: def.ID,
				Error:     "LLM call: " + llmErr.Error(),
				ElapsedMs: time.Since(start).Milliseconds(),
			}
		}

		tc := parseToolCall(reply)
		if tc == nil {
			lastOutput = reply
			break
		}

		log.Printf("[subagent %s] iter %d: calling %s", def.ID, iterations, tc.Tool)
		toolResult := executeTool(tc)

		messages = append(messages, llm.ChatMessage{Role: "assistant", Content: reply})
		messages = append(messages, llm.ChatMessage{Role: "tool", Content: toolResult})

		lastOutput = reply
	}

	return SubagentResult{
		TaskID: taskID, AgentID: def.ID,
		Output:     lastOutput,
		Iterations: iterations,
		ElapsedMs:  time.Since(start).Milliseconds(),
	}
}

// RunParallel executes multiple sub-agent tasks concurrently and returns combined results.
func RunParallel(tasks []ParallelTask, timeout time.Duration) []SubagentResult {
	results := make([]SubagentResult, len(tasks))
	done := make(chan struct{}, len(tasks))

	for i, t := range tasks {
		go func(idx int, task ParallelTask) {
			def, ok := Get(task.AgentID)
			if !ok {
				results[idx] = SubagentResult{
					TaskID: task.AgentID, AgentID: task.AgentID,
					Error: fmt.Sprintf("agent %q not found", task.AgentID),
				}
				done <- struct{}{}
				return
			}
			results[idx] = RunSubagent(fmt.Sprintf("parallel-%d", idx), task.Prompt, def, timeout)
			done <- struct{}{}
		}(i, t)
	}

	for range tasks {
		<-done
	}
	return results
}

type parsedToolCall struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

func parseToolCall(output string) *parsedToolCall {
	raw := strings.TrimSpace(output)
	braceStart := strings.Index(raw, "{")
	braceEnd := strings.LastIndex(raw, "}")
	if braceStart < 0 || braceEnd <= braceStart {
		return nil
	}

	var tc parsedToolCall
	if err := json.Unmarshal([]byte(raw[braceStart:braceEnd+1]), &tc); err != nil {
		return nil
	}
	if tc.Tool == "" {
		return nil
	}
	return &tc
}

func executeTool(tc *parsedToolCall) string {
	if !tools.HasTool(tc.Tool) {
		return fmt.Sprintf("Error: tool %q not found", tc.Tool)
	}
	argsJSON, _ := json.Marshal(tc.Arguments)
	result := tools.Execute(tc.Tool, string(argsJSON))
	if result.Error != "" {
		return fmt.Sprintf("Tool %s error: %s", tc.Tool, result.Error)
	}
	return fmt.Sprintf("Tool %s result:\n%s", tc.Tool, truncateStr(result.Output, 2000))
}

func buildSubagentPrompt(def Definition) string {
	p := def.SystemPrompt
	if p == "" {
		p = "You are a helpful sub-agent. Complete the delegated task concisely."
	}
	p += "\n\nTo use a tool, output a JSON object on its own line:\n"
	p += `{"tool": "name", "arguments": {"key": "value"}}` + "\n"
	p += "When done, respond with your final answer in plain text (no JSON)."
	return p
}

func buildToolDescriptions(allowedTools []string) string {
	defs := tools.GetDefinitions(allowedTools)
	if len(defs) == 0 {
		return "No tools available."
	}
	var b strings.Builder
	b.WriteString("Available tools:\n")
	for _, d := range defs {
		b.WriteString(fmt.Sprintf("  - %s: %s\n", d.Function.Name, d.Function.Description))
	}
	return b.String()
}

func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
